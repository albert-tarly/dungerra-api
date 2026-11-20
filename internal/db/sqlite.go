package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/albert-tarly/dungerra-api/internal/config"
	"go.mongodb.org/mongo-driver/bson/primitive"

	// Pure-Go SQLite driver: no CGO required, so the server cross-compiles
	// into a single self-contained binary for desktop bundles.
	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// sqliteMigrations are applied in order; PRAGMA user_version records how many
// have run. Only ever append to this list — never edit a shipped migration.
var sqliteMigrations = []string{
	// 1: initial schema
	`
	CREATE TABLE users (
		id              TEXT PRIMARY KEY,
		provider        TEXT NOT NULL,         -- 'google' | 'local'
		provider_id     TEXT NOT NULL,         -- Google ID, or lower-cased local username
		email           TEXT NOT NULL DEFAULT '',
		google_id       TEXT UNIQUE,           -- NULL when absent (sparse unique)
		username        TEXT NOT NULL,
		password_hash   TEXT NOT NULL DEFAULT '', -- bcrypt; local accounts only
		is_active       INTEGER NOT NULL,
		created_at      INTEGER NOT NULL,      -- unix millis
		current_session TEXT NOT NULL DEFAULT '',
		UNIQUE (provider, provider_id)
	);

	CREATE TABLE game_sessions (
		id             TEXT PRIMARY KEY,
		name           TEXT NOT NULL,
		host_id        TEXT NOT NULL,
		max_players    INTEGER NOT NULL,
		is_private     INTEGER NOT NULL,
		password       TEXT NOT NULL DEFAULT '',
		is_active      INTEGER NOT NULL,
		game_version   TEXT NOT NULL,
		created_at     INTEGER NOT NULL,
		last_updated   INTEGER NOT NULL,
		players        TEXT NOT NULL,          -- JSON
		world_map      TEXT NOT NULL,          -- JSON
		shared_objects TEXT NOT NULL           -- JSON
	);
	CREATE INDEX idx_game_sessions_host_id   ON game_sessions(host_id);
	CREATE INDEX idx_game_sessions_is_active ON game_sessions(is_active);

	CREATE TABLE leaderboard (
		id           TEXT PRIMARY KEY,
		user_id      TEXT NOT NULL,
		username     TEXT NOT NULL,
		session_id   TEXT NOT NULL,
		session_name TEXT NOT NULL,
		score        INTEGER NOT NULL DEFAULT 0,
		kills        INTEGER NOT NULL DEFAULT 0,
		deaths       INTEGER NOT NULL DEFAULT 0,
		created_at   INTEGER NOT NULL,
		updated_at   INTEGER NOT NULL,
		UNIQUE (user_id, session_id)
	);
	CREATE INDEX idx_leaderboard_score         ON leaderboard(score DESC);
	CREATE INDEX idx_leaderboard_session_score ON leaderboard(session_id, score DESC);
	CREATE INDEX idx_leaderboard_user_updated  ON leaderboard(user_id, updated_at DESC);
	`,
	// 2: per-session difficulty (JSON; '' = created before difficulties, plays as Hard)
	`ALTER TABLE game_sessions ADD COLUMN difficulty TEXT NOT NULL DEFAULT '';`,
}

type sqliteStore struct {
	db          *sql.DB
	users       *sqliteUserRepository
	sessions    *sqliteGameSessionRepository
	leaderboard *sqliteLeaderboardRepository
}

func openSQLite(ctx context.Context, path string) (*sqliteStore, error) {
	if path == "" {
		return nil, errors.New("db: SQLITE_PATH is required for the sqlite driver")
	}
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("db: create sqlite directory: %w", err)
		}
	}

	// WAL lets readers proceed during writes; busy_timeout absorbs short lock
	// contention instead of failing with SQLITE_BUSY.
	dsn := "file:" + filepath.ToSlash(path) + "?" + url.Values{
		"_pragma": {"journal_mode(WAL)", "busy_timeout(5000)", "foreign_keys(ON)", "synchronous(NORMAL)"},
	}.Encode()

	conn, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// SQLite permits a single writer. A single connection serialises access in
	// Go rather than in SQLite's lock, which is simpler and plenty for this
	// workload (low-frequency persistence, not per-tick traffic).
	conn.SetMaxOpenConns(1)

	if err := conn.PingContext(ctx); err != nil {
		conn.Close()
		return nil, err
	}
	if err := migrateSQLite(ctx, conn); err != nil {
		conn.Close()
		return nil, err
	}

	return &sqliteStore{
		db:          conn,
		users:       &sqliteUserRepository{db: conn},
		sessions:    &sqliteGameSessionRepository{db: conn},
		leaderboard: &sqliteLeaderboardRepository{db: conn},
	}, nil
}

