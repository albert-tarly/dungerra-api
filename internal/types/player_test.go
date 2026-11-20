package types

import (
	"testing"

	"github.com/albert-tarly/dungerra-api/internal/config"
)

func aidKitBonus(quantity int32) *Bonus {
	return &Bonus{
		ScreenObject: ScreenObject{ID: "b1", Position: &Vector2{}},
		Type:         BonusTypeChest,
		Inventory: []InventoryItem{
			{Type: InventoryItemAidKit, Quantity: quantity},
			{Type: InventoryItemGoggles, Quantity: 1},
		},
	}
}

// Picked-up aid kits heal straight away while the player is hurt; only the
// ones not needed go into the inventory, and other items are unaffected.
func TestPickupBonusAppliesAidKitsWhenHurt(t *testing.T) {
	maxLives := float32(config.PlayerLives)
	tests := []struct {
		name       string
		lives      float32
		kits       int32
		wantLives  float32
		wantStored int32
	}{
		{name: "full health stores all", lives: maxLives, kits: 2, wantLives: maxLives, wantStored: 2},
		{name: "one life missing uses one", lives: maxLives - 1, kits: 3, wantLives: maxLives, wantStored: 2},
		{name: "badly hurt uses all", lives: 1, kits: 2, wantLives: 3, wantStored: 0},
		{name: "fraction missing uses one, capped", lives: maxLives - 0.5, kits: 1, wantLives: maxLives, wantStored: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Player{Lives: tt.lives}
			bonus := aidKitBonus(tt.kits)

			p.PickupBonus(bonus)

			if p.Lives != tt.wantLives {
				t.Errorf("Lives = %v, want %v", p.Lives, tt.wantLives)
			}
			if got := p.GetInventoryItemQuantity(InventoryItemAidKit); got != tt.wantStored {
				t.Errorf("stored aid kits = %v, want %v", got, tt.wantStored)
			}
			if got := p.GetInventoryItemQuantity(InventoryItemGoggles); got != 1 {
				t.Errorf("goggles = %v, want 1", got)
			}
			if len(bonus.Inventory) != 0 {
				t.Errorf("bonus not emptied: %+v", bonus.Inventory)
			}
		})
	}
}

// A difficulty with more lives raises both the respawn lives and the cap
// aid kits heal up to.
func TestMaxLivesFromDifficulty(t *testing.T) {
	p := &Player{Lives: 6, MaxLives: 8}
	p.PickupBonus(aidKitBonus(3))
	if p.Lives != 8 || p.GetInventoryItemQuantity(InventoryItemAidKit) != 1 {
		t.Fatalf("lives %v, stored kits %d; want 8 and 1", p.Lives, p.GetInventoryItemQuantity(InventoryItemAidKit))
	}

	p.Die()
	p.Respawn(&Vector2{})
	if p.Lives != 8 {
		t.Fatalf("respawned with %v lives, want 8", p.Lives)
	}
}
