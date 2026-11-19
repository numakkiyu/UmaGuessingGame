package content

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	assetutil "github.com/bhcn/umaguessinggame/internal/assets"
)

// avatarDir contains tracked avatars, so clean checkouts can build content packs.
const avatarDir = "data/assets/characters/avatars"

// joinStats records how the joins between artifacts actually resolved. Every
// number here is computed, never assumed: the report must show the real figure
// even when it is unflattering .
type joinStats struct {
	ReadyEntries    int
	BlockedEntries  int
	NormalizedFiles int
	WikiRecords     int

	ReadyMatchedWiki       int
	ReadyUnmatched         []string
	BlockedMatchedWiki     int
	BlockedUnmatched       []string
	NormalizedMatchedWiki  int
	NormalizedUnmatched    []string
	ReadyMatchedNormalized int

	// LegacyMapped counts ready entries that resolved to a canonical id.
	LegacyMapped int

	// WikiNotInAnyBank lists snapshot records that neither bank covers.
	WikiNotInAnyBank []string
}

// BuildDataset turns raw inputs into the canonical dataset.
func BuildDataset(in *RawInputs, generatedAt, datasetVersion, root string) (*Dataset, *joinStats, error) {
	if err := validateKnowledge(in); err != nil {
		return nil, nil, err
	}
	if err := validateFieldOverrides(in); err != nil {
		return nil, nil, err
	}
	st := &joinStats{
		ReadyEntries:    len(in.Ready),
		BlockedEntries:  len(in.Blocked),
		NormalizedFiles: len(in.Normalized),
		WikiRecords:     len(in.Wiki.Characters),
	}

	// Index the wiki snapshot by canonical id and by Japanese name. The Japanese
	// name is the only slot shared with the legacy artifacts, so it is the join
	// key; display names are never used as a foreign key .
	wikiByID := make(map[string]*RawWikiCharacter, len(in.Wiki.Characters))
	wikiByJa := make(map[string]*RawWikiCharacter, len(in.Wiki.Characters))
	for i := range in.Wiki.Characters {
		w := &in.Wiki.Characters[i]
		if w.ID == "" {
			return nil, nil, fmt.Errorf("content: empty canonical id in wiki snapshot")
		}
		if _, exists := wikiByID[w.ID]; exists {
			return nil, nil, fmt.Errorf("content: duplicate canonical id %q in wiki snapshot", w.ID)
		}
		wikiByID[w.ID] = w
		if w.Names.Ja != "" {
			if _, dup := wikiByJa[w.Names.Ja]; dup {
				return nil, nil, fmt.Errorf("content: duplicate Japanese name %q in wiki snapshot", w.Names.Ja)
			}
			wikiByJa[w.Names.Ja] = w
		}
	}

	// Index normalised files by romanised id and by file name.
	normByID := make(map[string]*RawNormalizedCharacter, len(in.Normalized))
	normByFile := make(map[string]*RawNormalizedCharacter, len(in.Normalized))
	for i := range in.Normalized {
		n := &in.Normalized[i]
		if _, exists := normByID[n.ID]; n.ID != "" && exists {
			return nil, nil, fmt.Errorf("content: duplicate normalized id %q", n.ID)
		}
		if _, exists := normByFile[n.FileName]; n.FileName != "" && exists {
			return nil, nil, fmt.Errorf("content: duplicate normalized filename %q", n.FileName)
		}
		normByID[n.ID] = n
		normByFile[n.FileName] = n
	}

	// Resolve every wiki record to (optional) legacy identity.
	legacyByCanonical := make(map[string]*legacyEntry, len(in.Ready)+len(in.Blocked))

	for i := range in.Ready {
		r := &in.Ready[i]
		w := wikiByJa[r.NameJp]
		if w == nil {
			st.ReadyUnmatched = append(st.ReadyUnmatched, r.ID+"("+r.NameZh+")")
			continue
		}
		st.ReadyMatchedWiki++
		li := legacyByCanonical[w.ID]
		if li == nil {
			li = &legacyEntry{}
			legacyByCanonical[w.ID] = li
		}
		li.romanised = r.ID
		li.ready = r
		if n := normByID[r.ID]; n != nil {
			li.norm = n
			li.order = n.Order
			st.ReadyMatchedNormalized++
		} else {
			st.ReadyUnmatched = append(st.ReadyUnmatched, r.ID+"(no normalized file)")
		}
		st.LegacyMapped++
	}

	for i := range in.Blocked {
		b := &in.Blocked[i]
		n := normByFile[b.FileName]
		if n == nil {
			st.BlockedUnmatched = append(st.BlockedUnmatched, b.FileName+"(no normalized file)")
			continue
		}
		w := wikiByJa[n.Names.Jp]
		if w == nil {
			st.BlockedUnmatched = append(st.BlockedUnmatched, b.FileName+"("+n.Names.Jp+")")
			continue
		}
		st.BlockedMatchedWiki++
		li := legacyByCanonical[w.ID]
		if li == nil {
			li = &legacyEntry{}
			legacyByCanonical[w.ID] = li
		}
		if li.romanised == "" {
			li.romanised = b.ID
		}
		li.blocked = b
		li.norm = n
		li.order = b.Order
	}

	for i := range in.Normalized {
		n := &in.Normalized[i]
		if w := wikiByJa[n.Names.Jp]; w != nil {
			st.NormalizedMatchedWiki++
			li := legacyByCanonical[w.ID]
			if li == nil {
				li = &legacyEntry{}
				legacyByCanonical[w.ID] = li
			}
			if li.romanised == "" {
				li.romanised = n.ID
			}
			if li.norm == nil {
				li.norm = n
			}
			if li.order == 0 {
				li.order = n.Order
			}
		} else {
			st.NormalizedUnmatched = append(st.NormalizedUnmatched, n.FileName+"("+n.Names.Jp+")")
		}
	}

	// Verify avatar files on disk rather than trusting cache flags
	assetState := verifyAssets(in, root, datasetVersion)

	// Skill definitions, used only to measure attribution-name matching.
	skillNames := make(map[string]int, len(in.Skills.Skills))
	for _, s := range in.Skills.Skills {
		skillNames[s.NameZh]++
	}

	ds := &Dataset{
		SchemaVersion:  SchemaVersion,
		DatasetVersion: datasetVersion,
		GeneratedAt:    generatedAt,
		Locale:         "zh-Hans",
		Region:         "CN",
		ByLegacyID:     make(map[string]string, len(legacyByCanonical)),
		ByNumericID:    make(map[string]string, len(legacyByCanonical)),
		Sources:        in.Snapshots,
		Characters:     make([]Character, 0, len(in.Wiki.Characters)),
	}

	// Classic ambiguity: characters whose reviewed nine-field vector is identical
	// are indistinguishable and must be excluded from competitive packs .
	groupSignature := make(map[string][]string)
	for _, r := range in.Ready {
		g := groupsFromReady(r, in.Overrides[r.ID])
		if len(g.Missing()) != 0 {
			continue
		}
		groupSignature[g.Signature()] = append(groupSignature[g.Signature()], r.NameJp)
	}
	ambiguity := make(map[string]string) // ja name -> group label
	groupLabels := make(map[string]string)
	for sig, members := range groupSignature {
		if len(members) < 2 {
			continue
		}
		sort.Strings(members)
		label := "amb-" + shortHash(sig)
		groupLabels[sig] = label
		for _, ja := range members {
			ambiguity[ja] = label
		}
	}

	// Prototype/race aggregation happens once over the snapshot.
	races := buildRaces(in.Wiki.Characters)
	if err := applyRaceCorrections(races, in.RaceCorrections); err != nil {
		return nil, nil, err
	}

	for i := range in.Wiki.Characters {
		w := &in.Wiki.Characters[i]
		c, err := buildCharacter(w, legacyByCanonical[w.ID], in.Overrides, assetState, skillNames)
		if err != nil {
			return nil, nil, err
		}
		if label, ok := ambiguity[w.Names.Ja]; ok {
			c.ReviewNotes = append(c.ReviewNotes,
				"classic nine-field vector is identical to another character ("+label+"); excluded from competitive classic packs")
		}
		ds.Characters = append(ds.Characters, *c)
		if len(c.LegacyIDs) > 0 {
			ds.ByLegacyID[c.LegacyIDs[0]] = c.ID
			ds.ByNumericID[c.ID] = c.LegacyIDs[0]
		}
	}

	sort.Slice(ds.Characters, func(i, j int) bool { return ds.Characters[i].ID < ds.Characters[j].ID })

	// Outfits: attached to their character and also published flat, because a
	// variant is its own identity and must never be a different answer .
	outfitByChar := make(map[string][]Outfit)
	for _, o := range in.Outfits.Outfits {
		of := Outfit{
			OutfitID:       o.OutfitID,
			CharacterID:    o.CharacterID,
			NicknameJp:     o.NicknameJp,
			NicknameZhHans: o.NicknameZhHans,
			NicknameZhHant: o.NicknameZhHant,
			Title:          o.Title,
			Type:           o.Type,
			Star:           o.Star,
			Implemented:    o.Implemented,
		}
		ds.Outfits = append(ds.Outfits, of)
		outfitByChar[o.CharacterID] = append(outfitByChar[o.CharacterID], of)
	}
	for i := range ds.Characters {
		if v, ok := outfitByChar[ds.Characters[i].ID]; ok {
			sort.Slice(v, func(a, b int) bool { return v[a].OutfitID < v[b].OutfitID })
			ds.Characters[i].Outfits = v
		}
	}

	ds.Races = races

	for _, w := range in.Wiki.Characters {
		if _, ok := legacyByCanonical[w.ID]; !ok {
			st.WikiNotInAnyBank = append(st.WikiNotInAnyBank, w.ID+"("+w.Names.ZhHans+"/"+w.Implementation.Status+")")
		}
	}
	sort.Strings(st.WikiNotInAnyBank)
	sort.Strings(st.ReadyUnmatched)
	sort.Strings(st.BlockedUnmatched)
	sort.Strings(st.NormalizedUnmatched)

	return ds, st, nil
}

