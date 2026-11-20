package config

import (
	"fmt"
	"math"
)

// Difficulty presets. A preset is only a name for a set of values: the
// resolved values are what a session stores, so retuning a preset later does
// not change worlds that were created with it.
const (
	DifficultyPeaceful  = "peaceful"
	DifficultyEasy      = "easy"
	DifficultyHard      = "hard"
	DifficultyNightmare = "nightmare"
	DifficultyCustom    = "custom"
)

// Difficulty holds the per-session gameplay knobs. Percentages are relative to
// Hard (100 = the game as it was before difficulties existed).
type Difficulty struct {
	Preset string `bson:"preset" json:"preset"`

	SoldiersPct    int `bson:"soldiers_pct" json:"soldiers_pct"`       // Expected soldiers per wall, % of Hard
	LieutenantsPct int `bson:"lieutenants_pct" json:"lieutenants_pct"` // Expected lieutenants per wall, % of Hard
	SnipersPct     int `bson:"snipers_pct" json:"snipers_pct"`         // Expected snipers per wall, % of Hard
	TowersPct      int `bson:"towers_pct" json:"towers_pct"`           // Towers per chunk, % of Hard (100 = one)

	MoneyPct         int `bson:"money_pct" json:"money_pct"`                     // Money per kill (enemies and players), % of Hard
	DropChancePct    int `bson:"drop_chance_pct" json:"drop_chance_pct"`         // Soldier and lieutenant drop chance, % of Hard (capped at 100% chance)
	WallChestPct     int `bson:"wall_chest_pct" json:"wall_chest_pct"`           // Chance a wall gets a loot chest next to it, %
	EnemyFireRatePct int `bson:"enemy_fire_rate_pct" json:"enemy_fire_rate_pct"` // Enemy shots per second, % of Hard
	PlayerLives      int `bson:"player_lives" json:"player_lives"`               // Lives a player spawns with and can heal up to

	// ScoreMultiplier is derived from the values above (see
	// ComputeScoreMultiplier) and stored so the leaderboard stays consistent
	// with the formula in force when the session was created.
	ScoreMultiplier float64 `bson:"score_multiplier" json:"score_multiplier"`
}

// Inclusive bounds for custom difficulty values.
const (
	DifficultyEnemyPctMin     = 0
	DifficultyEnemyPctMax     = 200
	DifficultyMoneyPctMin     = 0
	DifficultyMoneyPctMax     = 300
	DifficultyDropPctMin      = 0
	DifficultyDropPctMax      = 300
	DifficultyWallChestPctMin = 0
	DifficultyWallChestPctMax = 100
	DifficultyFireRatePctMin  = 25 // Not 0: the shoot delay is divided by it
	DifficultyFireRatePctMax  = 200
	DifficultyPlayerLivesMin  = 1
	DifficultyPlayerLivesMax  = 12
)

// Score multiplier formula. The danger of a world is the weighted share of
// Hard's enemy population, scaled by enemy fire rate and by how many hits the
// player can take. The square root keeps easier settings from being punished
// too hard, and the clamp keeps the leaderboard sane at the extremes.
const (
	ScoreWeightSoldiers    = 0.40
	ScoreWeightLieutenants = 0.25
	ScoreWeightSnipers     = 0.15
	ScoreWeightTowers      = 0.20
	ScoreMultiplierMin     = 0.25
	ScoreMultiplierMax     = 2.0
	ScoreMultiplierSteps   = 20 // Rounded to 1/20 = 0.05 steps so it reads cleanly in the UI
)

var difficultyPresets = map[string]Difficulty{
	DifficultyPeaceful: {
		SoldiersPct: 0, LieutenantsPct: 0, SnipersPct: 0, TowersPct: 0,
		MoneyPct: 200, DropChancePct: 100, WallChestPct: 25,
		EnemyFireRatePct: 100, PlayerLives: int(PlayerLives),
	},
	DifficultyEasy: {
		SoldiersPct: 50, LieutenantsPct: 50, SnipersPct: 0, TowersPct: 0,
		MoneyPct: 200, DropChancePct: 200, WallChestPct: 0,
		EnemyFireRatePct: 75, PlayerLives: 8,
	},
	DifficultyHard: {
		SoldiersPct: 100, LieutenantsPct: 100, SnipersPct: 100, TowersPct: 100,
		MoneyPct: 100, DropChancePct: 100, WallChestPct: 0,
		EnemyFireRatePct: 100, PlayerLives: int(PlayerLives),
	},
	DifficultyNightmare: {
		SoldiersPct: 150, LieutenantsPct: 150, SnipersPct: 150, TowersPct: 150,
		MoneyPct: 150, DropChancePct: 100, WallChestPct: 0,
		EnemyFireRatePct: 100, PlayerLives: int(PlayerLives),
	},
}

