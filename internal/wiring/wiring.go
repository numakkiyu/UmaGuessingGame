// Package wiring assembles the concrete collaborators the HTTP layer needs.
//
// It is deliberately the only place that knows about all of: the compiled
// content pack, the mode registry, the room service and the HTTP server. Keeping
// that knowledge here means internal/httpapi and internal/room stay testable
// without a real pack, and cmd/server stays a thin process shell.
package wiring

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/bhcn/umaguessinggame/internal/characternames"
	"sort"
	"strings"

	"github.com/bhcn/umaguessinggame/internal/content"
	"github.com/bhcn/umaguessinggame/internal/modes"
	"github.com/bhcn/umaguessinggame/internal/modes/aptitudegrid"
	"github.com/bhcn/umaguessinggame/internal/modes/audio"
	"github.com/bhcn/umaguessinggame/internal/modes/bluff"
	"github.com/bhcn/umaguessinggame/internal/modes/classic"
	"github.com/bhcn/umaguessinggame/internal/modes/cluepuzzle"
	"github.com/bhcn/umaguessinggame/internal/modes/coracepath"
	"github.com/bhcn/umaguessinggame/internal/modes/creative"
	"github.com/bhcn/umaguessinggame/internal/modes/draft"
	"github.com/bhcn/umaguessinggame/internal/modes/memory"
	"github.com/bhcn/umaguessinggame/internal/modes/pedigree"
	"github.com/bhcn/umaguessinggame/internal/modes/racerebuild"
	"github.com/bhcn/umaguessinggame/internal/modes/rapport"
	sortgame "github.com/bhcn/umaguessinggame/internal/modes/sort"
	"github.com/bhcn/umaguessinggame/internal/modes/strategyrace"
	"github.com/bhcn/umaguessinggame/internal/modes/trade"
	"github.com/bhcn/umaguessinggame/internal/room"
)

// AssetBaseURL is the public prefix under which character media is served.
const AssetBaseURL = "/assets/characters/avatars"

// ModeSet is the registry of playable modes plus the ids that were requested but
// could not be compiled from content.
type ModeSet struct {
	Registry *modes.Registry
	// Built lists the mode ids that were registered.
	//
	// Availability is deliberately absent: it is answered by each mode's own admission
	// check at request time (room.ModeAvailability), not cached here.
	Built []string
}

// BuildModes registers every implemented mode.
//
// Availability is decided later by content admission: a mode whose pack section
// is missing reports itself unavailable rather than disappearing silently.
//
// Modes whose answers live in the pack's private tree (race-rebuild ranks) reach
// it through the ContentIndex, which implements modeutil.SecretSource. When no
// private tree is loaded those modes refuse to open with a clear reason instead
// of grading against nothing.
func BuildModes() (*ModeSet, error) {
	candidates := []modes.Mode{
		classic.New(),
		aptitudegrid.New(),
		draft.New(),
		racerebuild.New(),
		coracepath.New(),
		sortgame.New(),
		memory.New(),
		// Register all supported engines, including audio and bluff, whose admission checks reject unreviewed content.
		bluff.New(),
		cluepuzzle.New(),
		creative.New(),
		pedigree.New(),
		rapport.New(),
		strategyrace.New(),
		trade.New(),
		audio.New(),
	}
	reg, err := modes.NewRegistry(candidates...)
	if err != nil {
		return nil, err
	}
	built := reg.IDs()

	// Availability is NOT declared here.
	//
	// A hand-maintained map of unavailable modes used to live at this point. It was
	// never read: the lobby asks room.ModeAvailability, which runs each mode's own
	// admission check against the loaded content pack. The map therefore drifted from
	// reality without anyone noticing — it still listed pedigree as "not implemented"
	// long after pedigree was registered, playable, and passing its content check.
	//
	// Two sources of truth for one question is the defect; the stale entry was only
	// the symptom. A mode now reports itself unavailable through its own admission
	// check, which cannot disagree with what the mode can actually do.
	return &ModeSet{Registry: reg, Built: built}, nil
}

// ContentIndex adapts a compiled content pack to the room service's read-only
// public content interface.
//
// It never exposes private material: the underlying *content.Pack only opens the
// public directory, and every method here returns a projection, not raw pack
// rows .
type ContentIndex struct {
	pack *content.Pack
	// secrets is the pack's private tree (answers, ranks). It is nil when the
	// private directory is missing, which makes answer-dependent modes refuse to
	// open rather than grade against nothing.
	secrets *content.SecretStore

	// characters is the parsed public index, keyed by character id.
	characters map[string]room.CharacterCard
	// order is the sorted character id list used for stable cursor paging.
	order []string
	// neighbours is the co-race-path adjacency, keyed by character id.
	neighbours map[string][]room.Neighbor
}