// buildCharacter maps one wiki record plus its legacy identity into the
// canonical model, deciding entity kind and playability itself.
func buildCharacter(
	w *RawWikiCharacter,
	li *legacyEntry,
	overrides RawFieldOverrides,
	assets assetVerification,
	skillNames map[string]int,
) (*Character, error) {
	c := &Character{
		ID:         w.ID,
		Slug:       w.Slug,
		Names:      namesFromWiki(w),
		Profile:    profileFromWiki(w, li, overrides),
		Sources:    sourcesFromWiki(w),
		Confidence: ConfidenceConfirmed,
	}

	// Aptitudes come only from the game data block; when it is absent the maps
	// stay nil so a consumer can distinguish "unknown" from "all G" .
	if a := w.Game.Aptitudes; a.Surface != nil || a.Distance != nil || a.RunningStyle != nil {
		c.Aptitudes = Aptitudes{
			Surface:      a.Surface,
			Distance:     a.Distance,
			RunningStyle: a.RunningStyle,
		}
	}

	// Aliases: every non-empty name slot and every recorded variant, deduped by
	// text, with the confidence that slot carries.
	c.Aliases = aliasesFromWiki(w)
	c.SearchNames = searchNamesFromSources(w, li)
	aliasTexts := make(map[string]bool, len(c.Aliases))
	for _, alias := range c.Aliases {
		aliasTexts[alias.Text] = true
	}
	for _, nickname := range c.SearchNames.Nicknames {
		if !aliasTexts[nickname] {
			c.Aliases = append(c.Aliases, Alias{Text: nickname, Locale: "zh-Hans", Confidence: ConfidenceConfirmed})
			aliasTexts[nickname] = true
		}
	}

	if li != nil {
		if li.romanised != "" {
			c.LegacyIDs = []string{li.romanised}
		}
		if li.ready != nil {
			g := groupsFromReady(*li.ready, overrides[li.romanised])
			c.Groups = &g
			if ov := overrides[li.romanised]; ov["racing_source_url"] != "" {
				c.Sources = append(c.Sources, SourceRef{Site: "racing-correction", URL: ov["racing_source_url"]})
				c.ReviewNotes = append(c.ReviewNotes, ov["racing_note"]+" ("+ov["racing_checked_at"]+")")
			}
			c.AssetID = li.ready.AssetID
		}
		if li.blocked != nil {
			c.MissingFields = append([]string(nil), li.blocked.MissingFields...)
		}
	}

	// Entity kind and playability are decided here, not copied from
	// implementation.status . The order matters: a planned character is
	// planned even though it has no game block, so the planned check must precede
	// the "no in-game data therefore cast" heuristic.
	switch {
	case strings.HasPrefix(w.ID, "mg-"):
		// Records merged from the secondary wiki with no in-game data: they are
		// cast, not trainable characters.
		c.EntityKind = EntityNPC
		c.PlayableStatus = PlayableNotPlayable
		c.BlockReason = "merged from secondary wiki without in-game data (no game block, no outfit)"
		c.Confidence = ConfidenceInferred
	case w.Implementation.Status == "planned":
		c.EntityKind = EntityPlanned
		c.PlayableStatus = PlayableNotPlayable
		c.BlockReason = "announced but not implemented in game (no aptitudes or stats)"
		c.Confidence = ConfidenceConfirmed
	case !w.Implementation.InGameCategory && isEmptyGame(w.Game.Aptitudes):
		c.EntityKind = EntityNPC
		c.PlayableStatus = PlayableNotPlayable
		c.BlockReason = "not in the in-game character category and has no game data"
		c.Confidence = ConfidenceInferred
	default:
		c.EntityKind = EntityPlayable
	}

	// Playability for real characters is a field-completeness verdict.
	if c.EntityKind == EntityPlayable {
		if c.Groups != nil && len(c.Groups.Missing()) == 0 {
			c.PlayableStatus = PlayableReady
		} else {
			c.PlayableStatus = PlayableBlocked
			var missing []string
			if c.Groups != nil {
				missing = c.Groups.Missing()
			} else {
				missing = []string{"legacyNineFieldVector"}
			}
			c.MissingFields = missing
			c.BlockReason = "missing classic fields: " + strings.Join(missing, ",")
		}
	}

	// Image paths resolve within the published content pack
	if av, ok := assets.avatarFor(w.ID); ok {
		route := PackAvatarRoute(av.CharacterID)
		c.ImagePath = &route
		c.AssetID = av.CharacterID + "-avatar"
		// The upstream URL lets the client prefer the CDN and keep the local file
		// as a fallback. Not every character has one, which is exactly why the
		// fallback must exist rather than being optional.
		if remote, ok := assets.remoteFor(w.ID); ok {
			c.RemoteImageURL = remote
		}
	}

	// Prototype horse: a separate entity, kept for pedigree and race modes.
	if p := prototypeFromWiki(w); p != nil {
		c.Prototype = p
	}
	for _, g := range w.Game.Goals {
		c.Goals = append(c.Goals, Goal{
			Index: g.Index, Name: g.Name, Time: g.Time,
			Condition: g.Condition, Race: g.Race,
		})
	}
	sort.Slice(c.Goals, func(i, j int) bool { return c.Goals[i].Index < c.Goals[j].Index })
	for _, s := range w.Trivia.Secrets {
		if strings.TrimSpace(s) == "" {
			continue
		}
		c.Secrets = append(c.Secrets, Secret{Text: s, Source: "wiki trivia"})
	}

	// Facts whose confidence is not confirmed must be flagged on the record so a
	// downstream mode can refuse them .
	if c.Names.JaConfidence == ConfidenceInferred {
		c.Confidence = ConfidenceInferred
		c.ReviewNotes = append(c.ReviewNotes,
			"Japanese name is inferred from the prototype horse name and has not been confirmed by a primary source")
	}
	if c.Groups == nil && c.PlayableStatus == PlayableBlocked {
		c.ReviewNotes = append(c.ReviewNotes,
			"no reviewed legacy nine-field vector; classic mode cannot use this record")
	}
	if c.ImagePath == nil {
		c.ReviewNotes = append(c.ReviewNotes,
			"no verified avatar file on disk; image-dependent modes must exclude this record")
	}

	if err := c.Validate(); err != nil {
		return nil, err
	}
	return c, nil
}

