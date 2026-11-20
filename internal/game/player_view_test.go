package game

import (
	"testing"

	"github.com/albert-tarly/dungerra-api/internal/protocol"
	"github.com/albert-tarly/dungerra-api/internal/types"
)

// newTwoPlayerEngine connects an observer and a target standing offset apart,
// and brings the observer's client up to date.
func newTwoPlayerEngine(t *testing.T, offset float64) (*Engine, *types.Player, *types.Player) {
	t.Helper()
	e, observer := newTickTestEngine(t)
	target := e.ConnectPlayer("p2", "target")
	target.Position = &types.Vector2{X: observer.Position.X + offset, Y: observer.Position.Y}
	target.InvulnerableTimer = 0
	observer.InvulnerableTimer = 0
	syncView(e, observer.ID)
	return e, observer, target
}

// syncView computes a delta for the player and marks it delivered.
func syncView(e *Engine, playerID string) *protocol.GameStateDeltaMessage {
	delta, view := e.GetGameStateDeltaForPlayer(playerID)
	e.CommitPlayerView(playerID, view)
	return delta
}

func isRemoved(delta *protocol.GameStateDeltaMessage, id string) bool {
	for _, removed := range delta.RemovedPlayers {
		if removed == id {
			return true
		}
	}
	return false
}

// A player wearing goggles is still seen inside the observer's torch light:
// added once, then kept up to date, not re-added every tick with stale data.
func TestGoggledPlayerInTorchLightIsUpdated(t *testing.T) {
	e, observer, target := newTwoPlayerEngine(t, 100)
	target.NightVisionTimer = 20

	delta := syncView(e, observer.ID)
	if _, added := delta.AddedPlayers[target.ID]; added || isRemoved(delta, target.ID) {
		t.Fatalf("goggles on in torch light: want an update, got added=%v removed=%v", added, isRemoved(delta, target.ID))
	}
	if update := delta.UpdatedPlayers[target.ID]; update == nil || update.Timers == nil || update.Timers.NightVisionTimer != 20 {
		t.Fatalf("night vision not sent: %+v", delta.UpdatedPlayers[target.ID])
	}

	target.Position.X += 10
	delta = syncView(e, observer.ID)
	if _, added := delta.AddedPlayers[target.ID]; added {
		t.Fatal("goggled player re-added instead of updated")
	}
	if update := delta.UpdatedPlayers[target.ID]; update == nil || update.Position == nil {
		t.Fatalf("goggled player's move not sent: %+v", update)
	}
}

// A goggled player out of the torch light is removed once; walking into the
// light adds them once with their goggles on, and walking out removes them.
func TestGoggledPlayerEntersAndLeavesTorchLight(t *testing.T) {
	e, observer, target := newTwoPlayerEngine(t, 800)
	target.NightVisionTimer = 20

	if delta := syncView(e, observer.ID); !isRemoved(delta, target.ID) {
		t.Fatal("goggled player out of the light not removed")
	}
	if delta := syncView(e, observer.ID); len(delta.AddedPlayers) != 0 || isRemoved(delta, target.ID) {
		t.Fatalf("hidden goggled player still reported: %+v", delta)
	}

	target.Position.X = observer.Position.X + 100
	delta := syncView(e, observer.ID)
	added := delta.AddedPlayers[target.ID]
	if added == nil || added.NightVisionTimer != 20 {
		t.Fatalf("goggled player entering the light not added with goggles: %+v", added)
	}
	if delta = syncView(e, observer.ID); delta.AddedPlayers[target.ID] != nil {
		t.Fatal("goggled player in the light added again")
	}

	target.Position.X = observer.Position.X + 800
	if delta = syncView(e, observer.ID); !isRemoved(delta, target.ID) {
		t.Fatal("goggled player leaving the light not removed")
	}
}

// Both wearing goggles: the target is visible across the sight radius.
func TestGoggledPlayersSeeEachOther(t *testing.T) {
	e, observer, target := newTwoPlayerEngine(t, 800)
	observer.NightVisionTimer = 20
	target.NightVisionTimer = 20

	delta := syncView(e, observer.ID)
	if isRemoved(delta, target.ID) {
		t.Fatal("goggled player removed although the observer wears goggles")
	}
	target.Position.X += 10
	if update := syncView(e, observer.ID).UpdatedPlayers[target.ID]; update == nil || update.Position == nil {
		t.Fatalf("goggled player not tracked by a goggled observer: %+v", update)
	}
}

// A player who dies in view and respawns in view must be reported alive
// again, not left as a corpse on the observer's screen.
func TestRespawnInViewIsReportedAlive(t *testing.T) {
	e, observer, target := newTwoPlayerEngine(t, 100)

	target.Die()
	if update := syncView(e, observer.ID).UpdatedPlayers[target.ID]; update == nil || update.Lives == nil || update.Lives.IsAlive {
		t.Fatalf("death not sent: %+v", update)
	}
	// The corpse stays known while it lies in the torch light
	if delta := syncView(e, observer.ID); delta.AddedPlayers[target.ID] != nil || isRemoved(delta, target.ID) {
		t.Fatalf("corpse re-added or removed: %+v", delta)
	}

	target.Respawn(&types.Vector2{X: observer.Position.X + 800, Y: observer.Position.Y})
	delta := syncView(e, observer.ID)
	alive := false
	if added := delta.AddedPlayers[target.ID]; added != nil {
		alive = added.IsAlive
	} else if update := delta.UpdatedPlayers[target.ID]; update != nil && update.Lives != nil {
		alive = update.Lives.IsAlive
	}
	if !alive {
		t.Fatalf("respawn not reported alive: added=%+v updated=%+v", delta.AddedPlayers[target.ID], delta.UpdatedPlayers[target.ID])
	}
}

// A corpse outside the torch light is removed; the respawned player is added.
func TestRespawnAfterUnseenDeathIsAdded(t *testing.T) {
	e, observer, target := newTwoPlayerEngine(t, 800)

	target.Die()
	if delta := syncView(e, observer.ID); !isRemoved(delta, target.ID) {
		t.Fatal("corpse in the dark not removed")
	}

	target.Respawn(&types.Vector2{X: observer.Position.X + 700, Y: observer.Position.Y})
	added := syncView(e, observer.ID).AddedPlayers[target.ID]
	if added == nil || !added.IsAlive {
		t.Fatalf("respawned player not added alive: %+v", added)
	}
}