// OpenContent loads the current published pack and builds the read-only index.
func OpenContent(ctx context.Context, contentDir string) (*ContentIndex, error) {
	pack, err := content.OpenCurrent(contentDir)
	if err != nil {
		return nil, fmt.Errorf("wiring: open content pack: %w", err)
	}
	// The private tree is optional: a pack without answers still serves public
	// content, and answer-dependent modes report themselves unavailable.
	//
	// OpenSecrets takes the PACK directory, not the content root, so the current
	// pointer is resolved first. Passing the root silently failed and made every
	// answer-dependent mode refuse to open.
	var secrets *content.SecretStore
	if packDir, dirErr := content.ResolveCurrent(contentDir); dirErr == nil {
		if sec, secErr := content.OpenSecrets(packDir); secErr == nil {
			secrets = sec
		}
	}
	idx := &ContentIndex{
		pack:       pack,
		secrets:    secrets,
		characters: map[string]room.CharacterCard{},
		neighbours: map[string][]room.Neighbor{},
	}
	if err := idx.loadCharacters(); err != nil {
		return nil, err
	}
	if err := idx.loadNeighbours(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return idx, nil
}

// loadCharacters parses the public character index.
func (c *ContentIndex) loadCharacters() error {
	raw, ok := c.pack.Section("characters.index")
	if !ok {
		// Fall back to the section name the compiler documented.
		raw, ok = c.pack.Section("characters_index")
	}
	if !ok {
		return fmt.Errorf("wiring: content pack has no character index section")
	}
	var rows []struct {
		SearchNames    *characternames.Names `json:"searchNames"`
		ID             string                `json:"id"`
		LegacyID       string                `json:"legacyId"`
		NameZh         string                `json:"nameZh"`
		LocalizedNames map[string]string     `json:"localizedNames"`
		NameJp         string                `json:"nameJp"`
		NameEn         string                `json:"nameEn"`
		Aliases        []string              `json:"aliases"`
		ImagePath      *string               `json:"imagePath"`
		RemoteImageURL string                `json:"remoteImageUrl"`
		Playable       bool                  `json:"playable"`
	}
	if err := json.Unmarshal(raw, &rows); err != nil {
		return fmt.Errorf("wiring: decode character index: %w", err)
	}
	for _, r := range rows {
		// Aliases feed search only; they are public by design .
		aliases := make([]string, 0, len(r.Aliases))
		seen := map[string]bool{}
		for _, a := range r.Aliases {
			a = strings.TrimSpace(a)
			if a == "" || seen[a] {
				continue
			}
			seen[a] = true
			aliases = append(aliases, a)
		}
		sort.Strings(aliases)

		kind := "npc"
		if r.Playable {
			kind = "playable"
		}
		local := ""
		if r.ImagePath != nil {
			local = *r.ImagePath
		}
		c.characters[r.ID] = room.CharacterCard{
			LocalizedNames:  r.LocalizedNames,
			SearchNames:     r.SearchNames,
			CharacterID:     r.ID,
			DisplayName:     r.NameZh,
			Aliases:         aliases,
			EntityKind:      kind,
			PortraitAssetID: r.ID,
			ImagePath:       local,
			RemoteImageURL:  r.RemoteImageURL,
			DatasetVersion:  c.pack.Version(),
		}
		c.order = append(c.order, r.ID)
	}
	sort.Strings(c.order)
	return nil
}

// loadNeighbours parses the co-race-path graph into adjacency lists.
func (c *ContentIndex) loadNeighbours() error {
	raw, ok := c.pack.Section("co-race-path.graph")
	if !ok {
		// The mode is simply unavailable; an empty adjacency is correct here and
		// PathNeighbors will report false.
		return nil
	}
	var graph struct {
		Nodes []struct {
			CharacterID string `json:"characterId"`
			Name        string `json:"name"`
		} `json:"nodes"`
		Edges []struct {
			From     string `json:"from"`
			To       string `json:"to"`
			RaceID   string `json:"raceId"`
			Evidence []struct {
				EvidenceID string `json:"evidenceId"`
				SourceURL  string `json:"sourceUrl"`
				Label      string `json:"label"`
				RaceID     string `json:"raceId"`
			} `json:"evidence"`
		} `json:"edges"`
	}
	if err := json.Unmarshal(raw, &graph); err != nil {
		return fmt.Errorf("wiring: decode co-race-path graph: %w", err)
	}
	nameOf := make(map[string]string, len(graph.Nodes))
	for _, n := range graph.Nodes {
		nameOf[n.CharacterID] = n.Name
	}
	add := func(from, to string, evidence []room.EvidenceRef) {
		c.neighbours[from] = append(c.neighbours[from], room.Neighbor{
			CharacterID: to,
			DisplayName: nameOf[to],
			AssetID:     to,
			Evidence:    evidence,
		})
	}
	for _, e := range graph.Edges {
		ev := make([]room.EvidenceRef, 0, len(e.Evidence))
		for _, x := range e.Evidence {
			ev = append(ev, room.EvidenceRef{
				EvidenceID: x.EvidenceID,
				SourceURL:  x.SourceURL,
				Label:      x.Label,
				RaceID:     x.RaceID,
			})
		}
		if len(ev) == 0 && e.RaceID != "" {
			ev = append(ev, room.EvidenceRef{RaceID: e.RaceID, Label: e.RaceID})
		}
		add(e.From, e.To, ev)
		add(e.To, e.From, ev)
	}
	// Stable ordering keeps the open-book neighbour list deterministic.
	for id := range c.neighbours {
		list := c.neighbours[id]
		sort.Slice(list, func(i, j int) bool { return list[i].CharacterID < list[j].CharacterID })
		c.neighbours[id] = list
	}
	return nil
}

// Section implements modes.ContentSource.
func (c *ContentIndex) Section(name string) (json.RawMessage, bool) { return c.pack.Section(name) }

// Version implements modes.ContentSource.
func (c *ContentIndex) Version() string { return c.pack.Version() }

// DatasetVersion is the canonical dataset version of the loaded pack.
func (c *ContentIndex) DatasetVersion() string {
	if m := c.pack.Manifest(); m != nil {
		return m.DatasetVersion
	}
	return c.pack.Version()
}

// AssetBaseURL is the public prefix for media references.
func (c *ContentIndex) AssetBaseURL() string { return AssetBaseURL }

// Character returns a public character projection by id.
func (c *ContentIndex) Character(id string) (room.CharacterCard, bool) {
	card, ok := c.characters[id]
	return card, ok
}

// Characters returns a page of public character projections filtered by query.
//
// The cursor is the last returned character id, which is opaque to clients and
// stable because order is sorted.
func (c *ContentIndex) Characters(cursor string, limit int, query string) ([]room.CharacterCard, string, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	q := strings.ToLower(strings.TrimSpace(query))

	items := make([]room.CharacterCard, 0, limit)
	next := ""
	for _, id := range c.order {
		if cursor != "" && id <= cursor {
			continue
		}
		card := c.characters[id]
		if q != "" && !matches(card, q) {
			continue
		}
		if len(items) == limit {
			// There is at least one more match; report the cursor of the last
			// item actually returned.
			next = items[len(items)-1].CharacterID
			return items, next, nil
		}
		items = append(items, card)
	}
	return items, "", nil
}

// matches reports whether a card satisfies a lowercase query on any public name.
func matches(card room.CharacterCard, q string) bool {
	if strings.Contains(strings.ToLower(card.DisplayName), q) {
		return true
	}
	for _, a := range card.Aliases {
		if strings.Contains(strings.ToLower(a), q) {
			return true
		}
	}
	return false
}

// PathNeighbors returns content-graph neighbours for an open-book path round.
func (c *ContentIndex) PathNeighbors(characterID string) ([]room.Neighbor, bool) {
	list, ok := c.neighbours[characterID]
	if !ok || len(list) == 0 {
		return nil, false
	}
	out := make([]room.Neighbor, len(list))
	copy(out, list)
	return out, true
}

// ModeAvailability reports, for each registered mode, whether its content section
// compiled with enough usable items.
func (c *ContentIndex) ModeAvailability(reg *modes.Registry) map[string]content.ModeAdmission {
	out := make(map[string]content.ModeAdmission, len(reg.IDs()))
	for _, id := range reg.IDs() {
		if adm, ok := c.pack.ModeAvailable(id); ok {
			out[id] = adm
			continue
		}
		out[id] = content.ModeAdmission{
			Mode:      id,
			Available: false,
			Reason:    "no admission record in the content pack",
		}
	}
	return out
}

// Pack exposes the underlying pack for readiness and diagnostics.
func (c *ContentIndex) Pack() *content.Pack { return c.pack }

// ModeAdmission returns the content compiler's authoritative verdict for a mode.
//
// This is the correct source for availability. Inferring it from
// modes.ContentRequirement.Fields would be wrong: those name canonical fact
// fields (star, aptitudes.distance), not pack section names, so checking them
// with Section() reports every mode unavailable.
func (c *ContentIndex) ModeAdmission(modeID string) (room.ModeAdmission, bool) {
	adm, ok := c.pack.ModeAvailable(modeID)
	if !ok {
		return room.ModeAdmission{}, false
	}
	return room.ModeAdmission{
		Mode:          adm.Mode,
		Available:     adm.Available,
		QuestionCount: adm.QuestionCount,
		Section:       adm.Section,
		Reason:        adm.Reason,
	}, true
}

// Secrets implements modeutil.SecretSource.
//
// The room layer hands PrepareInput.Content the ContentIndex, so a mode that needs
// answer material asserts this interface and reads only the private tree. Public
// callers never see it: Section() on this type answers public sections only.
func (c *ContentIndex) Secrets() modes.ContentSource {
	if c == nil || c.secrets == nil {
		return nil
	}
	return c.secrets
}
