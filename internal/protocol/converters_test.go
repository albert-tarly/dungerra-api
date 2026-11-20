package protocol

import (
	"math"
	"testing"

	"github.com/albert-tarly/dungerra-api/internal/types"
)

// The view keeps a clone of each shop, so an unchanged inventory has equal
// items behind different pointers and must not produce an update.
func TestShopUpdateComparesItemsNotPointers(t *testing.T) {
	shop := &types.Shop{
		ScreenObject: types.ScreenObject{ID: "s", Position: &types.Vector2{}},
		Inventory: map[types.InventoryItemID]*types.ShopInventoryItem{
			types.InventoryItemAidKit: {Price: 50, Quantity: 3, PackSize: 1},
		},
	}
	clone := shop.Clone()
	if update := ToProtoShopUpdate(clone, shop); update != nil {
		t.Fatalf("unchanged shop produced an update: %+v", update)
	}

	shop.Inventory[types.InventoryItemAidKit].Quantity = 2
	update := ToProtoShopUpdate(clone, shop)
	if update == nil || update.Inventory[int32(types.InventoryItemAidKit)].Quantity != 2 {
		t.Fatalf("purchase not sent: %+v", update)
	}
}

func TestFromProtoInputAimRotation(t *testing.T) {
	f := func(v float64) *float64 { return &v }
	tests := []struct {
		name string
		in   *float64
		want *float64
	}{
		{"unset stays unset", nil, nil},
		{"in range", f(45), f(45)},
		{"normalised", f(-90), f(270)},
		{"NaN dropped", f(math.NaN()), nil},
		{"Inf dropped", f(math.Inf(1)), nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FromProtoInput(&InputMessage{AimRotation: tt.in}).AimRotation
			if (got == nil) != (tt.want == nil) || (got != nil && *got != *tt.want) {
				t.Errorf("AimRotation = %v, want %v", got, tt.want)
			}
		})
	}
}
