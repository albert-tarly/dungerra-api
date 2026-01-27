package types

import (
	"github.com/besuhoff/dungeon-game-go/internal/config"
	"github.com/besuhoff/dungeon-game-go/internal/utils"
)

type Ray struct {
	ScreenObject
	OwnerID     string   `json:"owner_id"`
	EndPosition *Vector2 `json:"end_position"`
	Type        string   `json:"type"`
}

func (r *Ray) IsVisibleToPlayer(player *Player) bool {
	closestPointX, closestPointY := utils.ClosestPointOnLineSegment(r.Position.X, r.Position.Y, r.EndPosition.X, r.EndPosition.Y, player.Position.X, player.Position.Y)
	return player.DistanceToPoint(&Vector2{X: closestPointX, Y: closestPointY}) <= config.SightRadius
}

func (r *Ray) Clone() *Ray {
	return &Ray{
		ScreenObject: ScreenObject{
			ID:       r.ID,
			Position: r.Position.Clone(),
		},
		OwnerID:     r.OwnerID,
		EndPosition: r.EndPosition.Clone(),
		Type:        r.Type,
	}
}