// legacyEntry is one canonical record's legacy identity: the romanised id the old
// question bank uses, the numeric order, and the bank/normalised rows it came
// from. The mapping is preserved in both directions .
type legacyEntry struct {
	romanised string
	order     int
	ready     *RawQuestionBankEntry
	blocked   *RawBlockedEntry
	norm      *RawNormalizedCharacter
}

func namesFromWiki(w *RawWikiCharacter) Names {
	n := Names{
		Ja:           w.Names.Ja,
		ZhHans:       w.Names.ZhHans,
		ZhHant:       w.Names.ZhHant,
		En:           w.Names.En,
		Nickname:     w.Names.NicknameJp,
		ZhHantSource: w.Names.ZhHantSource,
		JaSource:     w.Names.JaSource,
	}
	if w.Names.Romaji != nil {
		n.Romaji = *w.Names.Romaji
	}
	switch Confidence(w.Names.JaConfidence) {
	case ConfidenceConfirmed, ConfidenceInferred, ConfidenceUnknown:
		n.JaConfidence = Confidence(w.Names.JaConfidence)
	default:
		// No per-record confidence means the scrape did not flag it, i.e. it came
		// from a primary source field rather than a guess.
		if w.Names.Ja != "" {
			n.JaConfidence = ConfidenceConfirmed
		} else {
			n.JaConfidence = ConfidenceUnknown
		}
	}
	return n
}

