// Package staticserv serves the SPA and public pack media with separate cache policies.
// API and health paths bypass SPA fallback; private pack files are never exposed.
package staticserv

import (
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/bhcn/umaguessinggame/internal/spafs"
)

// Config describes where the assets live on disk.
type Config struct {
	// SPADir overrides the embedded SPA with a Vite output directory.
	SPADir string
	// ContentDir is the published content directory holding public/assets.
	ContentDir string
	// FaceAlias resolves a memory board's per-card face token to the pair's stored
	// file, relative to the media root (for example "faces/mp-x.webp").
	//
	// The memory mode must not hand out a URL that reveals which two cards form a
	// pair . Each card therefore gets an opaque token with no shared prefix,
	// and this resolver is the only place that knows which stored image a token
	// points at. It returns ("", false) for an unknown token, which becomes a 404.
	FaceAlias func(token string) (storedRelPath string, ok bool)
}

// Handler serves the SPA and content media, delegating everything else to next.
//
// The routing order matters: API and health prefixes are checked first so they
// are never answered with an SPA shell.
func Handler(cfg Config, next http.Handler) http.Handler {
	// DirOrEmbedded prefers an explicit directory and otherwise serves the embedded SPA used by production images.
	spa := spafs.DirOrEmbedded(cfg.SPADir)
	media := newMediaHandler(cfg.ContentDir, cfg.FaceAlias)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path

		/*
		 * Defensive headers for every non-API response this handler produces.
		 *
		 * httpapi.withSecurityHeaders sets the same three, but it wraps only the API
		 * chain: a request answered by a static file never reaches it. Measured
		 * 2026-11-11 — the pages a player actually opens had *none* of them while the
		 * API had all three.
		 *
		 * - X-Frame-Options: DENY — another site cannot frame the room page and
		 * overlay it, which would hijack "start round" / "submit" clicks.
		 * - Referrer-Policy: no-referrer — room codes and invite links stay out of
		 * the Referer header on outbound navigation.
		 * - X-Content-Type-Options: nosniff — no MIME sniffing on served files.
		 *
		 * Set here, once, before the switch, so the SPA shell, hashed assets and
		 * content media all get them. Per-branch copies are how they went missing:
		 * the media branch set nosniff only.
		 */
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")

		switch {
		case strings.HasPrefix(p, "/api/"):
			next.ServeHTTP(w, r)
			return
		case p == "/health/live" || p == "/health/ready" || p == "/metrics":
			next.ServeHTTP(w, r)
			return
		case strings.HasPrefix(p, "/content-assets/"):
			// The content media namespace is unambiguous, so it always belongs to
			// the content pack.
			media.ServeHTTP(w, r)
			return
		case strings.HasPrefix(p, "/assets/"):
			// Both roots use /assets/. The SPA bundle owns its hashed files, so try
			// the SPA first and fall back to content media only when the bundle has
			// no such file. This ordering is why the two roots can share a prefix
			// without shadowing each other.
			if hasFile(spa, p) {
				spa.ServeHTTP(w, r)
				return
			}
			media.ServeHTTP(w, r)
			return
		default:
			spa.ServeHTTP(w, r)
			return
		}
	})
}

// ---- content media -------------------------------------------------------

type mediaHandler struct {
	root  string
	srv   http.Handler
	ok    bool
	alias func(string) (string, bool)
}

func newMediaHandler(contentDir string, alias func(string) (string, bool)) *mediaHandler {
	if contentDir == "" {
		return &mediaHandler{}
	}
	root, err := resolveMediaRoot(contentDir)
	if err != nil {
		return &mediaHandler{}
	}
	return &mediaHandler{root: root, srv: http.FileServer(http.Dir(root)), ok: true, alias: alias}
}

// resolveMediaRoot finds the live public asset directory.
//
// content/current is a file naming the active pack directory; falling back to the
// newest pack-* directory keeps a fresh checkout working before the pointer is
// written.
func resolveMediaRoot(contentDir string) (string, error) {
	if b, err := os.ReadFile(filepath.Join(contentDir, "current")); err == nil {
		name := strings.TrimSpace(string(b))
		if name != "" {
			candidate := filepath.Join(contentDir, name, "public", "assets")
			if info, statErr := os.Stat(candidate); statErr == nil && info.IsDir() {
				return candidate, nil
			}
		}
	}
	entries, err := os.ReadDir(contentDir)
	if err != nil {
		return "", err
	}
	var newest string
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), "pack-") {
			continue
		}
		candidate := filepath.Join(contentDir, e.Name(), "public", "assets")
		if info, statErr := os.Stat(candidate); statErr == nil && info.IsDir() {
			if e.Name() > newest {
				newest = e.Name()
			}
		}
	}
	if newest == "" {
		return "", os.ErrNotExist
	}
	return filepath.Join(contentDir, newest, "public", "assets"), nil
}

