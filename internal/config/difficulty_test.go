package config

import "testing"

func TestDifficultyPresetScoreMultipliers(t *testing.T) {
	want := map[string]float64{
		DifficultyPeaceful:  0.25,
		DifficultyEasy:      0.45,
		DifficultyHard:      1.0,
		DifficultyNightmare: 1.2,
	}
	for name, multiplier := range want {
		d, ok := DifficultyPreset(name)
		if !ok {
			t.Fatalf("preset %q missing", name)
		}
		if d.Preset != name || d.ScoreMultiplier != multiplier {
			t.Errorf("%s: preset=%q score=%v, want score %v", name, d.Preset, d.ScoreMultiplier, multiplier)
		}
	}
}

func TestHardDifficultyMatchesLegacyGame(t *testing.T) {
	d := HardDifficulty()
	if d.SoldiersPct != 100 || d.LieutenantsPct != 100 || d.SnipersPct != 100 || d.TowersPct != 100 ||
		d.MoneyPct != 100 || d.DropChancePct != 100 || d.WallChestPct != 0 ||
		d.EnemyFireRatePct != 100 || d.PlayerLives != int(PlayerLives) || d.ScoreMultiplier != 1 {
		t.Fatalf("Hard must reproduce the pre-difficulty game: %+v", d)
	}
}

func TestResolveDifficulty(t *testing.T) {
	// A named preset ignores values sent alongside it.
	d, err := ResolveDifficulty(Difficulty{Preset: DifficultyEasy, SoldiersPct: 200, ScoreMultiplier: 9})
	if err != nil || d.SoldiersPct != 50 || d.ScoreMultiplier != 0.45 {
		t.Fatalf("easy: %+v, %v", d, err)
	}

	// Custom keeps the values and computes the score multiplier itself.
	custom := HardDifficulty()
	custom.Preset = DifficultyCustom
	custom.SnipersPct = 0
	custom.ScoreMultiplier = 9
	d, err = ResolveDifficulty(custom)
	if err != nil || d.SnipersPct != 0 || d.ScoreMultiplier != 0.9 {
		t.Fatalf("custom: %+v, %v", d, err)
	}

	for name, req := range map[string]Difficulty{
		"missing":        {},
		"unknown":        {Preset: "insane"},
		"zero fire":      func() Difficulty { c := custom; c.EnemyFireRatePct = 0; return c }(),
		"too many":       func() Difficulty { c := custom; c.TowersPct = 201; return c }(),
		"no lives":       func() Difficulty { c := custom; c.PlayerLives = 0; return c }(),
		"chest over 100": func() Difficulty { c := custom; c.WallChestPct = 101; return c }(),
	} {
		if _, err := ResolveDifficulty(req); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
}

func TestScoreMultiplierClamped(t *testing.T) {
	maxed := Difficulty{SoldiersPct: 200, LieutenantsPct: 200, SnipersPct: 200, TowersPct: 200, EnemyFireRatePct: 200, PlayerLives: 1}
	if got := ComputeScoreMultiplier(maxed); got != ScoreMultiplierMax {
		t.Errorf("max: got %v", got)
	}
	empty := Difficulty{EnemyFireRatePct: 100, PlayerLives: 6}
	if got := ComputeScoreMultiplier(empty); got != ScoreMultiplierMin {
		t.Errorf("min: got %v", got)
	}
}