func profileFromWiki(w *RawWikiCharacter, li *legacyEntry, overrides RawFieldOverrides) Profile {
	p := Profile{
		BirthdayMonth: w.Profile.Birthday.Month,
		BirthdayDay:   w.Profile.Birthday.Day,
		HeightCm:      w.Profile.HeightCm,
		CV:            w.Profile.CV,
		Introduction:  w.Game.Introduction,
	}
	if w.Game.Star != nil {
		p.Star = w.Game.Star
	} else if w.Profile.Star != nil {
		p.Star = w.Profile.Star
	}
	if li != nil && li.norm != nil {
		p.SchoolGrade = li.norm.Profile.SchoolGrade
		p.Dormitory = li.norm.Profile.Dormitory
		p.SexType = li.norm.Profile.SexType
	}
	// Manual overrides are keyed by legacy romanised id and only ever patch a
	// field a human explicitly corrected (currently: one schoolGrade).
	if li != nil && li.romanised != "" {
		if ov, ok := overrides[li.romanised]; ok {
			if v, ok := ov["grade"]; ok && v != "" {
				p.SchoolGrade = v
			}
			if v, ok := ov["dormitory"]; ok && v != "" {
				p.Dormitory = v
			}
			if v, ok := ov["sex_type"]; ok && v != "" {
				p.SexType = v
			}
		}
	}
	return p
}

