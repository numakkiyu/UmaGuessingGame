package staticserv

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// buildSPA writes a minimal SPA bundle into a temp dir.
func buildSPA(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	must := func(rel, body string) {
		full := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	must("index.html", "<!doctype html><title>shell</title>")
	must("assets/app-abc123.js", "console.log('app')")
	must("favicon.ico", "icon")
	return dir
}

// buildContent writes a fake published pack with media files.
func buildContent(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	pack := filepath.Join(dir, "pack-test1")
	must := func(rel, body string) {
		full := filepath.Join(pack, "public", filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	must("assets/faces/mf-aaa.png", "PNGDATA")
	must("assets/avatars/1001.png", "AVATAR")
	must("assets/audio/voice.webm", "WEBM_OPUS")
	// A secret outside the pack that traversal must never reach.
	if err := os.WriteFile(filepath.Join(dir, "secret.txt"), []byte("TOPSECRET"), 0o600); err != nil {
		t.Fatalf("write secret: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "current"), []byte("pack-test1"), 0o644); err != nil {
		t.Fatalf("write current: %v", err)
	}
	return dir
}

func newHandler(t *testing.T) http.Handler {
	t.Helper()
	api := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte(`{"from":"api"}`))
	})
	return Handler(Config{SPADir: buildSPA(t), ContentDir: buildContent(t)}, api)
}

func get(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func TestOpusWebMAudioTypeAndRange(t *testing.T) {
	h := newHandler(t)
	request := httptest.NewRequest(http.MethodGet, "/assets/audio/voice.webm", nil)
	request.Header.Set("Range", "bytes=0-3")
	response := httptest.NewRecorder()
	h.ServeHTTP(response, request)
	if response.Code != http.StatusPartialContent || response.Body.String() != "WEBM" ||
		response.Header().Get("Content-Type") != "audio/webm; codecs=opus" ||
		response.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("audio range response: status=%d headers=%v body=%q", response.Code, response.Header(), response.Body.String())
	}
}

func TestMediaRejectsOtherAudioFormats(t *testing.T) {
	spa, content := buildSPA(t), buildContent(t)
	h := Handler(Config{SPADir: spa, ContentDir: content}, http.NotFoundHandler())
	for _, ext := range []string{".mp3", ".m4a", ".ogg", ".opus", ".wav", ".aac", ".flac"} {
		file := filepath.Join(content, "pack-test1", "public", "assets", "audio", "voice"+ext)
		if err := os.WriteFile(file, []byte("legacy media"), 0644); err != nil {
			t.Fatal(err)
		}
		if response := get(t, h, "/assets/audio/voice"+ext); response.Code != http.StatusNotFound {
			t.Errorf("%s: got %d, want 404", ext, response.Code)
		}
	}
}

// TestAPIPathsAreNeverAnsweredWithHTML is the core routing guarantee: an API or
// missing-asset request must not receive the SPA shell, because that turns a 404
// into a fake success .
func TestAPIPathsAreNeverAnsweredWithHTML(t *testing.T) {
	h := newHandler(t)

	rec := get(t, h, "/api/v1/rooms/abc")
	if rec.Code != http.StatusTeapot {
		t.Errorf("/api status = %d, want the API handler's 418", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "<!doctype html>") {
		t.Error("API path was answered with the SPA shell")
	}

	// A missing asset with an extension must 404, not fall back to the shell.
	rec = get(t, h, "/assets/missing-file.js")
	if rec.Code != http.StatusNotFound {
		t.Errorf("missing asset status = %d, want 404", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "shell") {
		t.Error("missing asset was answered with the SPA shell")
	}
}

// TestDeepLinkServesShell proves a client route survives a direct refresh.
func TestDeepLinkServesShell(t *testing.T) {
	h := newHandler(t)
	for _, p := range []string{"/", "/rooms/rom_1", "/events/ev_1/host", "/help"} {
		rec := get(t, h, p)
		if rec.Code != http.StatusOK {
			t.Errorf("%s status = %d, want 200", p, rec.Code)
			continue
		}
		if !strings.Contains(rec.Body.String(), "shell") {
			t.Errorf("%s did not serve the shell", p)
		}
		if got := rec.Header().Get("Cache-Control"); !strings.Contains(got, "no-cache") {
			t.Errorf("%s Cache-Control = %q, want no-cache so a stale shell cannot point at deleted bundles", p, got)
		}
	}
}

// TestHashedAssetsAreImmutable checks the cache policy for content-hashed files.
func TestHashedAssetsAreImmutable(t *testing.T) {
	h := newHandler(t)
	rec := get(t, h, "/assets/app-abc123.js")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Cache-Control"); !strings.Contains(got, "immutable") {
		t.Errorf("Cache-Control = %q, want immutable for a hashed bundle", got)
	}
}

// TestContentMediaServed proves both public prefixes resolve to the same media
// root, so a pack that references either prefix works.
func TestContentMediaServed(t *testing.T) {
	h := newHandler(t)
	for _, p := range []string{
		"/content-assets/faces/mf-aaa.png",
		"/assets/faces/mf-aaa.png",
		"/assets/avatars/1001.png",
	} {
		rec := get(t, h, p)
		if rec.Code != http.StatusOK {
			t.Errorf("%s status = %d, want 200", p, rec.Code)
			continue
		}
		if rec.Body.Len() == 0 {
			t.Errorf("%s returned an empty body", p)
		}
		if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
			t.Errorf("%s X-Content-Type-Options = %q, want nosniff", p, got)
		}
	}
}

// TestPathTraversalIsRefused is the security-critical case: a crafted media path
// must never read a file outside the media root .
func TestPathTraversalIsRefused(t *testing.T) {
	h := newHandler(t)
	paths := []string{
		"/assets/../secret.txt",
		"/assets/../../secret.txt",
		"/content-assets/../secret.txt",
		"/assets/faces/../../secret.txt",
		"/assets/%2e%2e/secret.txt",
		"/assets/faces/..%2f..%2fsecret.txt",
	}
	for _, p := range paths {
		rec := get(t, h, p)
		if strings.Contains(rec.Body.String(), "TOPSECRET") {
			t.Errorf("%s leaked a file outside the media root", p)
		}
		if rec.Code == http.StatusOK && strings.Contains(rec.Body.String(), "TOPSECRET") {
			t.Errorf("%s served the secret with 200", p)
		}
	}
}

// TestMissingMediaRootIs404 proves a deployment without a content pack does not
// accidentally serve the SPA shell for a media URL.
func TestMissingMediaRootIs404(t *testing.T) {
	h := Handler(Config{SPADir: buildSPA(t), ContentDir: t.TempDir()}, http.NotFoundHandler())
	rec := get(t, h, "/assets/faces/mf-aaa.png")
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404 when no pack is published", rec.Code)
	}
}

