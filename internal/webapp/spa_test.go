package webapp

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newTestSite(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"index.html":             "<html>app</html>",
		"main.abc123.js":         "console.log('app')",
		"assets/sound.ogg":       "OggS",
		"assets/nested/info.txt": "nested",
	}
	for name, body := range files {
		full := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestSPAHandler(t *testing.T) {
	h := NewSPAHandler(newTestSite(t))

	tests := []struct {
		name, method, path string
		wantStatus         int
		wantBody           string // substring
		wantType           string // prefix of Content-Type, if set
	}{
		{"root serves index", http.MethodGet, "/", 200, "app</html>", "text/html"},
		{"existing bundle", http.MethodGet, "/main.abc123.js", 200, "console.log", "text/javascript"},
		{"ogg has audio type", http.MethodGet, "/assets/sound.ogg", 200, "OggS", "audio/ogg"},
		{"deep link falls back", http.MethodGet, "/65f0c0ffee/", 200, "app</html>", "text/html"},
		{"session route falls back", http.MethodGet, "/65f0c0ffee", 200, "app</html>", "text/html"},
		{"directory falls back, no listing", http.MethodGet, "/assets/", 200, "app</html>", "text/html"},
		{"traversal is rejected", http.MethodGet, "/../../etc/passwd", 400, "invalid URL path", ""},
		{"unknown api route is 404", http.MethodGet, "/api/v1/nope", 404, "", ""},
		{"head works", http.MethodHead, "/", 200, "", ""},
		{"post rejected", http.MethodPost, "/", 405, "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(tt.method, tt.path, nil))
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d (body %q)", rec.Code, tt.wantStatus, rec.Body.String())
			}
			if tt.wantBody != "" && !strings.Contains(rec.Body.String(), tt.wantBody) {
				t.Errorf("body = %q, want it to contain %q", rec.Body.String(), tt.wantBody)
			}
			if tt.wantType != "" && !strings.HasPrefix(rec.Header().Get("Content-Type"), tt.wantType) {
				t.Errorf("Content-Type = %q, want prefix %q", rec.Header().Get("Content-Type"), tt.wantType)
			}
		})
	}
}

func TestSPAHandlerIndexIsRevalidated(t *testing.T) {
	h := NewSPAHandler(newTestSite(t))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/some/route", nil))
	if got := rec.Header().Get("Cache-Control"); got != "no-cache" {
		t.Errorf("Cache-Control = %q, want no-cache so updated clients load", got)
	}
}

// Registered at "/" on a mux, the API routes keep priority over the SPA.
func TestSPAHandlerOnMuxKeepsAPIRoutes(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/version", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("api")) })
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("OK")) })
	mux.Handle("/", NewSPAHandler(newTestSite(t)))

	for path, want := range map[string]string{
		"/api/v1/version": "api",
		"/health":         "OK",
		"/leaderboard":    "app</html>",
	} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("%s: body = %q, want %q", path, rec.Body.String(), want)
		}
	}
}
