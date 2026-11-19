package game

import (
	"encoding/json"
	"testing"

	"github.com/besuhoff/dungerra/internal/config"
	"github.com/besuhoff/dungerra/internal/db"
	"github.com/besuhoff/dungerra/internal/types"
)

// The SQLite backend stores world objects as JSON, which decodes numbers as
// float64 (BSON yields int32). Shop inventory must survive that round-trip.
func TestShopInventorySurvivesJSONRoundTrip(t *testing.T) {
	src := NewEngine("s")
	src.state.shopsByChunk["0,0"] = map[string]*types.Shop{
		"shop1": {
			ScreenObject: types.ScreenObject{ID: "shop1", Position: &types.Vector2{X: 10, Y: 20}},
			Name:         "Corner Store",
			Inventory: map[types.InventoryItemID]*types.ShopInventoryItem{
				types.InventoryItemAidKit: {Price: 150, Quantity: 7, PackSize: 1},
			},
		},
	}

	saved := &db.GameSession{GameVersion: config.GameVersion}
	src.SaveToSession(saved)

	raw, err := json.Marshal(saved.SharedObjects)
	if err != nil {
		t.Fatal(err)
	}
	loaded := &db.GameSession{GameVersion: config.GameVersion}
	if err := json.Unmarshal(raw, &loaded.SharedObjects); err != nil {
		t.Fatal(err)
	}

	dst := NewEngine("s")
	dst.LoadFromSession(loaded)

	shop := dst.state.shopsByChunk["0,0"]["shop1"]
	if shop == nil {
		t.Fatal("shop not restored")
	}
	item := shop.Inventory[types.InventoryItemAidKit]
	if item == nil || item.Price != 150 || item.Quantity != 7 || item.PackSize != 1 {
		t.Fatalf("shop inventory not restored: %+v", item)
	}
}