func aliasesFromWiki(w *RawWikiCharacter) []Alias {
	seen := make(map[string]bool)
	var out []Alias
	add := func(text, locale string, conf Confidence) {
		text = strings.TrimSpace(text)
		if text == "" || seen[text] {
			return
		}
		seen[text] = true
		out = append(out, Alias{Text: text, Locale: locale, Confidence: conf})
	}
	jaConf := ConfidenceConfirmed
	if Confidence(w.Names.JaConfidence) == ConfidenceInferred {
		jaConf = ConfidenceInferred
	}
	add(w.Names.ZhHans, "zh-Hans", ConfidenceConfirmed)
	add(w.Names.ZhHant, "zh-Hant", ConfidenceConfirmed)
	add(w.Names.Ja, "ja", jaConf)
	add(w.Names.En, "en", ConfidenceConfirmed)
	add(w.Names.GameZhHans, "zh-Hans", ConfidenceConfirmed)
	add(w.Names.GameZhHant, "zh-Hant", ConfidenceConfirmed)
	add(w.Names.WikiTitle, "zh-Hans", ConfidenceConfirmed)
	add(w.Names.MoegirlZh, "zh-Hans", ConfidenceInferred)
	add(w.Names.MoegirlJa, "ja", ConfidenceInferred)
	for _, s := range w.Names.ZhAll {
		add(s, "zh-Hans", ConfidenceConfirmed)
	}
	if w.Names.NicknameJp != "" {
		add("【"+w.Names.NicknameJp+"】"+w.Names.ZhHans, "zh-Hans", ConfidenceConfirmed)
		add(w.Names.NicknameJp, "ja", ConfidenceConfirmed)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Locale < out[j].Locale })
	return out
}

func sourcesFromWiki(w *RawWikiCharacter) []SourceRef {
	var out []SourceRef
	seen := map[string]bool{}
	add := func(site, page, base string) {
		page = strings.TrimSpace(page)
		if page == "" || seen[site+"/"+page] {
			return
		}
		seen[site+"/"+page] = true
		out = append(out, SourceRef{Site: site, Page: page, URL: base + url.PathEscape(page)})
	}
	add("biliwiki", w.Names.WikiTitle, "https://wiki.biligame.com/umamusume/")
	for _, page := range w.Sources.Pages {
		add("biliwiki", page, "https://wiki.biligame.com/umamusume/")
	}
	add("biliwiki", w.Implementation.Source, "https://wiki.biligame.com/umamusume/")
	add("moegirl", w.Sources.Moegirl, "https://zh.moegirl.org.cn/")
	return out
}

