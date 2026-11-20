package db

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/albert-tarly/dungerra-api/internal/config"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// Every backend must pass the same contract. SQLite always runs; MongoDB runs
// when MONGODB_TEST_URL points at a disposable instance (it uses the real
// database name, so never point it at production).
func backends(t *testing.T) map[string]func(t *testing.T) Store {
	b := map[string]func(t *testing.T) Store{
		DriverSQLite: func(t *testing.T) Store {
			s, err := openSQLite(context.Background(), filepath.Join(t.TempDir(), "nested", "test.db"))
			if err != nil {
				t.Fatalf("open sqlite: %v", err)
			}
			t.Cleanup(func() { s.Close(context.Background()) })
			return s
		},
	}
	if url := os.Getenv("MONGODB_TEST_URL"); url != "" {
		b[DriverMongoDB] = func(t *testing.T) Store {
			s, err := openMongo(context.Background(), url)
			if err != nil {
				t.Fatalf("open mongo: %v", err)
			}
			t.Cleanup(func() { s.Close(context.Background()) })
			return s
		}
	}
	return b
}

func TestStoreContract(t *testing.T) {
	for name, open := range backends(t) {
		t.Run(name, func(t *testing.T) {
			t.Run("users", func(t *testing.T) { testUsers(t, open(t)) })
			t.Run("sessions", func(t *testing.T) { testSessions(t, open(t)) })
			t.Run("leaderboard", func(t *testing.T) { testLeaderboard(t, open(t)) })
		})
	}
}

func testUsers(t *testing.T, s Store) {
	ctx := context.Background()
	users := s.Users()

	if _, err := users.FindByID(ctx, primitive.NewObjectID()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("FindByID on missing user: want ErrNotFound, got %v", err)
	}

	if err := users.Create(ctx, &User{Username: "no-provider"}); err == nil {
		t.Fatal("Create without provider: want error")
	}

	gid := "g-" + primitive.NewObjectID().Hex()
	u := &User{Provider: ProviderGoogle, ProviderID: gid, Email: gid + "@example.com", GoogleID: gid, Username: "alice"}
	if err := users.Create(ctx, u); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if u.ID.IsZero() || !u.IsActive || u.CreatedAt.IsZero() {
		t.Fatalf("Create did not populate defaults: %+v", u)
	}

	got, err := users.FindByProvider(ctx, ProviderGoogle, gid)
	if err != nil || got.ID != u.ID || got.Username != "alice" || got.Email != u.Email {
		t.Fatalf("FindByProvider: %+v, %v", got, err)
	}
	if got.DisplayName() != "alice" {
		t.Fatalf("google DisplayName: %q", got.DisplayName())
	}

	// Same provider identity twice → ErrDuplicate.
	if err := users.Create(ctx, &User{Provider: ProviderGoogle, ProviderID: gid, Username: "dup"}); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("Create duplicate identity: want ErrDuplicate, got %v", err)
	}

	// Accounts are separated by provider: a local account with the same
	// username/provider ID is a different user. Local accounts have no email
	// or Google ID, and several of them must not collide on those indexes.
	localName := "alice-" + primitive.NewObjectID().Hex()[:8]
	local := &User{Provider: ProviderLocal, ProviderID: localName, Username: localName, PasswordHash: "hash"}
	if err := users.Create(ctx, local); err != nil {
		t.Fatalf("Create local: %v", err)
	}
	if err := users.Create(ctx, &User{Provider: ProviderLocal, ProviderID: gid, Username: "x", PasswordHash: "h"}); err != nil {
		t.Fatalf("Create local with google's provider ID: %v", err)
	}
	got, err = users.FindByProvider(ctx, ProviderLocal, localName)
	if err != nil || got.ID != local.ID || got.PasswordHash != "hash" || got.Email != "" {
		t.Fatalf("FindByProvider local: %+v, %v", got, err)
	}
	if got.DisplayName() != localName+LocalDisplaySuffix {
		t.Fatalf("local DisplayName: %q", got.DisplayName())
	}
	if _, err := users.FindByProvider(ctx, ProviderLocal, "missing-"+gid); !errors.Is(err, ErrNotFound) {
		t.Fatalf("FindByProvider missing: want ErrNotFound, got %v", err)
	}

	// Setting and then clearing CurrentSession must both persist.
	u.CurrentSession = "abc"
	if err := users.Update(ctx, u); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if got, _ := users.FindByID(ctx, u.ID); got.CurrentSession != "abc" {
		t.Fatalf("CurrentSession not set: %q", got.CurrentSession)
	}
	u.CurrentSession = ""
	if err := users.Update(ctx, u); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if got, _ := users.FindByID(ctx, u.ID); got.CurrentSession != "" {
		t.Fatalf("CurrentSession not cleared: %q", got.CurrentSession)
	}
}

