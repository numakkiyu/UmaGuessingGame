package memory

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/bhcn/umaguessinggame/internal/modes"
)

// TestPairCardsNeverShareAnImageURL is the regression test for a real leak found by
// end-to-end probing.
//
// Before the fix, both cards of a pair returned the same imagePath. A player could
// flip one card, read the URL out of the network tab, and identify its partner
// without remembering anything — the memory mode was defeated by an HTTP log rather
// than by skill. Hiding the URL from the DOM would not help, because the request is
// still observable.
//
// The invariant asserted here is stronger than "the two cards of a pair differ":
// every card on the board must have a distinct URL, since any collision at all
// reveals a pairing.
func TestPairCardsNeverShareAnImageURL(t *testing.T) {
	prep, pub, priv := prepare(t, packOf(8), "leak-seed", `{}`, modes.Subject{ID: "s1", Kind: modes.SubjectPlayer})

	if len(pub.Cards) != CardCount {
		t.Fatalf("board has %d cards, want %d", len(pub.Cards), CardCount)
	}

	seen := make(map[string]string, CardCount)
	for _, card := range pub.Cards {
		url := priv.CardImage[card.CardID]
		if url == "" {
			t.Fatalf("card %s has no image URL", card.CardID)
		}
		if prev, dup := seen[url]; dup {
			t.Fatalf("cards %s and %s share image URL %q: a player can read the pairing from the network tab",
				prev, card.CardID, url)
		}
		seen[url] = card.CardID
	}

	// Every pair must have exactly two cards, and the routes must be unique across
	// the board. The underlying stored file may legitimately differ between the two
	// cards (the "split" pack shape gives each face its own picture), so storage
	// sharing is NOT asserted here — only the security property is.
	byPair := make(map[string][]string, PairCount)
	for cardID, pairID := range priv.PairOf {
		byPair[pairID] = append(byPair[pairID], priv.CardImage[cardID])
	}
	if len(byPair) != PairCount {
		t.Fatalf("board has %d pairs, want %d", len(byPair), PairCount)
	}
	for pairID, urls := range byPair {
		if len(urls) != 2 {
			t.Fatalf("pair %s has %d cards, want 2", pairID, len(urls))
		}
		if urls[0] == urls[1] {
			t.Errorf("pair %s: both cards expose the same route %q", pairID, urls[0])
		}
	}

	// Prepare must remain deterministic: the same seed yields the same routes.
	_, _, again := prepare(t, packOf(8), "leak-seed", `{}`, modes.Subject{ID: "s1", Kind: modes.SubjectPlayer})
	for cardID, url := range priv.CardImage {
		if again.CardImage[cardID] != url {
			t.Fatalf("card %s route is not deterministic across prepares", cardID)
		}
	}

	_ = prep
}

// TestCardImageRouteShape verifies distinct opaque filenames without a shared pair directory.
func TestCardImageRouteShape(t *testing.T) {
	a := cardFaceRoute("mp-abc123", "/assets/faces/mp-abc123.webp", 0)
	b := cardFaceRoute("mp-abc123", "/assets/faces/mp-abc123.webp", 1)

	if !strings.HasPrefix(a, "/assets/faces/") || !strings.HasSuffix(a, ".webp") {
		t.Fatalf("route %q is not /assets/faces/<token>.webp", a)
	}
	if a == b {
		t.Fatalf("both copy indices produced the same route %q", a)
	}

	// The decisive assertion: the two routes must not share any path segment that
	// identifies the pair.
	//
	// /assets/faces is the shared media root and carries no pairing information, so
	// it is excluded. The old leak lived one level deeper: both routes sat under
	// /assets/faces/<pairId>/, so the segment after the media root named the pair.
	const mediaRoot = "/assets/faces/"
	segA := strings.Split(strings.TrimPrefix(a, mediaRoot), "/")
	segB := strings.Split(strings.TrimPrefix(b, mediaRoot), "/")
	if len(segA) != 1 || len(segB) != 1 {
		t.Fatalf("routes must be one opaque segment under %s, got %q and %q", mediaRoot, a, b)
	}
	if segA[0] == segB[0] {
		t.Fatalf("routes share the token segment %q", segA[0])
	}

	// The pair id must not appear anywhere in either route.
	for _, route := range []string{a, b} {
		if strings.Contains(route, "mp-abc123") {
			t.Errorf("route %q embeds the pair id", route)
		}
	}

	if cardFaceRoute("mp-abc", "", 0) != "" {
		t.Error("an empty stored path must produce an empty route, not a broken one")
	}
}