// TestEmptySPADirFallsBackToEmbedded verifies that an unset SPADir serves the embedded bundle.
func TestEmptySPADirFallsBackToEmbedded(t *testing.T) {
	h := Handler(Config{SPADir: "", ContentDir: ""}, http.NotFoundHandler())
	rec := get(t, h, "/rooms/rom_1")
	// A real build is embedded in this repository, so the shell must be served.
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 from the embedded SPA", rec.Code)
	}
	if !strings.Contains(strings.ToLower(rec.Body.String()), "<!doctype html") {
		t.Error("the embedded SPA did not return an HTML document")
	}
}

// TestSPADirWithoutIndexIsIgnored proves a directory that exists but has no
// index.html is not treated as a build: a half-populated dist would otherwise
// serve 404s for every route.
func TestSPADirWithoutIndexIsIgnored(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "stray.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	h := Handler(Config{SPADir: dir, ContentDir: ""}, http.NotFoundHandler())
	rec := get(t, h, "/rooms/rom_1")
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 from the embedded fallback", rec.Code)
	}
}

// TestMethodNotAllowed proves non-GET methods are refused on static paths.
func TestMethodNotAllowed(t *testing.T) {
	h := newHandler(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/help", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST status = %d, want 405", rec.Code)
	}
}

func TestSPARejectsUntrustedScripts(t *testing.T) {
	h := Handler(Config{SPADir: buildSPA(t)}, http.NotFoundHandler())
	rec := get(t, h, "/matchmaking")
	policy := rec.Header().Get("Content-Security-Policy")
	for _, part := range []string{"script-src 'self' https://challenges.cloudflare.com", "worker-src 'self'", "object-src 'none'", "base-uri 'none'", "frame-ancestors 'none'"} {
		if !strings.Contains(policy, part) {
			t.Fatalf("missing directive %s", part)
		}
	}
	script := strings.Split(strings.Split(policy, "script-src ")[1], ";")[0]
	if strings.Contains(script, "'unsafe-inline'") || strings.Contains(script, "'unsafe-eval'") {
		t.Fatal("script policy permits inline execution")
	}
}

func TestPublishedMediaRejectsActiveFilesAndEscapingSymlinks(t *testing.T) {
	content := buildContent(t)
	root := filepath.Join(content, "pack-test1", "public", "assets")
	for _, name := range []string{"evil.html", "evil.svg", "evil.js", "private.json", "secret.db"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("private"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(content, "secret.txt"), filepath.Join(root, "escape.png")); err != nil {
		t.Fatal(err)
	}
	h := Handler(Config{SPADir: buildSPA(t), ContentDir: content}, http.NotFoundHandler())
	for _, name := range []string{"evil.html", "evil.svg", "evil.js", "private.json", "secret.db", "escape.png"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", "/content-assets/"+name, nil))
		if rec.Code != 404 {
			t.Fatalf("%s served: %d", name, rec.Code)
		}
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/content-assets/faces/mf-aaa.png", nil)
	req.Header.Set("Range", "bytes=0-2")
	h.ServeHTTP(rec, req)
	if rec.Code != 206 || rec.Body.String() != "PNG" {
		t.Fatalf("media range regressed: %d %q", rec.Code, rec.Body.String())
	}
}