func (h *mediaHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !h.ok {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Strip the public prefixes to get a path inside the media root.
	rel := r.URL.Path
	rel = strings.TrimPrefix(rel, "/content-assets")
	rel = strings.TrimPrefix(rel, "/assets")
	rel = strings.TrimPrefix(rel, "/")
	if rel == "" {
		http.NotFound(w, r)
		return
	}

	// A memory board's face token is not a stored file name: it is an opaque alias
	// resolved here, and this is the only place that knows which stored image a token
	// points at . Without this step every face request 404'd, so a flipped card
	// showed a broken image instead of the character.
	if h.alias != nil && strings.HasPrefix(rel, "faces/") {
		token := strings.TrimSuffix(strings.TrimPrefix(rel, "faces/"), path.Ext(rel))
		if stored, ok := h.alias(token); ok && stored != "" {
			rel = stored
		}
	}

	// Path traversal guard: reject anything that escapes the root after cleaning.
	clean := path.Clean("/" + rel)
	if strings.Contains(clean, "..") {
		http.NotFound(w, r)
		return
	}
	trimmed := strings.TrimPrefix(clean, "/")
	full := filepath.Join(h.root, filepath.FromSlash(trimmed))

	absRoot, err := filepath.Abs(h.root)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	absFull, err := filepath.Abs(full)
	if err != nil || absFull == absRoot || !strings.HasPrefix(absFull, absRoot+string(os.PathSeparator)) {
		// Escaping the media root is a 404, never a directory listing or a file
		// from elsewhere on disk .
		http.NotFound(w, r)
		return
	}

	servePath := trimmed
	info, err := os.Stat(absFull)
	if err != nil || info.IsDir() {
		// A memory face token resolves through the caller-supplied alias, which is
		// the only place the token -> stored file mapping lives.
		if rel, ok := h.resolveFaceToken(trimmed); ok {
			servePath = rel
			absFull = filepath.Join(h.root, filepath.FromSlash(rel))
			info, err = os.Stat(absFull)
		}
		if err != nil || info.IsDir() {
			http.NotFound(w, r)
			return
		}
	}

	// Media is versioned by content pack, so it can be cached for a long time.
	w.Header().Set("Cache-Control", "public, max-age=604800")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	// Serve the cleaned relative path through the rooted file server so range
	// requests and content types keep working.
	r2 := r.Clone(r.Context())
	r2.URL.Path = "/" + servePath
	h.srv.ServeHTTP(w, r2)
}

// hasFile reports whether the SPA handler can serve a real file at the URL path.
//
// It is used only to disambiguate the shared /assets/ prefix: the SPA owns its
// content-hashed files, and anything else under /assets/ belongs to the content
// media root. A handler that cannot answer returns false so media gets the request.
func hasFile(h http.Handler, urlPath string) bool {
	type haser interface{ Has(string) bool }
	if hh, ok := h.(haser); ok {
		return hh.Has(urlPath)
	}
	return false
}

// resolveFaceToken maps a per-card memory face route to the pair's stored file.
//
// Only the single opaque segment directly under faces/ is considered, so a token
// can never reach a nested path or a sibling directory.
func (h *mediaHandler) resolveFaceToken(trimmed string) (string, bool) {
	const faceDir = "faces/"
	if h.alias == nil || !strings.HasPrefix(trimmed, faceDir) {
		return "", false
	}
	rest := strings.TrimPrefix(trimmed, faceDir)
	if strings.Contains(rest, "/") {
		return "", false
	}
	ext := path.Ext(rest)
	if ext != ".webp" && ext != ".png" {
		return "", false
	}
	token := strings.TrimSuffix(rest, ext)
	if token == "" {
		return "", false
	}
	rel, ok := h.alias(token)
	if !ok || rel == "" {
		return "", false
	}
	// The resolver returns a path relative to the media root; reject anything that
	// tries to escape it.
	clean := path.Clean("/" + rel)
	if strings.Contains(clean, "..") {
		return "", false
	}
	return strings.TrimPrefix(clean, "/"), true
}