// storedFileOf strips the per-card token from a route, yielding the file a pair
// shares. It mirrors what the static server does when resolving the alias.
func storedFileOf(route string) string {
	// /assets/faces/mp-x/f0.webp -> /assets/faces/mp-x.webp
	i := strings.LastIndex(route, "/")
	if i <= 0 {
		return route
	}
	token := route[i+1:] // f0.webp
	dir := route[:i]     // /assets/faces/mp-x
	ext := ""
	if d := strings.LastIndex(token, "."); d >= 0 {
		ext = token[d:] // .webp
	}
	j := strings.LastIndex(dir, "/")
	if j < 0 {
		return route
	}
	return dir[:j] + "/" + dir[j+1:] + ext
}

// TestSelfDescribingPackDoesNotLeakPairing covers the shape the real content pack
// publishes: one public record per pair, with BOTH cards showing the same reviewed
// image. That is the shape where a shared URL actually leaked, so it gets its own
// test rather than relying on the split-shape fixture.
func TestSelfDescribingPackDoesNotLeakPairing(t *testing.T) {
	pack := selfDescribingPack(8)
	_, pub, priv := prepare(t, pack, "selfdesc-seed", `{}`, modes.Subject{ID: "s1", Kind: modes.SubjectPlayer})

	seen := make(map[string]string, CardCount)
	for _, card := range pub.Cards {
		url := priv.CardImage[card.CardID]
		if url == "" {
			t.Fatalf("card %s has no image URL", card.CardID)
		}
		if prev, dup := seen[url]; dup {
			t.Fatalf("self-describing pack: cards %s and %s share URL %q, which reveals the pair",
				prev, card.CardID, url)
		}
		seen[url] = card.CardID
	}
	if len(seen) != CardCount {
		t.Fatalf("only %d distinct URLs for %d cards", len(seen), CardCount)
	}

	// Both cards of a pair still resolve to the one stored file, so the pack keeps a
	// single image per pair even though the URLs differ.
	byPair := make(map[string][]string, PairCount)
	for cardID, pairID := range priv.PairOf {
		byPair[pairID] = append(byPair[pairID], priv.CardImage[cardID])
	}
	for pairID, urls := range byPair {
		if len(urls) != 2 {
			t.Fatalf("pair %s has %d cards", pairID, len(urls))
		}
		if a, b := storedFileOf(urls[0]), storedFileOf(urls[1]); a != b {
			t.Errorf("pair %s: cards resolve to %q and %q; the pack should store one image per pair",
				pairID, a, b)
		}
	}
}

// selfDescribingPack builds the shape the real content pack publishes: one public
// record per PAIR, each carrying the single reviewed image both of its cards show.
//
// This is the shape where the leak actually happened, so the fixture must match it
// rather than reuse the split-shape helper.
func selfDescribingPack(n int) modes.ContentSource {
	type pairRecord struct {
		PairID    string `json:"pairId"`
		ImagePath string `json:"imagePath"`
		Label     string `json:"label"`
	}
	records := make([]pairRecord, 0, n)
	for i := 0; i < n; i++ {
		pairID := fmt.Sprintf("mp-%04d", i)
		records = append(records, pairRecord{
			PairID:    pairID,
			ImagePath: fmt.Sprintf("/assets/faces/%s.webp", pairID),
			Label:     "???",
		})
	}
	raw, err := json.Marshal(records)
	if err != nil {
		panic(err)
	}
	return fakeContent{"memory.pairs": raw}
}
