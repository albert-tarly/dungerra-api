package game

import (
	"math"
	"testing"

	"github.com/albert-tarly/dungerra-api/internal/config"
	"github.com/albert-tarly/dungerra-api/internal/types"
)

func TestMovementDirection(t *testing.T) {
	d := 1 / math.Sqrt2
	tests := []struct {
		name   string
		input  types.InputPayload
		wx, wy float64
	}{
		{"idle", types.InputPayload{}, 0, 0},
		{"up", types.InputPayload{Forward: true}, 0, -1},
		{"down", types.InputPayload{Backward: true}, 0, 1},
		{"left", types.InputPayload{Left: true}, -1, 0},
		{"right", types.InputPayload{Right: true}, 1, 0},
		{"up-right is unit length", types.InputPayload{Forward: true, Right: true}, d, -d},
		{"down-left is unit length", types.InputPayload{Backward: true, Left: true}, -d, d},
		{"opposites cancel", types.InputPayload{Forward: true, Backward: true, Left: true, Right: true}, 0, 0},
		{"opposites cancel on one axis", types.InputPayload{Left: true, Right: true, Backward: true}, 0, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			x, y := movementDirection(&tt.input)
			if math.Abs(x-tt.wx) > 1e-9 || math.Abs(y-tt.wy) > 1e-9 {
				t.Errorf("got (%v, %v), want (%v, %v)", x, y, tt.wx, tt.wy)
			}
		})
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
