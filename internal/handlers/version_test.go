package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/albert-tarly/dungerra-api/internal/config"
)

func TestHandleVersion(t *testing.T) {
	rec := httptest.NewRecorder()
	HandleVersion(rec, httptest.NewRequest(http.MethodGet, "/api/v1/version", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var got VersionResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.GameVersion != config.GameVersion {
		t.Errorf("game_version = %q, want %q", got.GameVersion, config.GameVersion)
	}

	rec = httptest.NewRecorder()
	HandleVersion(rec, httptest.NewRequest(http.MethodPost, "/api/v1/version", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST status = %d, want 405", rec.Code)
	}
}
