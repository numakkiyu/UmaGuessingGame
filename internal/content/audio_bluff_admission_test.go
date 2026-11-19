package content

import (
	"os"
	"path/filepath"
	"testing"
)

// TestAudioClipRequiresAFileOnDisk pins the rule the audio mode depends on.
//
// The wiki snapshot records remote CDN URLs only. Audio mode needs a decodable local
// file, so a clip counts only when its bytes are actually present. The check used to
// be missing entirely: HasLocalFile was declared, read, and never assigned, so the
// counter was permanently zero and the mode could not become available no matter how
// many clips were downloaded. That is a judgement bug, not a missing asset, and it
// would have survived any amount of extra material.
func TestAudioClipRequiresAFileOnDisk(t *testing.T) {
	// hasLocalAudio resolves against a repo-relative directory, so the fixture is
	// created there and removed afterwards. Using t.TempDir would test a path the
	// function never looks at.
	name := "zz-test-clip-" + t.Name() + ".webm"
	full := filepath.Join(audioDir, name)
	if err := os.MkdirAll(audioDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(full) })
	_ = os.Remove(full)

	ctx := &compileContext{}
	if ctx.hasLocalAudio("https://cdn.example/x/y/" + name) {
		t.Error("a clip with no file on disk must not count")
	}

	// A zero-byte file is a failed download, not material: counting it would offer
	// silence as a playable clip.
	if err := os.WriteFile(full, nil, 0o644); err != nil {
		t.Fatalf("write empty: %v", err)
	}
	if ctx.hasLocalAudio("https://cdn.example/x/y/" + name) {
		t.Error("a zero-byte file must not count")
	}

	if err := os.WriteFile(full, []byte("ID3realbytes"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if !ctx.hasLocalAudio("https://cdn.example/x/y/" + name) {
		t.Error("a non-empty file must count")
	}
	// A cache-busting query string must not become part of the file name.
	if !ctx.hasLocalAudio("https://cdn.example/x/y/" + name + "?v=2") {
		t.Error("a query string must be ignored when deriving the file name")
	}
	if ctx.hasLocalAudio("") {
		t.Error("an empty URL must not count")
	}
}

// TestBluffFactRequiresNamedReviewerAndCitation pins the review gate.
//
// Bluff presents one fact as true. Publishing a fact nobody approved would assert an
// unverified claim as game content, so the gate is deliberately strict: approval needs
// approved=true, a citation, and a named reviewer. Any of those missing leaves the
// fact pending, and pending is not consent.
func TestBluffFactRequiresNamedReviewerAndCitation(t *testing.T) {
	cases := []struct {
		name     string
		review   RawBluffReviews
		wantOK   bool
		whyShort string
	}{
		{
			name:   "approved with citation and reviewer",
			review: RawBluffReviews{Reviewer: "lead", Facts: []RawBluffReview{{FactID: "1001-1", Approved: true, Source: "wiki"}}},
			wantOK: true,
		},
		{
			name:     "approved but no citation",
			review:   RawBluffReviews{Reviewer: "lead", Facts: []RawBluffReview{{FactID: "1001-1", Approved: true}}},
			wantOK:   false,
			whyShort: "citation",
		},
		{
			name:     "approved but no reviewer",
			review:   RawBluffReviews{Facts: []RawBluffReview{{FactID: "1001-1", Approved: true, Source: "wiki"}}},
			wantOK:   false,
			whyShort: "reviewer",
		},
		{
			name:     "not approved",
			review:   RawBluffReviews{Reviewer: "lead", Facts: []RawBluffReview{{FactID: "1001-1", Source: "wiki"}}},
			wantOK:   false,
			whyShort: "approval",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ctx := &compileContext{raw: &RawInputs{BluffReviews: c.review}}
			idx := ctx.bluffReviewIndex()
			r, found := idx["1001-1"]
			got := found && r.Approved && r.Source != "" && ctx.reviewerName() != ""
			if got != c.wantOK {
				t.Errorf("publishable = %v, want %v (missing %s)", got, c.wantOK, c.whyShort)
			}
		})
	}
}

// TestAudioClipPublishesALocalRoute pins that a downloaded clip is served by this
// deployment rather than hot-linked from the wiki.
//
// requires the game to work without reaching a third party. A published
// audioUrl pointing at the CDN would make the mode depend on a URL that can change,
// expire, or be blocked — and would fail entirely offline. The route is derived from
// the URL's last segment, which is the CDN hash: unique per clip, stable across a
// change of host.
func TestAudioClipPublishesALocalRoute(t *testing.T) {
	cases := []struct {
		url  string
		want string
	}{
		{"https://cdn.example/a/b/clip1.webm", "/assets/audio/clip1.webm"},
		{"https://cdn.example/a/b/clip2.webm?v=3", "/assets/audio/clip2.webm"},
		{"https://other.host/x/y/clip3.webm", "/assets/audio/clip3.webm"},
		{"https://cdn.example/a/b/clip1.mp3", "/assets/audio/clip1.webm"},
		{"https://cdn.example/a/b/clip1.mp3?v=/other.webm", "/assets/audio/clip1.webm"},
		{"", ""},
	}
	for _, c := range cases {
		if got := audioRouteFor(c.url); got != c.want {
			t.Errorf("audioRouteFor(%q) = %q, want %q", c.url, got, c.want)
		}
	}
}