func groupsFromReady(r RawQuestionBankEntry, overrides map[string]string) LegacyGroups {
	g := LegacyGroups{
		Star:              r.Star,
		SurfaceGroup:      append([]string(nil), r.SurfaceGroup...),
		DistanceGroup:     append([]string(nil), r.DistanceGroup...),
		RunningStyleGroup: append([]string(nil), r.RunningStyleGroup...),
		SexType:           r.SexType,
		G1Bracket:         r.G1Bracket,
		G23Bracket:        r.G23Bracket,
		G1HasJpn:          r.G1HasJpn,
		G23HasJpn:         r.G23HasJpn,
		SchoolGrade:       r.SchoolGrade,
		Dormitory:         r.Dormitory,
	}
	for key, dst := range map[string]*string{"grade": &g.SchoolGrade, "dormitory": &g.Dormitory, "sex_type": &g.SexType, "g1_bracket": &g.G1Bracket, "g23_bracket": &g.G23Bracket} {
		if v, ok := overrides[key]; ok {
			*dst = v
		}
	}
	if v, ok := overrides["g1_has_jpn"]; ok {
		g.G1HasJpn = v == "true"
	}
	if v, ok := overrides["g23_has_jpn"]; ok {
		g.G23HasJpn = v == "true"
	}
	return g
}

func prototypeFromWiki(w *RawWikiCharacter) *PrototypeHorse {
	raw := w.PrototypeHorse
	if raw.Names.Ja == "" && raw.Names.Zh == "" && raw.NKBID == "" && len(raw.RaceHistory) == 0 {
		return nil
	}
	p := &PrototypeHorse{
		NKBID:       raw.NKBID,
		Names:       Names{Ja: raw.Names.Ja, ZhHans: raw.Names.Zh, En: raw.Names.En, JaConfidence: ConfidenceConfirmed},
		Gender:      raw.Gender,
		BirthDate:   NormalizeDate(raw.Birthday),
		Owner:       raw.Owner,
		Trainer:     raw.Trainer,
		Farm:        raw.Farm,
		Nation:      raw.Nation,
		RaceResult:  raw.RaceResult,
		MainWinning: raw.MainWinning,
		Glory:       raw.Glory,
		Earnings:    raw.Earnings,
		RaceCount:   raw.RaceCount,
		Pedigree: Pedigree{
			Sire:     raw.Pedigree.Sire,
			Dam:      raw.Pedigree.Dam,
			SireSire: raw.Pedigree.SireSire,
			SireDam:  raw.Pedigree.SireDam,
			DamSire:  raw.Pedigree.DamSire,
			DamDam:   raw.Pedigree.DamDam,
		},
	}
	p.Sources = sourcesFromWiki(w)
	if raw.NKBID != "" {
		p.Sources = append(p.Sources, SourceRef{Site: "netkeiba", Page: raw.NKBID, URL: "https://db.netkeiba.com/horse/" + url.PathEscape(raw.NKBID) + "/"})
	}
	return p
}