func migrateSQLite(ctx context.Context, conn *sql.DB) error {
	var version int
	if err := conn.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	for i := version; i < len(sqliteMigrations); i++ {
		tx, err := conn.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, sqliteMigrations[i]); err != nil {
			tx.Rollback()
			return fmt.Errorf("db: sqlite migration %d: %w", i+1, err)
		}
		// PRAGMA does not accept bound parameters.
		if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", i+1)); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

func (s *sqliteStore) Users() UserRepository              { return s.users }
func (s *sqliteStore) Sessions() GameSessionRepository    { return s.sessions }
func (s *sqliteStore) Leaderboard() LeaderboardRepository { return s.leaderboard }
func (s *sqliteStore) Close(context.Context) error        { return s.db.Close() }

// --- helpers ---

// rowScanner is satisfied by both *sql.Row and *sql.Rows.
type rowScanner interface {
	Scan(dest ...any) error
}

func mapSQLErr(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	var sqliteErr *sqlite.Error
	if errors.As(err, &sqliteErr) {
		switch sqliteErr.Code() {
		case sqlite3.SQLITE_CONSTRAINT_UNIQUE, sqlite3.SQLITE_CONSTRAINT_PRIMARYKEY:
			return ErrDuplicate
		}
	}
	return err
}

// Times are stored as unix milliseconds, the same precision MongoDB keeps.
func toMillis(t time.Time) int64 { return t.UnixMilli() }
func fromMillis(ms int64) time.Time {
	return time.UnixMilli(ms).UTC()
}

func parseObjectID(hex string) (primitive.ObjectID, error) {
	id, err := primitive.ObjectIDFromHex(hex)
	if err != nil {
		return primitive.NilObjectID, fmt.Errorf("db: corrupt id %q: %w", hex, err)
	}
	return id, nil
}

func nullIfEmpty(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}

func queryAll[T any](ctx context.Context, conn *sql.DB, scan func(rowScanner) (*T, error), query string, args ...any) ([]T, error) {
	rows, err := conn.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []T
	for rows.Next() {
		item, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *item)
	}
	return out, rows.Err()
}

func queryOne[T any](ctx context.Context, conn *sql.DB, scan func(rowScanner) (*T, error), query string, args ...any) (*T, error) {
	item, err := scan(conn.QueryRowContext(ctx, query, args...))
	if err != nil {
		return nil, mapSQLErr(err)
	}
	return item, nil
}

// --- Users ---

type sqliteUserRepository struct {
	db *sql.DB
}

const userColumns = `id, provider, provider_id, email, google_id, username, password_hash, is_active, created_at, current_session`

func scanUser(row rowScanner) (*User, error) {
	var (
		u         User
		id        string
		googleID  sql.NullString
		createdAt int64
	)
	if err := row.Scan(&id, &u.Provider, &u.ProviderID, &u.Email, &googleID, &u.Username, &u.PasswordHash,
		&u.IsActive, &createdAt, &u.CurrentSession); err != nil {
		return nil, err
	}
	var err error
	if u.ID, err = parseObjectID(id); err != nil {
		return nil, err
	}
	u.GoogleID = googleID.String
	u.CreatedAt = fromMillis(createdAt)
	return &u, nil
}

func (r *sqliteUserRepository) FindByProvider(ctx context.Context, provider, providerID string) (*User, error) {
	return queryOne(ctx, r.db, scanUser,
		`SELECT `+userColumns+` FROM users WHERE provider = ? AND provider_id = ?`, provider, providerID)
}

func (r *sqliteUserRepository) FindByID(ctx context.Context, id primitive.ObjectID) (*User, error) {
	return queryOne(ctx, r.db, scanUser, `SELECT `+userColumns+` FROM users WHERE id = ?`, id.Hex())
}

