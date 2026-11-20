package db

import (
	"time"

	"github.com/albert-tarly/dungerra-api/internal/config"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// Models are shared by all storage backends. The bson tags drive the MongoDB
// backend; the json tags are used both by the HTTP API and by the SQLite
// backend for the nested documents it stores as JSON.
//
// primitive.ObjectID is used purely as a compact, globally unique,
// client-generated ID; the SQLite backend stores it as its hex string.

// Authentication providers. A user account belongs to exactly one provider and
// is identified by (Provider, ProviderID); accounts of different providers are
// never merged, even if they share an email or username.
const (
	ProviderGoogle = "google" // ProviderID = Google account ID
	ProviderLocal  = "local"  // ProviderID = lower-cased username
)

// LocalDisplaySuffix marks local accounts wherever names are shown to players,
// so "alice" (Google) and "alice@local" can't be confused.
const LocalDisplaySuffix = "@local"

// User represents a user in the database
type User struct {
	ID             primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	Provider       string             `bson:"provider" json:"provider"`
	ProviderID     string             `bson:"provider_id" json:"-"`
	Email          string             `bson:"email,omitempty" json:"email,omitempty"`
	GoogleID       string             `bson:"google_id,omitempty" json:"google_id,omitempty"` // kept for Google accounts for backward compatibility
	Username       string             `bson:"username" json:"username"`
	PasswordHash   string             `bson:"password_hash,omitempty" json:"-"` // bcrypt; local accounts only
	IsActive       bool               `bson:"is_active" json:"is_active"`
	CreatedAt      time.Time          `bson:"created_at" json:"created_at"`
	CurrentSession string             `bson:"current_session,omitempty" json:"current_session,omitempty"`
}

// DisplayName is the name shown to other players (in-game, leaderboards).
func (u *User) DisplayName() string {
	if u.Provider == ProviderLocal {
		return u.Username + LocalDisplaySuffix
	}
	return u.Username
}

type InventoryItem struct {
	Type     int32 `bson:"type" json:"type"`
	Quantity int32 `bson:"quantity" json:"quantity"`
}

// PlayerState represents a player's state in a game session
type PlayerState struct {
	PlayerID                string           `bson:"player_id" json:"player_id"`
	Name                    string           `bson:"name" json:"name"`
	Position                Position         `bson:"position" json:"position"`
	Lives                   float32          `bson:"lives" json:"lives"`
	Score                   int              `bson:"score" json:"score"`
	Money                   int              `bson:"money" json:"money"`
	Kills                   int              `bson:"kills" json:"kills"`
	BulletsLeftByWeaponType map[string]int32 `bson:"bullets_left_by_weapon_type" json:"bullets_left_by_weapon_type"`
	InvulnerableTimer       float64          `bson:"invulnerable_timer" json:"invulnerable_timer"`
	NightVisionTimer        float64          `bson:"night_vision_timer" json:"night_vision_timer"`
	IsAlive                 bool             `bson:"is_alive" json:"is_alive"`
	IsConnected             bool             `bson:"is_connected" json:"is_connected"`
	LastUpdated             time.Time        `bson:"last_updated" json:"last_updated"`
	Inventory               []InventoryItem  `bson:"inventory" json:"inventory"`
	SelectedGunType         string           `bson:"selected_gun_type" json:"selected_gun_type"`
}

// Position represents x, y coordinates and rotation
type Position struct {
	X        float64 `bson:"x" json:"x"`
	Y        float64 `bson:"y" json:"y"`
	Rotation float64 `bson:"rotation" json:"rotation"`
}

// WorldObject represents an object in the game world
type WorldObject struct {
	ObjectID   string                 `bson:"object_id" json:"object_id"`
	Type       string                 `bson:"type" json:"type"` // wall, enemy, bonus, bullet
	X          float64                `bson:"x" json:"x"`
	Y          float64                `bson:"y" json:"y"`
	Properties map[string]interface{} `bson:"properties,omitempty" json:"properties,omitempty"`
	OwnerID    string                 `bson:"owner_id,omitempty" json:"owner_id,omitempty"`
}

// Chunk represents a chunk of the game world
type Chunk struct {
	ChunkID string                 `bson:"chunk_id" json:"chunk_id"`
	X       int                    `bson:"x" json:"x"`
	Y       int                    `bson:"y" json:"y"`
	Objects map[string]WorldObject `bson:"objects" json:"objects"`
}

// GameSession represents a multiplayer game session
type GameSession struct {
	ID            primitive.ObjectID     `bson:"_id,omitempty" json:"id"`
	Name          string                 `bson:"name" json:"name"`
	HostID        primitive.ObjectID     `bson:"host_id" json:"host_id"`
	Players       map[string]PlayerState `bson:"players" json:"players"`
	MaxPlayers    int                    `bson:"max_players" json:"max_players"`
	IsPrivate     bool                   `bson:"is_private" json:"is_private"`
	Password      string                 `bson:"password,omitempty" json:"-"`
	WorldMap      map[string]Chunk       `bson:"world_map" json:"world_map"`
	SharedObjects map[string]WorldObject `bson:"shared_objects" json:"shared_objects"`
	CreatedAt     time.Time              `bson:"created_at" json:"created_at"`
	LastUpdated   time.Time              `bson:"last_updated" json:"last_updated"`
	IsActive      bool                   `bson:"is_active" json:"is_active"`
	GameVersion   string                 `bson:"game_version" json:"game_version"`
	// Difficulty is nil for sessions created before difficulties existed;
	// those play as Hard (see EffectiveDifficulty).
	Difficulty *config.Difficulty `bson:"difficulty,omitempty" json:"difficulty,omitempty"`
}

// EffectiveDifficulty is the difficulty the session plays at.
func (s *GameSession) EffectiveDifficulty() config.Difficulty {
	if s.Difficulty == nil {
		return config.HardDifficulty()
	}
	return *s.Difficulty
}

// LeaderboardEntry is a user's best result within a session.
type LeaderboardEntry struct {
	ID          primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	UserID      primitive.ObjectID `bson:"user_id" json:"user_id"`
	Username    string             `bson:"username" json:"username"`
	SessionID   string             `bson:"session_id" json:"session_id"`
	SessionName string             `bson:"session_name" json:"session_name"`
	Score       int                `bson:"score" json:"score"`
	Kills       int                `bson:"kills" json:"kills"`
	Deaths      int                `bson:"deaths" json:"deaths"`
	CreatedAt   time.Time          `bson:"created_at" json:"created_at"`
	UpdatedAt   time.Time          `bson:"updated_at" json:"updated_at"`
}