// buildRaces aggregates the 2,127 prototype starts into races keyed by raceId.
// A raceId with only one recorded starter is still recorded but is not a usable
// race-rebuild question .
func buildRaces(chars []RawWikiCharacter) []Race {
	byID := make(map[string]*Race)
	for i := range chars {
		w := &chars[i]
		for _, row := range w.PrototypeHorse.RaceHistory {
			if row.ID == "" {
				continue
			}
			r := byID[row.ID]
			if r == nil {
				r = &Race{
					RaceID:   row.ID,
					Name:     row.Name,
					Date:     NormalizeDate(row.Date),
					DateRaw:  row.Date,
					Course:   row.Course,
					Surface:  row.Surface,
					Grade:    row.Grade,
					Distance: row.Distance,
					Region:   RegionForCourse(row.Course),
				}
				byID[row.ID] = r
			}
			r.Entries = append(r.Entries, RaceEntry{
				RaceID:        row.ID,
				RaceName:      row.Name,
				CharacterID:   w.ID,
				CharacterName: w.Names.ZhHans,
				Date:          NormalizeDate(row.Date),
				DateRaw:       row.Date,
				Course:        row.Course,
				Surface:       row.Surface,
				Grade:         row.Grade,
				Distance:      row.Distance,
				FinishRank:    row.FinishRank,
			})
		}
	}
	out := make([]Race, 0, len(byID))
	for _, r := range byID {
		sort.Slice(r.Entries, func(i, j int) bool {
			// Deterministic order: known ranks first by rank, unknown last by
			// character id. A null rank is never treated as last place .
			a, b := r.Entries[i], r.Entries[j]
			switch {
			case a.FinishRank != nil && b.FinishRank != nil:
				if *a.FinishRank != *b.FinishRank {
					return *a.FinishRank < *b.FinishRank
				}
				return a.CharacterID < b.CharacterID
			case a.FinishRank != nil:
				return true
			case b.FinishRank != nil:
				return false
			default:
				return a.CharacterID < b.CharacterID
			}
		})
		out = append(out, *r)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Date != out[j].Date {
			return out[i].Date < out[j].Date
		}
		return out[i].RaceID < out[j].RaceID
	})
	return out
}

func isEmptyGame(a RawAptitudeBlock) bool {
	return len(a.Surface) == 0 && len(a.Distance) == 0 && len(a.RunningStyle) == 0
}

// avatarSourceKind distinguishes verified files from separate asset sources.
type avatarSourceKind string

const (
	// AvatarSourceNone means no file was found for the character.
	AvatarSourceNone avatarSourceKind = ""
	// AvatarSourceCanonical identifies verified files from the tracked avatar directory.
	AvatarSourceCanonical avatarSourceKind = "canonical-avatars"
	// AvatarSourcePublic identifies thumbnails declared by the legacy asset manifest.
	AvatarSourcePublic avatarSourceKind = "public-manifest"
)

// verifiedAvatar is one character's avatar that was confirmed to exist on disk by
// a real os.Stat call, never by trusting a manifest flag.
type verifiedAvatar struct {
	// CharacterID is the canonical character id.
	CharacterID string
	// SourcePath is the repo-relative path the file was read from.
	SourcePath string
	// Source identifies which of the two avatar sets supplied the file.
	Source avatarSourceKind
	// Bytes and SHA256 are the file's real digest, used to detect duplicates.
	Bytes  int64
	SHA256 string
}

type assetVerification struct {
	// byCharacter maps a canonical character id to its verified avatar.
	byCharacter map[string]verifiedAvatar
	// remote maps a canonical character id to its upstream CDN URL, derived from
	// the legacy manifest's source_url (which embeds the numeric id).
	remote map[string]string

	// Legacy manifest set (data/assets/asset-manifest.json).
	manifestEntries       int
	manifestMissingOnDisk int
	onlyInManifest        int

	// Legacy cache index set (data/assets/cache-index.json).
	cacheIndexEntries       int
	cacheIndexMissingOnDisk int
	onlyInCacheIndex        int

	// Canonical avatars keyed by character ID
	stagingDeclared int
	stagingVerified int
	stagingOrphans  []string
	stagingMissing  []string

	// verifiedOnDisk is the total number of characters with a usable avatar,
	// counted after both sources were tried.
	verifiedOnDisk int

	// pairCharacter maps an opaque memory pair id to the character whose avatar it
	// shows. It exists so a published imagePath can be resolved back to a verified
	// file at build time instead of being assumed correct.
	pairCharacter map[string]string
}

// remoteFor returns the upstream CDN URL for a character's icon, if the snapshot
// carries one. Roughly 80% of characters have it; the rest rely on the local file.
func (a assetVerification) remoteFor(characterID string) (string, bool) {
	if a.remote == nil {
		return "", false
	}
	u, ok := a.remote[characterID]
	return u, ok
}

// avatarFor returns the verified avatar for a character, if any.
func (a assetVerification) avatarFor(characterID string) (verifiedAvatar, bool) {
	if a.byCharacter == nil {
		return verifiedAvatar{}, false
	}
	v, ok := a.byCharacter[characterID]
	return v, ok
}

