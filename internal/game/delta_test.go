package game

import (
	"testing"
	"time"

	"github.com/albert-tarly/dungerra-api/internal/config"
	"github.com/albert-tarly/dungerra-api/internal/types"
)

// addTestBullet puts an enemy blaster bullet next to the player, inside the
// torch radius, flying away from them.
func addTestBullet(e *Engine, player *types.Player, id string) *types.Bullet {
	b := &types.Bullet{
		ScreenObject: types.ScreenObject{ID: id, Position: &types.Vector2{X: player.Position.X + 50, Y: player.Position.Y}},
		Velocity:     &types.Vector2{X: config.BlasterBulletSpeed, Y: 0},
		OwnerID:      "enemy",
		IsEnemy:      true,
		IsActive:     true,
		SpawnTime:    time.Now(),
		Damage:       1,
		WeaponType:   types.WeaponTypeBlaster,
	}
	e.state.bullets[id] = b
	return b
}

// A delta that could not be queued must not count as delivered: the next delta
// has to carry its changes again, or the client misses them for good.
func TestUncommittedDeltaIsRepeated(t *testing.T) {
	e, player := newTickTestEngine(t)

	// Bring the client up to date with the world
	delta, view := e.GetGameStateDeltaForPlayer(player.ID)
	if _, ok := delta.AddedPlayers[player.ID]; !ok {
		t.Fatalf("first delta does not add the player")
	}
	e.CommitPlayerView(player.ID, view)

	addTestBullet(e, player, "b1")

	// This delta is "dropped": not committed
	delta, _ = e.GetGameStateDeltaForPlayer(player.ID)
	if _, ok := delta.AddedBullets["b1"]; !ok {
		t.Fatalf("delta does not add the new bullet")
	}

	delta, view = e.GetGameStateDeltaForPlayer(player.ID)
	if _, ok := delta.AddedBullets["b1"]; !ok {
		t.Fatalf("bullet from the dropped delta is not repeated")
	}
	e.CommitPlayerView(player.ID, view)

	delta, _ = e.GetGameStateDeltaForPlayer(player.ID)
	if _, ok := delta.AddedBullets["b1"]; ok {
		t.Fatalf("bullet is added again after the delta was committed")
	}
}

// Computing a delta must leave the committed view alone, including the
// entries the delta pass matches against.
func TestDeltaDoesNotConsumeCommittedView(t *testing.T) {
	e, player := newTickTestEngine(t)
	_, view := e.GetGameStateDeltaForPlayer(player.ID)
	e.CommitPlayerView(player.ID, view)

	before := 0
	for _, shops := range e.prevState[player.ID].shopsByChunk {
		before += len(shops)
	}
	beforeWalls := 0
	for _, walls := range e.prevState[player.ID].wallsByChunk {
		beforeWalls += len(walls)
	}

	for i := 0; i < 3; i++ {
		delta, _ := e.GetGameStateDeltaForPlayer(player.ID)
		if len(delta.RemovedShops) != 0 || len(delta.RemovedWalls) != 0 || len(delta.RemovedEnemies) != 0 {
			t.Fatalf("pass %d reports removals for an unchanged world: %+v", i, delta)
		}
	}

	after := 0
	for _, shops := range e.prevState[player.ID].shopsByChunk {
		after += len(shops)
	}
	afterWalls := 0
	for _, walls := range e.prevState[player.ID].wallsByChunk {
		afterWalls += len(walls)
	}
	if before != after || beforeWalls != afterWalls {
		t.Fatalf("committed view changed: shops %d → %d, walls %d → %d", before, after, beforeWalls, afterWalls)
	}
}

// Bullets in flight are moved by clients from their velocity, so the server
// only announces them when they appear and when they end.
func TestBulletsInFlightAreNotUpdated(t *testing.T) {
	e, player := newTickTestEngine(t)
	_, view := e.GetGameStateDeltaForPlayer(player.ID)
	e.CommitPlayerView(player.ID, view)

	b := addTestBullet(e, player, "b1")
	delta, view := e.GetGameStateDeltaForPlayer(player.ID)
	if _, ok := delta.AddedBullets["b1"]; !ok {
		t.Fatalf("bullet not added")
	}
	e.CommitPlayerView(player.ID, view)

	tick(e, 0.05)
	if b.Position.X == player.Position.X+50 {
		t.Fatalf("bullet did not move")
	}

	delta, view = e.GetGameStateDeltaForPlayer(player.ID)
	if len(delta.UpdatedBullets) != 0 {
		t.Errorf("in-flight bullet got a position update: %v", delta.UpdatedBullets)
	}
	if _, ok := delta.RemovedBullets["b1"]; ok {
		t.Errorf("in-flight bullet was removed")
	}
	e.CommitPlayerView(player.ID, view)

	// When it ends, the full bullet is sent as a removal for impact effects
	b.IsActive = false
	b.DeletedAt = time.Now()
	delta, _ = e.GetGameStateDeltaForPlayer(player.ID)
	removed, ok := delta.RemovedBullets["b1"]
	if !ok {
		t.Fatalf("spent bullet not removed")
	}
	if removed.IsActive || removed.WeaponType != types.WeaponTypeBlaster || removed.Position == nil || removed.DeletedAt == 0 {
		t.Errorf("removal lacks impact data: %+v", removed)
	}
}
