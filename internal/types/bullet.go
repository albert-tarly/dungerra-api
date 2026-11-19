package types

import (
	"time"

	"github.com/albert-tarly/dungerra-api/internal/config"
	"github.com/albert-tarly/dungerra-api/internal/utils"
)

// Bullet represents a projectile in the game
type Bullet struct {
	ScreenObject
	BatchID       string    `json:"batchId"`
	Velocity      *Vector2  `json:"velocity"`
	OwnerID       string    `json:"ownerId"`
	IsEnemy       bool      `json:"isEnemy"`
	EnemyType     string    `json:"enemyType"`
	IsActive      bool      `json:"isActive"`
	DeletedAt     time.Time `json:"deletedAt"`
	SpawnTime     time.Time `json:"-"`
	IsJustSpawned bool      `json:"-"`
	Damage        float32   `json:"damage"`
	WeaponType    string    `json:"weaponType"`
}

func (b *Bullet) IsVisibleToPlayer(player *Player) bool {
	if b.WeaponType == WeaponTypeRailgun {
		closestPointX, closestPointY := utils.ClosestPointOnLineSegment(
			b.Position.X,
			b.Position.Y,
			b.Position.X+b.Velocity.X,
			b.Position.Y+b.Velocity.Y,
			player.Position.X,
			player.Position.Y,
		)
		return player.DistanceToPoint(&Vector2{X: closestPointX, Y: closestPointY}) <= config.SightRadius
	}

	if player.NightVisionTimer > 0 {
		return b.DistanceToPoint(player.Position) <= config.SightRadius
	}

	detectionPoint, detectionDistance := player.DetectionParams()
	if b.WeaponType == WeaponTypeRocketLauncher && !b.IsActive {
		detectionDistance = config.SightRadius
	}

	distance := b.DistanceToPoint(detectionPoint)
	return distance <= detectionDistance
}

func (b *Bullet) Clone() *Bullet {
	clone := *b
	clone.Position = &Vector2{X: b.Position.X, Y: b.Position.Y}
	clone.Velocity = &Vector2{X: b.Velocity.X, Y: b.Velocity.Y}
	return &clone
}
