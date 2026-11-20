// Package webapp serves the built web client (a single-page app) from disk,
// so a desktop or LAN server delivers the client and the API from one origin.
package webapp

import (
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path"
	"strings"
)

func init() {
	// Not in Go's built-in table; without it the type depends on the OS
	// registry. The client loads its sounds as .ogg.
	_ = mime.AddExtensionType(".ogg", "audio/ogg")
}

// NewSPAHandler serves files from dir. Paths that do not name a file get
// dir/index.html, so client-side routes such as /<sessionId> load the app.
// Paths under /api/ never fall back: an unknown API route is a 404, not HTML.
// Register it at "/" so the more specific API and WebSocket routes win.
func NewSPAHandler(dir string) http.Handler {
	return &spaHandler{fsys: os.DirFS(dir), files: http.FileServerFS(os.DirFS(dir))}
}

type spaHandler struct {
	fsys  fs.FS
	files http.Handler
}

func (h *spaHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/api/") {
		http.NotFound(w, r)
		return
	}

	name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
	if name != "" && name != "index.html" {
		if info, err := fs.Stat(h.fsys, name); err == nil && !info.IsDir() {
			// Bundles and assets carry a content hash in their names
			h.files.ServeHTTP(w, r)
			return
		}
	}
	h.serveIndex(w, r)
}

// serveIndex sends index.html directly rather than through the file server,
// which would redirect /index.html to / and list directories.
func (h *spaHandler) serveIndex(w http.ResponseWriter, r *http.Request) {
	// index.html references the hashed bundles, so it must be revalidated to
	// pick up a new client after an update
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeFileFS(w, r, h.fsys, "index.html")
}
