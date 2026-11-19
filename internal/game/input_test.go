package game

import (
	"math"
	"testing"
	"time"

	"github.com/albert-tarly/dungerra-api/internal/config"
	"github.com/albert-tarly/dungerra-api/internal/types"
)

func TestMovementDirection(t *testing.T) {
	d := 1 / math.Sqrt2
	tests := []struct {
		name     string
		input    types.InputPayload
		rotation float64
		wx, wy   float64
	}{
		{"idle", types.InputPayload{}, 0, 0, 0},
		// Facing up the screen (-Y)
		{"facing up: forward", types.InputPayload{Forward: true}, 180, 0, -1},
		{"facing up: backward", types.InputPayload{Backward: true}, 180, 0, 1},
		{"facing up: strafe left", types.InputPayload{Left: true}, 180, -1, 0},
		{"facing up: strafe right", types.InputPayload{Right: true}, 180, 1, 0},
		{"facing up: forward-right is unit length", types.InputPayload{Forward: true, Right: true}, 180, d, -d},
		// Facing down the screen (+Y): left/right are mirrored on screen
		{"facing down: forward", types.InputPayload{Forward: true}, 0, 0, 1},
		{"facing down: strafe right", types.InputPayload{Right: true}, 0, -1, 0},
		// Facing left (-X): the player's right is up the screen
		{"facing left: forward", types.InputPayload{Forward: true}, 90, -1, 0},
		{"facing left: strafe right", types.InputPayload{Right: true}, 90, 0, -1},
		// Facing right (+X): the player's right is down the screen
		{"facing right: strafe right", types.InputPayload{Right: true}, 270, 0, 1},
		{"facing right: backward-left is unit length", types.InputPayload{Backward: true, Left: true}, 270, -d, -d},
		{"opposites cancel", types.InputPayload{Forward: true, Backward: true, Left: true, Right: true}, 45, 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			x, y := movementDirection(&tt.input, tt.rotation)
			if math.Abs(x-tt.wx) > 1e-9 || math.Abs(y-tt.wy) > 1e-9 {
				t.Errorf("got (%v, %v), want (%v, %v)", x, y, tt.wx, tt.wy)
			}
		})
	}
}

// newTickTestEngine returns an engine with one connected player and no walls or
// enemies around, so a tick's movement is deterministic.
func newTickTestEngine(t *testing.T) (*Engine, *types.Player) {
	t.Helper()
	prevConfig := config.AppConfig
	config.AppConfig = &config.Config{}
	t.Cleanup(func() { config.AppConfig = prevConfig })

	e := NewEngine("s")
	player := e.ConnectPlayer("p1", "tester")
	for key := range e.state.wallsByChunk {
		e.state.wallsByChunk[key] = map[string]*types.Wall{}
	}
	for key := range e.state.enemiesByChunk {
		e.state.enemiesByChunk[key] = map[string]*types.Enemy{}
	}
	return e, player
}

// tick runs one engine update with the given time step in seconds.
func tick(e *Engine, dt float64) {
	e.lastUpdate = time.Now().Add(-time.Duration(dt * float64(time.Second)))
	e.Update()
}

// The player turns toward the aim at the capped rate instead of snapping.
func TestUpdateTurnsTowardAimAtCappedRate(t *testing.T) {
	e, player := newTickTestEngine(t)
	player.Rotation = 0

	aim := 90.0
	e.UpdatePlayerInput("p1", types.InputPayload{AimRotation: &aim})
	const dt = 0.1
	tick(e, dt)

	// ~720°/s * 0.1 s = 72°, not snapped to 90°
	maxStep := config.PlayerRotationSpeed * dt
	if player.Rotation < maxStep-0.5 || player.Rotation >= aim {
		t.Errorf("Rotation = %v, want about %v (capped, below %v)", player.Rotation, maxStep, aim)
	}
}

// Strafing moves perpendicular to the facing, not along a fixed screen axis.
func TestUpdateStrafesRelativeToFacing(t *testing.T) {
	e, player := newTickTestEngine(t)
	player.Rotation = 90 // facing left (-X), so the player's right is -Y
	aim := 90.0
	startX, startY := player.Position.X, player.Position.Y

	e.UpdatePlayerInput("p1", types.InputPayload{Right: true, AimRotation: &aim})
	const dt = 0.1
	tick(e, dt)

	if dy := player.Position.Y - startY; dy > -config.PlayerSpeed*dt*0.9 {
		t.Errorf("moved dy = %v, want about %v", dy, -config.PlayerSpeed*dt)
	}
	if dx := player.Position.X - startX; math.Abs(dx) > 1e-9 {
		t.Errorf("moved dx = %v, want 0", dx)
	}
}

// A message without an aim (e.g. only a key changed) must not cancel an
// in-progress capped turn toward the last aim.
func TestUpdatePlayerInputKeepsLastAim(t *testing.T) {
	prevConfig := config.AppConfig
	config.AppConfig = &config.Config{}
	t.Cleanup(func() { config.AppConfig = prevConfig })

	e := NewEngine("s")
	aim := 135.0
	e.UpdatePlayerInput("p1", types.InputPayload{AimRotation: &aim})
	e.UpdatePlayerInput("p1", types.InputPayload{Forward: true})

	got := e.playerInputState["p1"].AimRotation
	if got == nil || *got != aim {
		t.Fatalf("AimRotation = %v, want %v", got, aim)
	}

	newAim := 10.0
	e.UpdatePlayerInput("p1", types.InputPayload{AimRotation: &newAim})
	if got := e.playerInputState["p1"].AimRotation; got == nil || *got != newAim {
		t.Fatalf("AimRotation = %v, want %v", got, newAim)
	}
}
