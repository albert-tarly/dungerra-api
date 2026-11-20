package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/albert-tarly/dungerra-api/internal/config"
)

// VersionResponse identifies the running server. The desktop launcher probes
// it before joining a LAN host, so it needs no authentication.
type VersionResponse struct {
	GameVersion string `json:"game_version"`
}

// HandleVersion returns the server's game version.
func HandleVersion(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(VersionResponse{GameVersion: config.GameVersion})
}