// DifficultyPreset returns the resolved settings of a named preset.
func DifficultyPreset(name string) (Difficulty, bool) {
	d, ok := difficultyPresets[name]
	if !ok {
		return Difficulty{}, false
	}
	d.Preset = name
	d.ScoreMultiplier = ComputeScoreMultiplier(d)
	return d, true
}

// HardDifficulty is what sessions created before difficulties existed play at.
func HardDifficulty() Difficulty {
	d, _ := DifficultyPreset(DifficultyHard)
	return d
}

// ResolveDifficulty turns a create-session request into stored settings: a
// named preset ignores any values sent with it, custom values are validated.
func ResolveDifficulty(req Difficulty) (Difficulty, error) {
	if req.Preset == DifficultyCustom {
		if err := validateCustomDifficulty(req); err != nil {
			return Difficulty{}, err
		}
		req.ScoreMultiplier = ComputeScoreMultiplier(req)
		return req, nil
	}
	if d, ok := DifficultyPreset(req.Preset); ok {
		return d, nil
	}
	if req.Preset == "" {
		return Difficulty{}, fmt.Errorf("difficulty is required")
	}
	return Difficulty{}, fmt.Errorf("unknown difficulty %q", req.Preset)
}

func validateCustomDifficulty(d Difficulty) error {
	checks := []struct {
		name     string
		value    int
		min, max int
	}{
		{"soldiers_pct", d.SoldiersPct, DifficultyEnemyPctMin, DifficultyEnemyPctMax},
		{"lieutenants_pct", d.LieutenantsPct, DifficultyEnemyPctMin, DifficultyEnemyPctMax},
		{"snipers_pct", d.SnipersPct, DifficultyEnemyPctMin, DifficultyEnemyPctMax},
		{"towers_pct", d.TowersPct, DifficultyEnemyPctMin, DifficultyEnemyPctMax},
		{"money_pct", d.MoneyPct, DifficultyMoneyPctMin, DifficultyMoneyPctMax},
		{"drop_chance_pct", d.DropChancePct, DifficultyDropPctMin, DifficultyDropPctMax},
		{"wall_chest_pct", d.WallChestPct, DifficultyWallChestPctMin, DifficultyWallChestPctMax},
		{"enemy_fire_rate_pct", d.EnemyFireRatePct, DifficultyFireRatePctMin, DifficultyFireRatePctMax},
		{"player_lives", d.PlayerLives, DifficultyPlayerLivesMin, DifficultyPlayerLivesMax},
	}
	for _, c := range checks {
		if c.value < c.min || c.value > c.max {
			return fmt.Errorf("%s must be between %d and %d", c.name, c.min, c.max)
		}
	}
	return nil
}

// ComputeScoreMultiplier rates how dangerous a difficulty is relative to Hard.
// Money and loot settings are deliberately left out: they make the game
// easier to gear up in but must not inflate leaderboard scores.
func ComputeScoreMultiplier(d Difficulty) float64 {
	enemies := (ScoreWeightSoldiers*float64(d.SoldiersPct) +
		ScoreWeightLieutenants*float64(d.LieutenantsPct) +
		ScoreWeightSnipers*float64(d.SnipersPct) +
		ScoreWeightTowers*float64(d.TowersPct)) / 100
	fireRate := float64(d.EnemyFireRatePct) / 100
	lives := 1.0
	if d.PlayerLives > 0 {
		lives = PlayerLives / float64(d.PlayerLives)
	}
	m := math.Sqrt(enemies * fireRate * lives)
	m = math.Round(m*ScoreMultiplierSteps) / ScoreMultiplierSteps
	return math.Max(ScoreMultiplierMin, math.Min(ScoreMultiplierMax, m))
}