func (r *sqliteUserRepository) Create(ctx context.Context, user *User) error {
	if err := prepareNewUser(user); err != nil {
		return err
	}
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO users (`+userColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		user.ID.Hex(), user.Provider, user.ProviderID, user.Email, nullIfEmpty(user.GoogleID), user.Username,
		user.PasswordHash, user.IsActive, toMillis(user.CreatedAt), user.CurrentSession,
	)
	return mapSQLErr(err)
}

func (r *sqliteUserRepository) Update(ctx context.Context, user *User) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE users
		    SET provider = ?, provider_id = ?, email = ?, google_id = ?, username = ?, password_hash = ?,
		        is_active = ?, created_at = ?, current_session = ?
		  WHERE id = ?`,
		user.Provider, user.ProviderID, user.Email, nullIfEmpty(user.GoogleID), user.Username, user.PasswordHash,
		user.IsActive, toMillis(user.CreatedAt), user.CurrentSession, user.ID.Hex(),
	)
	return mapSQLErr(err)
}

// --- Game sessions ---

type sqliteGameSessionRepository struct {
	db *sql.DB
}

const sessionColumns = `id, name, host_id, max_players, is_private, password, is_active, game_version,
	created_at, last_updated, players, world_map, shared_objects, difficulty`

func scanSession(row rowScanner) (*GameSession, error) {
	var (
		s                                GameSession
		id, hostID                       string
		createdAt, lastUpdated           int64
		players, worldMap, sharedObjects []byte
		difficulty                       string
	)
	if err := row.Scan(&id, &s.Name, &hostID, &s.MaxPlayers, &s.IsPrivate, &s.Password, &s.IsActive,
		&s.GameVersion, &createdAt, &lastUpdated, &players, &worldMap, &sharedObjects, &difficulty); err != nil {
		return nil, err
	}

	var err error
	if s.ID, err = parseObjectID(id); err != nil {
		return nil, err
	}
	if s.HostID, err = parseObjectID(hostID); err != nil {
		return nil, err
	}
	s.CreatedAt = fromMillis(createdAt)
	s.LastUpdated = fromMillis(lastUpdated)

	if err := json.Unmarshal(players, &s.Players); err != nil {
		return nil, fmt.Errorf("db: decode session players: %w", err)
	}
	if err := json.Unmarshal(worldMap, &s.WorldMap); err != nil {
		return nil, fmt.Errorf("db: decode session world map: %w", err)
	}
	if err := json.Unmarshal(sharedObjects, &s.SharedObjects); err != nil {
		return nil, fmt.Errorf("db: decode session shared objects: %w", err)
	}
	if difficulty != "" {
		s.Difficulty = &config.Difficulty{}
		if err := json.Unmarshal([]byte(difficulty), s.Difficulty); err != nil {
			return nil, fmt.Errorf("db: decode session difficulty: %w", err)
		}
	}
	return &s, nil
}

// sessionArgs returns the values for sessionColumns, in order.
func sessionArgs(s *GameSession) ([]any, error) {
	players, err := json.Marshal(s.Players)
	if err != nil {
		return nil, err
	}
	worldMap, err := json.Marshal(s.WorldMap)
	if err != nil {
		return nil, err
	}
	sharedObjects, err := json.Marshal(s.SharedObjects)
	if err != nil {
		return nil, err
	}
	// "" (not "null") marks a session without a difficulty, matching the column default.
	difficulty := ""
	if s.Difficulty != nil {
		raw, err := json.Marshal(s.Difficulty)
		if err != nil {
			return nil, err
		}
		difficulty = string(raw)
	}
	return []any{
		s.ID.Hex(), s.Name, s.HostID.Hex(), s.MaxPlayers, s.IsPrivate, s.Password, s.IsActive,
		s.GameVersion, toMillis(s.CreatedAt), toMillis(s.LastUpdated),
		string(players), string(worldMap), string(sharedObjects), difficulty,
	}, nil
}

func (r *sqliteGameSessionRepository) FindByID(ctx context.Context, id primitive.ObjectID) (*GameSession, error) {
	return queryOne(ctx, r.db, scanSession, `SELECT `+sessionColumns+` FROM game_sessions WHERE id = ?`, id.Hex())
}

func (r *sqliteGameSessionRepository) FindActiveSessions(ctx context.Context) ([]GameSession, error) {
	return queryAll(ctx, r.db, scanSession, `SELECT `+sessionColumns+` FROM game_sessions WHERE is_active = 1`)
}

