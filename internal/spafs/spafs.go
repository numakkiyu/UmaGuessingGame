// Package spafs embeds the SPA so deployment needs no separate frontend directory.
// The source placeholder permits backend-only builds and reports an unbuilt SPA at runtime.
package spafs

import (
	"embed"
	"io/fs"
	"net/http"
	"os"
	"path"
	"strings"
)

//go:embed all:dist
var embedded embed.FS

// FS returns the embedded SPA filesystem rooted at the dist directory.
//
// The second result is false when no real build is present, which happens when
// only the placeholder file exists. Callers use it to decide between serving the
// embedded files and reporting a build error.
func FS() (fs.FS, bool) {
	sub, err := fs.Sub(embedded, "dist")
	if err != nil {
		return nil, false
	}
	// The placeholder is the only file in a checkout that never built the SPA.
	if _, err := fs.Stat(sub, "index.html"); err != nil {
		return nil, false
	}
	return sub, true
}

// HasIndex reports whether a real SPA build is embedded.
func HasIndex() bool {
	_, ok := FS()
	return ok
}

// Handler serves the embedded SPA.
//
// Cache policy:
// - Vite emits content-hashed file names under assets/, so those are immutable;
// - index.html must never be cached, because a stale shell would reference
// bundles that no longer exist;
// - a path that looks like a file but is missing returns 404 rather than the
// shell, so a missing asset is never disguised as a successful page load;
// - any other path falls back to the shell so a deep link survives a refresh.
func Handler() http.Handler {
	sub, ok := FS()
	if !ok {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "the SPA bundle is not embedded in this binary; build the frontend before compiling the server", http.StatusServiceUnavailable)
		})
	}
	return &handler{files: sub}
}

type handler struct {
	files fs.FS
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	rel := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
	if rel == "" {
		h.serveIndex(w, r)
		return
	}

	if info, err := fs.Stat(h.files, rel); err == nil && !info.IsDir() {
		if strings.HasPrefix(rel, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache, must-revalidate")
		}
		http.ServeFileFS(w, r, h.files, rel)
		return
	}

	// A missing file must 404, not return HTML.
	if path.Ext(path.Base(rel)) != "" {
		http.NotFound(w, r)
		return
	}
	h.serveIndex(w, r)
}

func (h *handler) serveIndex(w http.ResponseWriter, r *http.Request) {
	// The shell references hashed bundles, so it must not be cached.
	w.Header().Set("Cache-Control", "no-cache, must-revalidate")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	http.ServeFileFS(w, r, h.files, "index.html")
}

// DirOrEmbedded chooses how to serve the SPA.
//
// An explicit directory wins: it lets a developer point the server at a live
// `vite build --watch` output without recompiling Go. When the directory is
// absent the embedded copy is used, which is the production path. When neither
// exists the caller gets a handler that reports the build error instead of
// silently serving 404s.
func DirOrEmbedded(dir string) http.Handler {
	if dir != "" {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			if _, err := os.Stat(path.Join(dir, "index.html")); err == nil {
				return &diskHandler{dir: dir, files: os.DirFS(dir)}
			}
		}
	}
	return Handler()
}

// diskHandler serves the SPA from a directory, with the same cache and fallback
// rules as the embedded handler.
type diskHandler struct {
	dir   string
	files fs.FS
}

func (h *diskHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	rel := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
	if rel == "" {
		h.serveIndex(w, r)
		return
	}
	if info, err := fs.Stat(h.files, rel); err == nil && !info.IsDir() {
		if strings.HasPrefix(rel, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache, must-revalidate")
		}
		http.ServeFileFS(w, r, h.files, rel)
		return
	}
	if path.Ext(path.Base(rel)) != "" {
		http.NotFound(w, r)
		return
	}
	h.serveIndex(w, r)
}

func (h *diskHandler) serveIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-cache, must-revalidate")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	http.ServeFileFS(w, r, h.files, "index.html")
}

// Has reports whether the embedded bundle contains a real file at the URL path.
//
// It exists so the caller can disambiguate a shared /assets/ prefix: the SPA owns
// its hashed files, everything else under /assets/ belongs to the content media
// root.
func (h *handler) Has(urlPath string) bool {
	rel := strings.TrimPrefix(path.Clean("/"+urlPath), "/")
	if rel == "" {
		return false
	}
	info, err := fs.Stat(h.files, rel)
	return err == nil && !info.IsDir()
}

// Has reports whether the on-disk bundle contains a real file at the URL path.
func (h *diskHandler) Has(urlPath string) bool {
	rel := strings.TrimPrefix(path.Clean("/"+urlPath), "/")
	if rel == "" {
		return false
	}
	info, err := fs.Stat(h.files, rel)
	return err == nil && !info.IsDir()
}