func testSessions(t *testing.T, s Store) {
	ctx := context.Background()
	sessions := s.Sessions()

	sess := &GameSession{
		Name:       "test",
		HostID:     primitive.NewObjectID(),
		MaxPlayers: 4,
		IsPrivate:  true,
		Password:   "secret",
	}
	if err := sessions.Create(ctx, sess); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if sess.ID.IsZero() || !sess.IsActive || sess.GameVersion == "" || sess.Players == nil {
		t.Fatalf("Create did not populate defaults: %+v", sess)
	}

	sess.Players["p1"] = PlayerState{
		PlayerID:                "p1",
		Name:                    "alice",
		Position:                Position{X: 1.5, Y: -2, Rotation: 90},
		Lives:                   3.5,
		Score:                   42,
		BulletsLeftByWeaponType: map[string]int32{"blaster": 6},
		Inventory:               []InventoryItem{{Type: 1, Quantity: 2}},
		IsAlive:                 true,
	}
	sess.WorldMap["0,0"] = Chunk{ChunkID: "0,0", Objects: map[string]WorldObject{}}
	sess.SharedObjects["w1"] = WorldObject{
		ObjectID: "w1", Type: "wall", X: 10, Y: 20,
		Properties: map[string]interface{}{"width": 30.0, "orientation": "vertical"},
	}
	if err := sessions.Update(ctx, sess); err != nil {
		t.Fatalf("Update: %v", err)
	}

	got, err := sessions.FindByID(ctx, sess.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got.Password != "secret" || got.HostID != sess.HostID || got.MaxPlayers != 4 || !got.IsPrivate {
		t.Fatalf("scalar fields not round-tripped: %+v", got)
	}
	p := got.Players["p1"]
	if p.Score != 42 || p.Position.X != 1.5 || p.Lives != 3.5 || p.BulletsLeftByWeaponType["blaster"] != 6 || len(p.Inventory) != 1 {
		t.Fatalf("player not round-tripped: %+v", p)
	}
	w := got.SharedObjects["w1"]
	if w.Properties["width"] != 30.0 || w.Properties["orientation"] != "vertical" {
		t.Fatalf("world object not round-tripped: %+v", w)
	}
	if _, ok := got.WorldMap["0,0"]; !ok {
		t.Fatal("world map not round-tripped")
	}

	// A session without a difficulty (created before difficulties) stays
	// without one and plays as Hard; a stored difficulty round-trips intact.
	if got.Difficulty != nil || got.EffectiveDifficulty().Preset != config.DifficultyHard {
		t.Fatalf("legacy session difficulty: %+v", got.Difficulty)
	}
	easy, _ := config.DifficultyPreset(config.DifficultyEasy)
	withDifficulty := &GameSession{Name: "easy", HostID: primitive.NewObjectID(), MaxPlayers: 4, Difficulty: &easy}
	if err := sessions.Create(ctx, withDifficulty); err != nil {
		t.Fatalf("Create with difficulty: %v", err)
	}
	if err := sessions.Update(ctx, withDifficulty); err != nil {
		t.Fatalf("Update with difficulty: %v", err)
	}
	gotDifficulty, err := sessions.FindByID(ctx, withDifficulty.ID)
	if err != nil || gotDifficulty.Difficulty == nil || *gotDifficulty.Difficulty != easy {
		t.Fatalf("difficulty not round-tripped: %+v (err=%v)", gotDifficulty, err)
	}

	active, err := sessions.FindActiveSessions(ctx)
	if err != nil || !containsSession(active, sess.ID) || !containsSession(active, withDifficulty.ID) {
		t.Fatalf("FindActiveSessions: missing created session (err=%v)", err)
	}

	if err := sessions.Delete(ctx, sess.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := sessions.FindByID(ctx, sess.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("FindByID after delete: want ErrNotFound, got %v", err)
	}
}

func containsSession(list []GameSession, id primitive.ObjectID) bool {
	for _, s := range list {
		if s.ID == id {
			return true
		}
	}
	return false
}

func testLeaderboard(t *testing.T, s Store) {
	ctx := context.Background()
	lb := s.Leaderboard()
	user := primitive.NewObjectID()
	session := "session-" + primitive.NewObjectID().Hex()

	// Three deaths: best score/kills must be kept, deaths counted.
	for _, r := range []struct{ score, kills int }{{100, 5}, {50, 9}, {200, 1}} {
		err := lb.UpsertEntry(ctx, &LeaderboardEntry{
			UserID: user, Username: "alice", SessionID: session, SessionName: "s", Score: r.score, Kills: r.kills,
		})
		if err != nil {
			t.Fatalf("UpsertEntry: %v", err)
		}
	}

	e, err := lb.GetUserSessionEntry(ctx, user, session)
	if err != nil {
		t.Fatalf("GetUserSessionEntry: %v", err)
	}
	if e.Score != 200 || e.Kills != 9 || e.Deaths != 3 || e.CreatedAt.IsZero() {
		t.Fatalf("upsert semantics wrong: %+v", e)
	}

	other := primitive.NewObjectID()
	if err := lb.UpsertEntry(ctx, &LeaderboardEntry{UserID: other, Username: "bob", SessionID: session, Score: 300}); err != nil {
		t.Fatalf("UpsertEntry: %v", err)
	}

	top, err := lb.GetTopScoresBySession(ctx, session, 10)
	if err != nil || len(top) != 2 || top[0].UserID != other || top[1].UserID != user {
		t.Fatalf("GetTopScoresBySession: %+v, %v", top, err)
	}
	if top, err := lb.GetTopScoresBySession(ctx, session, 1); err != nil || len(top) != 1 {
		t.Fatalf("GetTopScoresBySession limit: %+v, %v", top, err)
	}
	if _, err := lb.GetTopScores(ctx, 10); err != nil {
		t.Fatalf("GetTopScores: %v", err)
	}
	if stats, err := lb.GetUserStats(ctx, user); err != nil || stats.Score != 200 {
		t.Fatalf("GetUserStats: %+v, %v", stats, err)
	}
	if _, err := lb.GetUserStats(ctx, primitive.NewObjectID()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetUserStats on missing user: want ErrNotFound, got %v", err)
	}
}

func TestSQLiteReopenKeepsData(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "test.db")

	s, err := openSQLite(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	u := &User{Provider: ProviderLocal, ProviderID: "a", Username: "a"}
	err = s.Users().Create(ctx, u)
	s.Close(ctx)
	if err != nil {
		t.Fatal(err)
	}

	// Reopening must not re-run migrations and must see the data.
	s, err = openSQLite(ctx, path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s.Close(ctx)
	if _, err := s.Users().FindByID(ctx, u.ID); err != nil {
		t.Fatalf("data lost after reopen: %v", err)
	}
}
