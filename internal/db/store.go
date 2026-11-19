package db

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/besuhoff/dungerra/internal/config"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// ErrNotFound is returned by every repository when the requested record does
// not exist, regardless of the storage backend in use.
var ErrNotFound = errors.New("db: record not found")

// ErrDuplicate is returned when an insert violates a uniqueness constraint
// (e.g. a local username that is already taken).
var ErrDuplicate = errors.New("db: duplicate record")

// Supported storage backends.
const (
	DriverMongoDB = "mongodb"
	DriverSQLite  = "sqlite"
)

// UserRepository provides persistence for users.
type UserRepository interface {
	// FindByProvider looks up an account by its provider identity.
	FindByProvider(ctx context.Context, provider, providerID string) (*User, error)
	FindByID(ctx context.Context, id primitive.ObjectID) (*User, error)
	// Create inserts a new user; it returns ErrDuplicate if an account with
	// the same (Provider, ProviderID) already exists.
	Create(ctx context.Context, user *User) error
	Update(ctx context.Context, user *User) error
}

// GameSessionRepository provides persistence for game sessions.
type GameSessionRepository interface {
	FindByID(ctx context.Context, id primitive.ObjectID) (*GameSession, error)
	FindActiveSessions(ctx context.Context) ([]GameSession, error)
	Create(ctx context.Context, session *GameSession) error
	Update(ctx context.Context, session *GameSession) error
	Delete(ctx context.Context, id primitive.ObjectID) error
}

// LeaderboardRepository provides persistence for leaderboard entries.
type LeaderboardRepository interface {
	// UpsertEntry records a death for the user in the session, keeping the
	// highest score/kills seen so far and incrementing the death counter.
	UpsertEntry(ctx context.Context, entry *LeaderboardEntry) error
	GetTopScores(ctx context.Context, limit int) ([]LeaderboardEntry, error)
	GetTopScoresBySession(ctx context.Context, sessionID string, limit int) ([]LeaderboardEntry, error)
	GetUserStats(ctx context.Context, userID primitive.ObjectID) (*LeaderboardEntry, error)
	GetUserSessionEntry(ctx context.Context, userID primitive.ObjectID, sessionID string) (*LeaderboardEntry, error)
}

// Store is a storage backend.
type Store interface {
	Users() UserRepository
	Sessions() GameSessionRepository
	Leaderboard() LeaderboardRepository
	Close(ctx context.Context) error
}

// Options selects and configures the storage backend.
type Options struct {
	Driver     string // DriverMongoDB or DriverSQLite
	MongoDBURL string
	SQLitePath string
}

var current Store

// Open initialises the configured storage backend and makes it the active one.
func Open(opts Options) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var (
		store Store
		err   error
	)
	switch opts.Driver {
	case DriverMongoDB:
		store, err = openMongo(ctx, opts.MongoDBURL)
	case DriverSQLite:
		store, err = openSQLite(ctx, opts.SQLitePath)
	default:
		return fmt.Errorf("db: unknown driver %q (expected %q or %q)", opts.Driver, DriverMongoDB, DriverSQLite)
	}
	if err != nil {
		return err
	}

	current = store
	log.Printf("Database ready (driver: %s)", opts.Driver)
	return nil
}

// Close shuts down the active storage backend.
func Close() error {
	if current == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err := current.Close(ctx)
	current = nil
	return err
}

func mustCurrent() Store {
	if current == nil {
		panic("db: Open must be called before using repositories")
	}
	return current
}

// NewUserRepository returns the user repository of the active backend.
func NewUserRepository() UserRepository { return mustCurrent().Users() }

// NewGameSessionRepository returns the game session repository of the active backend.
func NewGameSessionRepository() GameSessionRepository { return mustCurrent().Sessions() }

// NewLeaderboardRepository returns the leaderboard repository of the active backend.
func NewLeaderboardRepository() LeaderboardRepository { return mustCurrent().Leaderboard() }

// Backend-independent defaults applied on insert, so that every backend
// behaves identically.

func prepareNewUser(user *User) error {
	if user.Provider == "" || user.ProviderID == "" {
		return errors.New("db: user must have a provider and provider ID")
	}
	if user.ID.IsZero() {
		user.ID = primitive.NewObjectID()
	}
	user.CreatedAt = time.Now()
	user.IsActive = true
	return nil
}

func prepareNewSession(session *GameSession) {
	if session.ID.IsZero() {
		session.ID = primitive.NewObjectID()
	}
	now := time.Now()
	session.CreatedAt = now
	session.LastUpdated = now
	session.GameVersion = config.GameVersion
	session.IsActive = true

	if session.Players == nil {
		session.Players = make(map[string]PlayerState)
	}
	if session.WorldMap == nil {
		session.WorldMap = make(map[string]Chunk)
	}
	if session.SharedObjects == nil {
		session.SharedObjects = make(map[string]WorldObject)
	}
}
