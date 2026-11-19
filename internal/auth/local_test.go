package auth

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/albert-tarly/dungerra-api/internal/config"
	"github.com/albert-tarly/dungerra-api/internal/db"
)

func setupLocalAuth(t *testing.T) (*LocalAuthHandler, *UserHandler) {
	t.Helper()
	config.AppConfig = &config.Config{SecretKey: "test-secret", AccessTokenExpireMinutes: 60, LocalAuthEnabled: true}
	if err := db.Open(db.Options{Driver: db.DriverSQLite, SQLitePath: filepath.Join(t.TempDir(), "auth.db")}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return NewLocalAuthHandler(), NewUserHandler()
}

func post(h http.HandlerFunc, username, password string) *httptest.ResponseRecorder {
	body, _ := json.Marshal(LocalCredentials{Username: username, Password: password})
	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body)))
	return rec
}

func TestLocalRegisterAndLogin(t *testing.T) {
	local, users := setupLocalAuth(t)

	rec := post(local.HandleRegister, "Alice", "correct horse")
	if rec.Code != http.StatusCreated {
		t.Fatalf("register: %d %s", rec.Code, rec.Body)
	}
	if strings.Contains(rec.Body.String(), "password") || strings.Contains(rec.Body.String(), "$2a$") {
		t.Fatalf("response leaks password data: %s", rec.Body)
	}
	var reg TokenResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &reg); err != nil {
		t.Fatal(err)
	}
	if reg.Token == "" || reg.DisplayNameOrEmpty() != "Alice@local" || reg.User.Provider != db.ProviderLocal {
		t.Fatalf("unexpected register response: %s", rec.Body)
	}

	// Usernames are unique case-insensitively.
	if rec := post(local.HandleRegister, "alice", "another password"); rec.Code != http.StatusConflict {
		t.Fatalf("duplicate register: want 409, got %d", rec.Code)
	}

	// Login is case-insensitive on the username, exact on the password.
	if rec := post(local.HandleLogin, "ALICE", "correct horse"); rec.Code != http.StatusOK {
		t.Fatalf("login: %d %s", rec.Code, rec.Body)
	}
	if rec := post(local.HandleLogin, "alice", "wrong password"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong password: want 401, got %d", rec.Code)
	}
	if rec := post(local.HandleLogin, "nobody", "whatever1"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("unknown user: want 401, got %d", rec.Code)
	}

	// The issued token works with the provider-independent user endpoint.
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+reg.Token)
	rec = httptest.NewRecorder()
	users.HandleGetUser(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"display_name":"Alice@local"`) {
		t.Fatalf("get user: %d %s", rec.Code, rec.Body)
	}
}

func TestLocalRegisterValidation(t *testing.T) {
	local, _ := setupLocalAuth(t)

	cases := []struct{ name, username, password string }{
		{"too short username", "ab", "password123"},
		{"too long username", strings.Repeat("a", 25), "password123"},
		{"@ not allowed", "bob@local", "password123"},
		{"spaces not allowed", "bob smith", "password123"},
		{"short password", "bob", "short"},
		{"long password", "bob", strings.Repeat("p", 73)},
	}
	for _, c := range cases {
		if rec := post(local.HandleRegister, c.username, c.password); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: want 400, got %d", c.name, rec.Code)
		}
	}

	rec := httptest.NewRecorder()
	local.HandleRegister(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET: want 405, got %d", rec.Code)
	}
}

// DisplayNameOrEmpty guards against a nil embedded user in assertions.
func (r TokenResponse) DisplayNameOrEmpty() string {
	if r.User.User == nil {
		return ""
	}
	return r.User.DisplayName
}
