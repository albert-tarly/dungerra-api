package db

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const mongoDatabaseName = "dungeon_game"

type mongoStore struct {
	client      *mongo.Client
	users       *mongoUserRepository
	sessions    *mongoGameSessionRepository
	leaderboard *mongoLeaderboardRepository
}

func openMongo(ctx context.Context, url string) (*mongoStore, error) {
	if url == "" {
		return nil, errors.New("db: MONGODB_URL is required for the mongodb driver")
	}

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(url))
	if err != nil {
		return nil, err
	}
	if err := client.Ping(ctx, nil); err != nil {
		_ = client.Disconnect(ctx)
		return nil, err
	}

	database := client.Database(mongoDatabaseName)
	// Unlike missing indexes, a failed user migration would break logins for
	// existing accounts, so it is fatal.
	if err := migrateMongoUsers(ctx, database.Collection("users")); err != nil {
		_ = client.Disconnect(ctx)
		return nil, fmt.Errorf("db: migrate users: %w", err)
	}
	if err := createMongoIndexes(ctx, database); err != nil {
		log.Printf("Warning: Failed to create indexes: %v", err)
	}

	return &mongoStore{
		client:      client,
		users:       &mongoUserRepository{collection: database.Collection("users")},
		sessions:    &mongoGameSessionRepository{collection: database.Collection("game_sessions")},
		leaderboard: &mongoLeaderboardRepository{collection: database.Collection("leaderboard")},
	}, nil
}

func (s *mongoStore) Users() UserRepository              { return s.users }
func (s *mongoStore) Sessions() GameSessionRepository    { return s.sessions }
func (s *mongoStore) Leaderboard() LeaderboardRepository { return s.leaderboard }
func (s *mongoStore) Close(ctx context.Context) error    { return s.client.Disconnect(ctx) }

// migrateMongoUsers brings user documents created before multi-provider auth
// up to date. It is idempotent and safe to run on every start.
func migrateMongoUsers(ctx context.Context, users *mongo.Collection) error {
	// 1. Pre-existing accounts are all Google accounts: give them a provider identity.
	res, err := users.UpdateMany(ctx,
		bson.M{
			"provider":  bson.M{"$exists": false},
			"google_id": bson.M{"$exists": true, "$nin": bson.A{nil, ""}},
		},
		mongo.Pipeline{{{Key: "$set", Value: bson.D{
			{Key: "provider", Value: ProviderGoogle},
			{Key: "provider_id", Value: "$google_id"},
		}}}},
	)
	if err != nil {
		return err
	}
	if res.ModifiedCount > 0 {
		log.Printf("Migrated %d existing users to provider %q", res.ModifiedCount, ProviderGoogle)
	}

	// 2. Email is no longer an identity (local accounts have none, and accounts
	// are separated by provider), so drop the legacy unique email index.
	if _, err := users.Indexes().DropOne(ctx, "email_1"); err != nil {
		var cmdErr mongo.CommandError
		// 26 = NamespaceNotFound, 27 = IndexNotFound: nothing to drop.
		if !errors.As(err, &cmdErr) || (cmdErr.Code != 26 && cmdErr.Code != 27) {
			return fmt.Errorf("drop legacy email index: %w", err)
		}
	}
	return nil
}

func createMongoIndexes(ctx context.Context, database *mongo.Database) error {
	_, err := database.Collection("users").Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			// Account identity. Partial so that any stray legacy document
			// without a provider can't block index creation.
			Keys: bson.D{{Key: "provider", Value: 1}, {Key: "provider_id", Value: 1}},
			Options: options.Index().SetUnique(true).
				SetPartialFilterExpression(bson.M{"provider": bson.M{"$exists": true}}),
		},
		{
			Keys:    bson.D{{Key: "google_id", Value: 1}},
			Options: options.Index().SetUnique(true).SetSparse(true),
		},
	})
	if err != nil {
		return err
	}

	_, err = database.Collection("game_sessions").Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "host_id", Value: 1}}},
		{Keys: bson.D{{Key: "is_active", Value: 1}}},
	})
	if err != nil {
		return err
	}

	_, err = database.Collection("leaderboard").Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "user_id", Value: 1}, {Key: "session_id", Value: 1}},
			Options: options.Index().SetUnique(true), // Unique per user per session
		},
		{Keys: bson.D{{Key: "score", Value: -1}}},                                  // Global leaderboard
		{Keys: bson.D{{Key: "session_id", Value: 1}, {Key: "score", Value: -1}}},   // Per-session leaderboards
		{Keys: bson.D{{Key: "user_id", Value: 1}, {Key: "updated_at", Value: -1}}}, // User history
	})
	return err
}

// mapMongoErr converts driver-specific errors into backend-neutral ones.
func mapMongoErr(err error) error {
	if errors.Is(err, mongo.ErrNoDocuments) {
		return ErrNotFound
	}
	return err
}

func mongoFindOne[T any](ctx context.Context, c *mongo.Collection, filter interface{}, opts ...*options.FindOneOptions) (*T, error) {
	var out T
	if err := c.FindOne(ctx, filter, opts...).Decode(&out); err != nil {
		return nil, mapMongoErr(err)
	}
	return &out, nil
}

