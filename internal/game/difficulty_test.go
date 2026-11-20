package game

import (
	"testing"

	"github.com/albert-tarly/dungerra-api/internal/config"
	"github.com/albert-tarly/dungerra-api/internal/db"
	"github.com/albert-tarly/dungerra-api/internal/types"
)

func newTestEngine(t *testing.T, difficulty *config.Difficulty) *Engine {
	t.Helper()
	// NewEngine reads the global config, which is normally set by LoadConfig at startup
	prevConfig := config.AppConfig
	config.AppConfig = &config.Config{}
	t.Cleanup(func() { config.AppConfig = prevConfig })

	e := NewEngine("s")
	e.LoadFromSession(&db.GameSession{GameVersion: config.GameVersion, Difficulty: difficulty})
	return e
}

func preset(t *testing.T, name string) *config.Difficulty {
	t.Helper()
	d, ok := config.DifficultyPreset(name)
	if !ok {
		t.Fatalf("preset %q missing", name)
	}
	return &d
}

type chunkCensus struct {
	walls, chunks int
	enemies       map[string]int
	chests        int
	money         int
}

// generate builds a block of chunks far from the probe player and counts what
// they contain.
func generate(e *Engine) chunkCensus {
	far := &types.Vector2{X: -1e6, Y: -1e6}
	for x := 0; x < 6; x++ {
		for y := 0; y < 6; y++ {
			e.generateChunk(x, y, far)
		}
	}
	c := chunkCensus{chunks: 36, enemies: map[string]int{}}
	for _, walls := range e.state.wallsByChunk {
		c.walls += len(walls)
	}
	for _, enemies := range e.state.enemiesByChunk {
		for _, enemy := range enemies {
			c.enemies[enemy.Type]++
		}
	}
	for _, bonus := range e.state.bonuses {
		if bonus.Type == types.BonusTypeChest {
			c.chests++
			for _, item := range bonus.Inventory {
				if item.Type == types.InventoryItemMoney {
					c.money += int(item.Quantity)
				}
			}
		}
	}
	return c
}

func TestPeacefulHasNoEnemiesAndWallChests(t *testing.T) {
	c := generate(newTestEngine(t, preset(t, config.DifficultyPeaceful)))
	if len(c.enemies) != 0 {
		t.Fatalf("peaceful spawned enemies: %v", c.enemies)
	}
	// 25% of walls, minus the few whose spot is blocked by another wall.
	if share := float64(c.chests) / float64(c.walls); share < 0.18 || share > 0.3 {
		t.Fatalf("wall chests on %.2f of walls, want about 0.25 (%d/%d)", share, c.chests, c.walls)
	}
	if c.money < c.chests*config.WallChestMoneyMin {
		t.Fatalf("every wall chest must hold money: %d over %d chests", c.money, c.chests)
	}
}

func TestEasyHasNoSnipersOrTowersAndHalfTheGuards(t *testing.T) {
	c := generate(newTestEngine(t, preset(t, config.DifficultyEasy)))
	if c.enemies[types.EnemyTypeSniper] != 0 || c.enemies[types.EnemyTypeTower] != 0 || c.chests != 0 {
		t.Fatalf("easy: %v, chests %d", c.enemies, c.chests)
	}
	guards := float64(c.enemies[types.EnemyTypeSoldier]+c.enemies[types.EnemyTypeLieutenant]) / float64(c.walls)
	// Hard has 0.855 soldiers+lieutenants per wall; Easy half of that.
	if guards < 0.37 || guards > 0.49 {
		t.Fatalf("easy guards per wall %.3f, want about 0.43", guards)
	}
}

func TestHardKeepsOneTowerPerChunkAndOriginalMix(t *testing.T) {
	c := generate(newTestEngine(t, nil)) // Legacy sessions play as Hard
	if c.enemies[types.EnemyTypeTower] != c.chunks {
		t.Fatalf("towers %d, want one per chunk (%d)", c.enemies[types.EnemyTypeTower], c.chunks)
	}
	perWall := float64(c.enemies[types.EnemyTypeSoldier]+c.enemies[types.EnemyTypeLieutenant]+c.enemies[types.EnemyTypeSniper]) / float64(c.walls)
	if perWall < 0.85 || perWall > 0.95 || c.chests != 0 {
		t.Fatalf("hard: %.3f enemies per wall (want ~0.9), chests %d", perWall, c.chests)
	}
}

func TestCustomDoubleSpawnsTwoTowersAndExtraGuards(t *testing.T) {
	d := config.HardDifficulty()
	d.Preset = config.DifficultyCustom
	d.SoldiersPct, d.LieutenantsPct, d.SnipersPct, d.TowersPct = 200, 200, 200, 200
	c := generate(newTestEngine(t, &d))
	if c.enemies[types.EnemyTypeTower] != 2*c.chunks {
		t.Fatalf("towers %d, want two per chunk", c.enemies[types.EnemyTypeTower])
	}
	perWall := float64(c.enemies[types.EnemyTypeSoldier]+c.enemies[types.EnemyTypeLieutenant]+c.enemies[types.EnemyTypeSniper]) / float64(c.walls)
	if perWall < 1.7 || perWall > 1.9 {
		t.Fatalf("custom 200%%: %.3f enemies per wall, want ~1.8", perWall)
	}
}

func TestAwardKillScalesMoneyAndScoreSeparately(t *testing.T) {
	e := newTestEngine(t, preset(t, config.DifficultyEasy))
	p := &types.Player{}
	e.awardKill(p, config.EnemyTowerReward)
	if p.Money != 400 || p.Score != 90 || p.Kills != 1 {
		t.Fatalf("easy tower kill: money %d score %d kills %d, want 400/90/1", p.Money, p.Score, p.Kills)
	}

	e = newTestEngine(t, nil)
	p = &types.Player{}
	e.awardKill(p, config.PlayerReward)
	if p.Money != 100 || p.Score != 100 {
		t.Fatalf("hard player kill: money %d score %d, want 100/100", p.Money, p.Score)
	}
}

func TestDifficultyLivesAndPersistence(t *testing.T) {
	e := newTestEngine(t, preset(t, config.DifficultyEasy))
	p := e.ConnectPlayer("p1", "alice")
	if p.Lives != 8 || p.MaxLives != 8 {
		t.Fatalf("easy player lives %v/%v, want 8/8", p.Lives, p.MaxLives)
	}

	saved := &db.GameSession{}
	e.SaveToSession(saved)
	if saved.Difficulty == nil || saved.Difficulty.Preset != config.DifficultyEasy {
		t.Fatalf("difficulty not saved: %+v", saved.Difficulty)
	}
	// Engine shoot delays follow the fire rate: Easy fires at 75%.
	if got := e.enemyShootDelay(config.EnemySoldierShootDelay); got < 1.33 || got > 1.34 {
		t.Fatalf("easy soldier shoot delay %v, want ~1.333", got)
	}
}
