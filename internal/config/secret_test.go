package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadOrCreateSecretKeyCreatesThenReuses(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", SecretKeyFileName)

	first, err := LoadOrCreateSecretKey(path)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if len(first) < 64 {
		t.Fatalf("generated key too short: %d chars", len(first))
	}

	second, err := LoadOrCreateSecretKey(path)
	if err != nil {
		t.Fatalf("reuse: %v", err)
	}
	if second != first {
		t.Fatal("key changed between calls; tokens would not survive a restart")
	}

	// No temporary files are left next to the key
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected only the key file, found %d entries", len(entries))
	}
}

func TestLoadOrCreateSecretKeyKeepsExistingKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), SecretKeyFileName)
	if err := os.WriteFile(path, []byte("  existing-key \r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	key, err := LoadOrCreateSecretKey(path)
	if err != nil {
		t.Fatal(err)
	}
	if key != "existing-key" {
		t.Fatalf("got %q, want trimmed existing key", key)
	}
}

func TestLoadOrCreateSecretKeyRejectsEmptyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), SecretKeyFileName)
	if err := os.WriteFile(path, []byte("\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadOrCreateSecretKey(path); err == nil {
		t.Fatal("empty key file must be an error, not silently replaced")
	}
}

// LoadConfig in desktop mode derives the SQLite path and secret from DATA_DIR.
func TestLoadConfigDataDir(t *testing.T) {
	prev := AppConfig
	t.Cleanup(func() { AppConfig = prev })

	dataDir := t.TempDir()
	staticDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(staticDir, "index.html"), []byte("<html></html>"), 0o600); err != nil {
		t.Fatal(err)
	}
	for key, value := range map[string]string{
		"DATA_DIR":             dataDir,
		"STATIC_DIR":           staticDir,
		"DB_DRIVER":            "",
		"MONGODB_URL":          "",
		"SQLITE_PATH":          "",
		"SECRET_KEY":           "",
		"GOOGLE_CLIENT_ID":     "",
		"GOOGLE_CLIENT_SECRET": "",
		"GOOGLE_AUTH_ENABLED":  "",
		"LOCAL_AUTH_ENABLED":   "",
	} {
		t.Setenv(key, value)
	}

	cfg := LoadConfig(LoadOptions{SkipDotEnv: true})

	if cfg.DBDriver != "sqlite" {
		t.Errorf("DBDriver = %q, want sqlite", cfg.DBDriver)
	}
	if want := filepath.Join(dataDir, "dungerra.db"); cfg.SQLitePath != want {
		t.Errorf("SQLitePath = %q, want %q", cfg.SQLitePath, want)
	}
	if cfg.SecretKey == "" {
		t.Error("SecretKey was not generated")
	}
	stored, err := os.ReadFile(filepath.Join(dataDir, SecretKeyFileName))
	if err != nil {
		t.Fatalf("secret key not persisted: %v", err)
	}
	if string(stored) != cfg.SecretKey+"\n" {
		t.Error("persisted key differs from the one in use")
	}
	if !cfg.LocalAuthEnabled || cfg.GoogleAuthEnabled {
		t.Errorf("auth providers: local=%v google=%v, want local only", cfg.LocalAuthEnabled, cfg.GoogleAuthEnabled)
	}
	if cfg.StaticDir != staticDir {
		t.Errorf("StaticDir = %q, want %q", cfg.StaticDir, staticDir)
	}
}