func mongoFindAll[T any](ctx context.Context, c *mongo.Collection, filter interface{}, opts ...*options.FindOptions) ([]T, error) {
	cursor, err := c.Find(ctx, filter, opts...)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var out []T
	if err := cursor.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// --- Users ---

type mongoUserRepository struct {
	collection *mongo.Collection
}

func (r *mongoUserRepository) FindByProvider(ctx context.Context, provider, providerID string) (*User, error) {
	return mongoFindOne[User](ctx, r.collection, bson.M{"provider": provider, "provider_id": providerID})
}

func (r *mongoUserRepository) FindByID(ctx context.Context, id primitive.ObjectID) (*User, error) {
	return mongoFindOne[User](ctx, r.collection, bson.M{"_id": id})
}

func (r *mongoUserRepository) Create(ctx context.Context, user *User) error {
	if err := prepareNewUser(user); err != nil {
		return err
	}
	_, err := r.collection.InsertOne(ctx, user)
	if mongo.IsDuplicateKeyError(err) {
		return ErrDuplicate
	}
	return err
}

// Update replaces the stored user with the given one. A full replace (rather
// than $set) is used so that emptied omitempty fields such as CurrentSession
// are actually cleared, matching the SQLite backend.
func (r *mongoUserRepository) Update(ctx context.Context, user *User) error {
	_, err := r.collection.ReplaceOne(ctx, bson.M{"_id": user.ID}, user)
	return err
}

// --- Game sessions ---

type mongoGameSessionRepository struct {
	collection *mongo.Collection
}

func (r *mongoGameSessionRepository) FindByID(ctx context.Context, id primitive.ObjectID) (*GameSession, error) {
	return mongoFindOne[GameSession](ctx, r.collection, bson.M{"_id": id})
}

func (r *mongoGameSessionRepository) FindActiveSessions(ctx context.Context) ([]GameSession, error) {
	return mongoFindAll[GameSession](ctx, r.collection, bson.M{"is_active": true})
}

func (r *mongoGameSessionRepository) Create(ctx context.Context, session *GameSession) error {
	prepareNewSession(session)
	_, err := r.collection.InsertOne(ctx, session)
	return err
}

func (r *mongoGameSessionRepository) Update(ctx context.Context, session *GameSession) error {
	session.LastUpdated = time.Now()
	_, err := r.collection.UpdateOne(ctx, bson.M{"_id": session.ID}, bson.M{"$set": session})
	return err
}

func (r *mongoGameSessionRepository) Delete(ctx context.Context, id primitive.ObjectID) error {
	_, err := r.collection.DeleteOne(ctx, bson.M{"_id": id})
	return err
}

// --- Leaderboard ---

type mongoLeaderboardRepository struct {
	collection *mongo.Collection
}

func (r *mongoLeaderboardRepository) UpsertEntry(ctx context.Context, entry *LeaderboardEntry) error {
	now := time.Now()
	filter := bson.M{
		"user_id":    entry.UserID,
		"session_id": entry.SessionID,
	}
	update := bson.M{
		"$max": bson.M{
			"score": entry.Score, // Only update if new score is higher
			"kills": entry.Kills, // Only update if new kills is higher
		},
		"$set": bson.M{
			"username":     entry.Username,
			"session_name": entry.SessionName,
			"updated_at":   now,
		},
		"$inc":         bson.M{"deaths": 1},
		"$setOnInsert": bson.M{"created_at": now},
	}

	_, err := r.collection.UpdateOne(ctx, filter, update, options.Update().SetUpsert(true))
	return err
}

func (r *mongoLeaderboardRepository) GetTopScores(ctx context.Context, limit int) ([]LeaderboardEntry, error) {
	opts := options.Find().SetSort(bson.D{{Key: "score", Value: -1}}).SetLimit(int64(limit))
	return mongoFindAll[LeaderboardEntry](ctx, r.collection, bson.M{}, opts)
}

func (r *mongoLeaderboardRepository) GetTopScoresBySession(ctx context.Context, sessionID string, limit int) ([]LeaderboardEntry, error) {
	opts := options.Find().SetSort(bson.D{{Key: "score", Value: -1}}).SetLimit(int64(limit))
	return mongoFindAll[LeaderboardEntry](ctx, r.collection, bson.M{"session_id": sessionID}, opts)
}

func (r *mongoLeaderboardRepository) GetUserStats(ctx context.Context, userID primitive.ObjectID) (*LeaderboardEntry, error) {
	opts := options.FindOne().SetSort(bson.D{{Key: "score", Value: -1}})
	return mongoFindOne[LeaderboardEntry](ctx, r.collection, bson.M{"user_id": userID}, opts)
}

func (r *mongoLeaderboardRepository) GetUserSessionEntry(ctx context.Context, userID primitive.ObjectID, sessionID string) (*LeaderboardEntry, error) {
	return mongoFindOne[LeaderboardEntry](ctx, r.collection, bson.M{
		"user_id":    userID,
		"session_id": sessionID,
	})
}