// verifyAssets checks both avatar sources against the real filesystem. Every
// candidate is confirmed with os.Stat; a manifest's "cached" flag is never
// treated as evidence that a file exists.
func verifyAssets(in *RawInputs, root, datasetVersion string) assetVerification {
	v := assetVerification{
		byCharacter:       make(map[string]verifiedAvatar),
		pairCharacter:     make(map[string]string),
		remote:            make(map[string]string),
		manifestEntries:   len(in.Assets),
		cacheIndexEntries: len(in.CacheIndex),
	}

	// Derive character id -> upstream URL from the manifest. The manifest is keyed
	// by the legacy romanised id but its source_url embeds the numeric id, so the
	// join is exact without a romanised-to-numeric table.
	manifestSources := make([]assetutil.ManifestSource, 0, len(in.Assets))
	for _, a := range in.Assets {
		manifestSources = append(manifestSources, assetutil.ManifestSource{
			AssetID:     a.AssetID,
			CharacterID: a.CharacterID,
			SourceURL:   a.SourceURL,
			LocalPath:   a.LocalPath,
		})
	}
	for id, u := range assetutil.BuildRemoteIndex(manifestSources) {
		v.remote[id] = u
	}

	// Canonical numeric IDs provide an exact join to tracked avatars
	avatarSource := avatarDir
	stagingDir := filepath.Join(root, avatarDir)
	entries, err := os.ReadDir(stagingDir)
	if err == nil {
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".png") {
				continue
			}
			v.stagingDeclared++
			id := strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))
			full := filepath.Join(stagingDir, e.Name())
			info, statErr := os.Stat(full)
			if statErr != nil || info.IsDir() || info.Size() == 0 {
				v.stagingMissing = append(v.stagingMissing, id)
				continue
			}
			digest, derr := fileSHA256(full)
			if derr != nil {
				v.stagingMissing = append(v.stagingMissing, id)
				continue
			}
			v.stagingVerified++
			v.byCharacter[id] = verifiedAvatar{
				CharacterID: id,
				SourcePath:  filepath.ToSlash(filepath.Join(avatarSource, e.Name())),
				Source:      AvatarSourceCanonical,
				Bytes:       info.Size(),
				SHA256:      digest,
			}
		}
	}

	// Verify legacy manifest paths separately; they never override canonical avatars.
	manifestIDs := make(map[string]bool, len(in.Assets))
	for _, a := range in.Assets {
		manifestIDs[a.AssetID] = true
		if !fileExists(filepath.Join(root, a.LocalPath)) {
			v.manifestMissingOnDisk++
		}
	}
	for id, p := range in.CacheIndex {
		if !fileExists(filepath.Join(root, p)) {
			v.cacheIndexMissingOnDisk++
		}
		if !manifestIDs[id] {
			v.onlyInCacheIndex++
		}
	}
	for id := range manifestIDs {
		if _, ok := in.CacheIndex[id]; !ok {
			v.onlyInManifest++
		}
	}

	// Any staging file whose id matches no character is an orphan and is reported
	// rather than silently copied into the pack.
	known := make(map[string]bool, len(in.Wiki.Characters))
	for i := range in.Wiki.Characters {
		known[in.Wiki.Characters[i].ID] = true
	}
	for id := range v.byCharacter {
		if !known[id] {
			v.stagingOrphans = append(v.stagingOrphans, id)
			delete(v.byCharacter, id)
		}
	}
	v.verifiedOnDisk = len(v.byCharacter)
	// The pair id is a digest of the dataset version and the character id, so the
	// index can be built here and used to resolve every published imagePath.
	for id := range v.byCharacter {
		v.pairCharacter[opaquePairID(id, datasetVersion)] = id
	}
	sort.Strings(v.stagingOrphans)
	sort.Strings(v.stagingMissing)
	return v
}

// fileSHA256 returns the hex digest of a file's bytes.
func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// fileExists reports whether a repo-relative path is a regular file. The path is
// resolved from the process working directory, which the CLI pins to the repo
// root so the check is reproducible.
func fileExists(rel string) bool {
	if rel == "" {
		return false
	}
	st, err := os.Stat(rel)
	if err != nil {
		return false
	}
	return !st.IsDir()
}

func shortHash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:4])
}

// marshalDeterministic renders v with sorted map keys and no HTML escaping so
// two runs over the same input produce byte-identical output.
func marshalDeterministic(v any) ([]byte, error) {
	var sb strings.Builder
	enc := json.NewEncoder(&sb)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return []byte(sb.String()), nil
}