func (r *sqliteGameSessionRepository) Create(ctx context.Context, session *GameSession) error {
	prepareNewSession(session)
	args, err := sessionArgs(session)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx,
		`INSERT INTO game_sessions (`+sessionColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		args...)
	return err
}

func (r *sqliteGameSessionRepository) Update(ctx context.Context, session *GameSession) error {
	session.LastUpdated = time.Now()
	args, err := sessionArgs(session)
	if err != nil {
		return err
	}
	// Move the id from the first position to the WHERE clause.
	args = append(args[1:], args[0])
	_, err = r.db.ExecContext(ctx,
		`UPDATE game_sessions
		    SET name = ?, host_id = ?, max_players = ?, is_private = ?, password = ?, is_active = ?,
		        game_version = ?, created_at = ?, last_updated = ?, players = ?, world_map = ?, shared_objects = ?,
		        difficulty = ?
		  WHERE id = ?`,
		args...)
	return err
}

func (r *sqliteGameSessionRepository) Delete(ctx context.Context, id primitive.ObjectID) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM game_sessions WHERE id = ?`, id.Hex())
	return err
}

// --- Leaderboard ---

type sqliteLeaderboardRepository struct {
	db *sql.DB
}

const leaderboardColumns = `id, user_id, username, session_id, session_name, score, kills, deaths, created_at, updated_at`

func scanLeaderboardEntry(row rowScanner) (*LeaderboardEntry, error) {
	var (
		e                    LeaderboardEntry
		id, userID           string
		createdAt, updatedAt int64
	)
	if err := row.Scan(&id, &userID, &e.Username, &e.SessionID, &e.SessionName,
		&e.Score, &e.Kills, &e.Deaths, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	var err error
	if e.ID, err = parseObjectID(id); err != nil {
		return nil, err
	}
	if e.UserID, err = parseObjectID(userID); err != nil {
		return nil, err
	}
	e.CreatedAt = fromMillis(createdAt)
	e.UpdatedAt = fromMillis(updatedAt)
	return &e, nil
}

func (r *sqliteLeaderboardRepository) UpsertEntry(ctx context.Context, entry *LeaderboardEntry) error {
	now := toMillis(time.Now())
	// Mirrors the MongoDB upsert: $max on score/kills, $set on names,
	// $inc deaths, $setOnInsert created_at.
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO leaderboard (`+leaderboardColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, 1, ?, ?)
		ON CONFLICT (user_id, session_id) DO UPDATE SET
			score        = MAX(score, excluded.score),
			kills        = MAX(kills, excluded.kills),
			username     = excluded.username,
			session_name = excluded.session_name,
			deaths       = deaths + 1,
			updated_at   = excluded.updated_at`,
		primitive.NewObjectID().Hex(), entry.UserID.Hex(), entry.Username, entry.SessionID, entry.SessionName,
		entry.Score, entry.Kills, now, now,
	)
	return err
}

func (r *sqliteLeaderboardRepository) GetTopScores(ctx context.Context, limit int) ([]LeaderboardEntry, error) {
	return queryAll(ctx, r.db, scanLeaderboardEntry,
		`SELECT `+leaderboardColumns+` FROM leaderboard ORDER BY score DESC LIMIT ?`, limit)
}

func (r *sqliteLeaderboardRepository) GetTopScoresBySession(ctx context.Context, sessionID string, limit int) ([]LeaderboardEntry, error) {
	return queryAll(ctx, r.db, scanLeaderboardEntry,
		`SELECT `+leaderboardColumns+` FROM leaderboard WHERE session_id = ? ORDER BY score DESC LIMIT ?`, sessionID, limit)
}

func (r *sqliteLeaderboardRepository) GetUserStats(ctx context.Context, userID primitive.ObjectID) (*LeaderboardEntry, error) {
	return queryOne(ctx, r.db, scanLeaderboardEntry,
		`SELECT `+leaderboardColumns+` FROM leaderboard WHERE user_id = ? ORDER BY score DESC LIMIT 1`, userID.Hex())
}

func (r *sqliteLeaderboardRepository) GetUserSessionEntry(ctx context.Context, userID primitive.ObjectID, sessionID string) (*LeaderboardEntry, error) {
	return queryOne(ctx, r.db, scanLeaderboardEntry,
		`SELECT `+leaderboardColumns+` FROM leaderboard WHERE user_id = ? AND session_id = ?`, userID.Hex(), sessionID)
}
