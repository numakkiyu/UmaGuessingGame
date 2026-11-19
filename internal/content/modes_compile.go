package content

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/bhcn/umaguessinggame/internal/modes/aptitudegrid"
	"golang.org/x/text/unicode/norm"
)

// Section names. The seven primary names are the contract mode packages read
// through modes.ContentSource.Section . Aliases are additionally
// published so a consumer written against an earlier name still resolves, but
// the primary name is the one guaranteed to hit.
const (
	SectionClassicCharacters = "classic.characters"
	SectionAptitudeGridCards = "aptitude-grid.cards"
	SectionDraftMarket       = "draft.market"
	SectionRaceRebuildRaces  = "race-rebuild.races"
	SectionCoRacePathGraph   = "co-race-path.graph"
	SectionSortCards         = "sort.cards"
	SectionMemoryPairs       = "memory.pairs"
	SectionBluffFacts        = "bluff.facts"
	SectionAudioClips        = "audio.clips"
	SectionTradeCards        = "trade.cards"
	SectionTradeObjectives   = "trade.objectives"
	SectionRulesTrack        = "rules.track"
	SectionCreativePrompts   = "creative.prompts"
	SectionCluePuzzleDeck    = "cluepuzzle.deck"
	SectionRapportQuestions  = "rapport.questions"
	// PublicIndexSection is the servable name index: identity and search text
	// only, never a comparison vector or a secret .
	PublicIndexSection = "characters.index"
)

// sectionAliases maps an accepted alias to its primary section name.
var sectionAliases = map[string]string{
	"rules":              SectionRulesTrack,
	"rules.track":        SectionRulesTrack,
	"classic":            SectionClassicCharacters,
	"characters":         SectionClassicCharacters,
	"aptitudeGrid.cards": SectionAptitudeGridCards,
	"aptitude-grid":      SectionAptitudeGridCards,
	"draft":              SectionDraftMarket,
	"draft.cards":        SectionDraftMarket,
	"raceRebuild.races":  SectionRaceRebuildRaces,
	"races":              SectionRaceRebuildRaces,
	"coRacePath.graph":   SectionCoRacePathGraph,
	"graph":              SectionCoRacePathGraph,
	"sort":               SectionSortCards,
	"memory":             SectionMemoryPairs,
}

// ---------------------------------------------------------------------------
// classic (G01): the nine-field identity game
// ---------------------------------------------------------------------------

// classicMinPool is the smallest answer pool the classic mode accepts. A pool
// smaller than this makes the mode degenerate, so it is refused instead of being
// opened with too few answers .
const classicMinPool = 8

func compileClassic(ctx *compileContext) (modeResult, error) {
	tally := RejectTally{}
	var cards []classicCard
	// Ambiguity groups are computed from the published vectors so the exclusion
	// is self-consistent with what a client actually receives.
	bySig := make(map[string][]int)

	for _, c := range ctx.ds.Characters {
		switch {
		case c.EntityKind != EntityPlayable:
			tally.add(RejectNotPlayable)
			continue
		case c.Groups == nil:
			tally.add(RejectMissingNineFields)
			continue
		}
		if missing := c.Groups.Missing(); len(missing) > 0 {
			tally.add(RejectMissingNineFields)
			continue
		}
		if c.Groups.Star == nil {
			tally.add(RejectUnknownStar)
			continue
		}
		// The classic board renders a card face per guess. A character without a
		// verified avatar would show an empty image, so it is excluded rather than
		// published with a null face .
		if c.ImagePath == nil {
			tally.add(RejectNoAsset)
			continue
		}
		card := classicCard{
			ID:                c.ID,
			NameZh:            c.Names.ZhHans,
			NameJp:            c.Names.Ja,
			NameJpConfidence:  string(c.Names.JaConfidence),
			Star:              c.Groups.Star,
			SurfaceGroup:      c.Groups.SurfaceGroup,
			DistanceGroup:     c.Groups.DistanceGroup,
			RunningStyleGroup: c.Groups.RunningStyleGroup,
			SexType:           c.Groups.SexType,
			G1Bracket:         c.Groups.G1Bracket,
			G23Bracket:        c.Groups.G23Bracket,
			G1HasJpn:          c.Groups.G1HasJpn,
			G23HasJpn:         c.Groups.G23HasJpn,
			SchoolGrade:       c.Groups.SchoolGrade,
			Dormitory:         c.Groups.Dormitory,
			ImagePath:         c.ImagePath,
			RemoteImageURL:    c.RemoteImageURL,
		}
		if len(c.LegacyIDs) > 0 {
			card.LegacyID = c.LegacyIDs[0]
		}
		bySig[c.Groups.Signature()] = append(bySig[c.Groups.Signature()], len(cards))
		cards = append(cards, card)
	}

	// Collapse duplicate vectors: competitive packs exclude every member of a
	// colliding group, and the exclusion is reported .
	ambiguous := 0
	for sig, idxs := range bySig {
		if len(idxs) < 2 {
			continue
		}
		label := "amb-" + shortHash(sig)
		for _, i := range idxs {
			cards[i].AmbiguityGroup = label
			ambiguous++
		}
	}
	if ambiguous > 0 {
		tally[RejectAmbiguousVector] += ambiguous
	}

	var competitive []classicCard
	var practice []classicCard
	for _, c := range cards {
		if c.AmbiguityGroup == "" {
			competitive = append(competitive, c)
		}
		practice = append(practice, c)
	}
	sort.Slice(competitive, func(i, j int) bool { return competitive[i].ID < competitive[j].ID })
	sort.Slice(practice, func(i, j int) bool { return practice[i].ID < practice[j].ID })

	notes := []string{
		fmt.Sprintf("%d answer candidates carry a reviewed legacy nine-field vector", len(cards)),
		fmt.Sprintf("%d excluded from competitive play because their nine-field vector is identical to another character; they remain available in practice packs flagged with ambiguityGroup", ambiguous),
		"the legacy *_group buckets are kept verbatim; the raw A-G aptitude grades were NOT substituted for them",
	}
	if len(practice) < classicMinPool || len(competitive) < 2 {
		return unavailable(ModeClassic, SectionClassicCharacters, classicMinPool, tally, notes...), nil
	}

	// The answer pool is built here but published through the private tree: the
	// eligible answer set and the excluded ambiguity groups are the solution, and a
	// client must only learn the one answer at reveal time .
	pool := privateClassicPool{
		Mode:            ModeClassic,
		SeedNamespace:   seedNamespace(ModeClassic, ctx.ds.DatasetVersion),
		AmbiguityGroups: ambiguityGroups(bySig, cards),
	}
	for i, card := range cards {
		if card.AmbiguityGroup == "" {
			pool.AnswerIDs = append(pool.AnswerIDs, cards[i].ID)
		}
	}
	sort.Strings(pool.AnswerIDs)
	if len(pool.AmbiguityGroups) == 0 {
		pool.AmbiguityGroups = nil
	}

	return available(ModeClassic, len(competitive), classicMinPool, tally, notes, map[string]any{
		SectionClassicCharacters: classicSection{
			Mode:              ModeClassic,
			Cards:             competitive,
			PracticeCards:     practice,
			CompetitiveCount:  len(competitive),
			PracticeCount:     len(practice),
			AmbiguousExcluded: ambiguous,
		},
	}, map[string]any{PrivateClassicAnswerPool: pool}), nil
}

// ambiguityGroups maps each collision label to its member character ids. The
// partition is derived from the same signatures the public section used, so the
// two trees cannot disagree.
func ambiguityGroups(bySig map[string][]int, cards []classicCard) map[string][]string {
	out := make(map[string][]string)
	for sig, idxs := range bySig {
		if len(idxs) < 2 {
			continue
		}
		label := "amb-" + shortHash(sig)
		ids := make([]string, 0, len(idxs))
		for _, i := range idxs {
			ids = append(ids, cards[i].ID)
		}
		sort.Strings(ids)
		out[label] = ids
	}
	return out
}

// classicSection is the payload of "classic.characters". Cards is the
// competitive pool; PracticeCards additionally contains the ambiguous vectors.
type classicSection struct {
	Mode              string        `json:"mode"`
	Cards             []classicCard `json:"cards"`
	PracticeCards     []classicCard `json:"practiceCards"`
	CompetitiveCount  int           `json:"competitiveCount"`
	PracticeCount     int           `json:"practiceCount"`
	AmbiguousExcluded int           `json:"ambiguousExcluded"`
}

// ---------------------------------------------------------------------------
// aptitude-grid (G02): 3x3 distance-A x style-A cross grid
// ---------------------------------------------------------------------------

// aptitudeGridMinPool is the smallest card pool the grid accepts. The grid needs
// nine distinct characters, so a pool under nine can never be solved.
const aptitudeGridMinPool = 9

func compileAptitudeGrid(ctx *compileContext) (modeResult, error) {
	tally := RejectTally{}
	var cards []aptitudeCard
	for _, c := range ctx.playableCharacters() {
		card, ok := ctx.aptitudeCard(c)
		if !ok {
			if len(c.Aptitudes.Surface)+len(c.Aptitudes.Distance)+len(c.Aptitudes.RunningStyle) == 0 {
				tally.add(RejectNoAptitudes)
			} else {
				tally.add(RejectIncompleteAptitude)
			}
			continue
		}
		// The grid shows a card face in every occupied cell, so an unverified
		// avatar would leave a blank cell .
		if c.ImagePath == nil {
			tally.add(RejectNoAsset)
			continue
		}
		cards = append(cards, card)
	}
	sort.Slice(cards, func(i, j int) bool { return cards[i].CharacterID < cards[j].CharacterID })

	// Solvability: a 3x3 grid over distance x style needs, for every cell, at
	// least one card graded A in both. Count the cells that are actually
	// coverable so the report states the real scarcity .
	type cell struct{ distance, style string }
	distances := []string{"短", "英", "中", "长"}
	styles := []string{"逃", "先", "差", "追"}
	cellCount := make(map[cell]int)
	for _, c := range cards {
		for _, d := range distances {
			if c.Aptitudes.Distance[d] != "A" {
				continue
			}
			for _, s := range styles {
				if c.Aptitudes.Style[s] != "A" {
					continue
				}
				cellCount[cell{d, s}]++
			}
		}
	}
	coverable := 0
	for _, d := range distances {
		for _, s := range styles {
			if cellCount[cell{d, s}] > 0 {
				coverable++
			}
		}
	}

	notes := []string{
		fmt.Sprintf("%d characters carry all ten A-G aptitude grades", len(cards)),
		fmt.Sprintf("%d of 16 distance x style cells have at least one A/A card", coverable),
		"aptitudes are per character record, not per outfit: the snapshot does not give each of the 272 outfits its own aptitude set",
	}
	boardCards := make([]aptitudegrid.Card, 0, len(cards))
	for _, c := range cards {
		boardCards = append(boardCards, aptitudegrid.Card{ID: c.CharacterID, Aptitudes: aptitudegrid.Aptitudes{
			Surface: c.Aptitudes.Surface, Distance: c.Aptitudes.Distance, Style: c.Aptitudes.Style,
		}})
	}
	canFill := aptitudegrid.HasSolution(boardCards)
	notes = append(notes, fmt.Sprintf("standard 3x3 board has a matching of nine distinct characters: %t", canFill))
	if len(cards) < aptitudeGridMinPool || coverable < 9 || !canFill {
		if coverable < 9 {
			tally[RejectIncompleteAptitude] += 9 - coverable
		}
		return unavailable(ModeAptitudeGrid, SectionAptitudeGridCards, aptitudeGridMinPool, tally, notes...), nil
	}

	return available(ModeAptitudeGrid, len(cards), aptitudeGridMinPool, tally, notes, map[string]any{
		SectionAptitudeGridCards: aptitudeGridSection{
			Mode:           ModeAptitudeGrid,
			Cards:          cards,
			CoverableCells: coverable,
			TotalCells:     16,
			GridDimensions: []string{"distance", "style"},
		},
	}), nil
}

// aptitudeGridSection is the payload of "aptitude-grid.cards".
type aptitudeGridSection struct {
	Mode           string         `json:"mode"`
	Cards          []aptitudeCard `json:"cards"`
	CoverableCells int            `json:"coverableCells"`
	TotalCells     int            `json:"totalCells"`
	GridDimensions []string       `json:"gridDimensions"`
}

// ---------------------------------------------------------------------------
// draft (G03): public card market with prices and tasks
// ---------------------------------------------------------------------------

const (
	draftMinCards   = 12
	draftMinTasks   = 4
	draftBudget     = 24
	draftRosterSize = 4
)

// draftPrice implements the trial price formula from :
//
//	3 + ceil(distanceA/2) + ceil(styleA/2) + (dirt A ? 1 : 0)
//
// The formula is a non-official trial balance parameter, not game data.
func draftPrice(a Aptitudes) int {
	return 3 + ceilDiv(a.CountA("distance"), 2) + ceilDiv(a.CountA("runningStyle"), 2) + boolToInt(a.Surface["dirt"] == "A")
}

func ceilDiv(n, d int) int { return int(math.Ceil(float64(n) / float64(d))) }

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func compileDraft(ctx *compileContext) (modeResult, error) {
	tally := RejectTally{}
	var cards []draftCard
	for _, c := range ctx.playableCharacters() {
		view, ok := toAptitudeView(c.Aptitudes)
		if !ok {
			tally.add(RejectIncompleteAptitude)
			continue
		}
		price := draftPrice(c.Aptitudes)
		if price <= 0 {
			tally.add(RejectNoPrice)
			continue
		}
		// Draft cards are shown face-up in the public market, so an unverified
		// avatar would be a blank card .
		if c.ImagePath == nil {
			tally.add(RejectNoAsset)
			continue
		}
		cards = append(cards, draftCard{
			CharacterID:    c.ID,
			Name:           c.Names.ZhHans,
			ImagePath:      c.ImagePath,
			RemoteImageURL: c.RemoteImageURL,
			Price:          price,
			Aptitudes:      view,
		})
	}
	sort.Slice(cards, func(i, j int) bool {
		if cards[i].Price != cards[j].Price {
			return cards[i].Price < cards[j].Price
		}
		return cards[i].CharacterID < cards[j].CharacterID
	})

	// Tasks: every distance x style pair that at least draftRosterSize cards can
	// satisfy. A task nobody can fill is not a task.
	tasks := buildDraftTasks(cards)

	// Feasibility: at least two distinct complete rosters must fit the budget.
	// Feasibility is checked per task; a market is only viable when its best task
	// admits two rosters .
	feasibleTasks, maxRosters := evaluateDraftFeasibility(cards, tasks)

	notes := []string{
		fmt.Sprintf("%d priced cards from the trial formula (3 + ceil(distanceA/2) + ceil(styleA/2) + dirtA)", len(cards)),
		fmt.Sprintf("price range %d..%d; the formula is a non-official trial balance parameter, not game data", minPrice(cards), maxPrice(cards)),
		fmt.Sprintf("%d of %d candidate tasks admit at least %d complete rosters", feasibleTasks, len(tasks), 2),
		fmt.Sprintf("budget %d, roster %d; best task admits %d distinct rosters", draftBudget, draftRosterSize, maxRosters),
		"outfit prices do not exist in the snapshot: prices are derived, never sourced",
	}
	if len(cards) < draftMinCards || feasibleTasks < draftMinTasks {
		if len(cards) < draftMinCards {
			tally[RejectNoPrice] += draftMinCards - len(cards)
		}
		return unavailable(ModeDraft, SectionDraftMarket, draftMinTasks, tally, notes...), nil
	}

	return available(ModeDraft, feasibleTasks, draftMinTasks, tally, notes, map[string]any{
		SectionDraftMarket: draftSection{
			Mode:          ModeDraft,
			Budget:        draftBudget,
			Roster:        draftRosterSize,
			Cards:         cards,
			Tasks:         tasks,
			FeasibleTasks: feasibleTasks,
		},
	}), nil
}

// draftCard is one priced card in the public market.
type draftCard struct {
	CharacterID string  `json:"characterId"`
	Name        string  `json:"name"`
	ImagePath   *string `json:"imagePath"`
	// RemoteImageURL is the upstream CDN URL; the client prefers it and falls back
	// to ImagePath when the CDN is unreachable (LAN/offline events).
	RemoteImageURL string       `json:"remoteImageUrl,omitempty"`
	Price          int          `json:"price"`
	Aptitudes      aptitudeView `json:"aptitudes"`
}

// draftTask is one assignment target: surface, distance and running style.
type draftTask struct {
	TaskID   string `json:"taskId"`
	Surface  string `json:"surface"`
	Distance string `json:"distance"`
	Style    string `json:"style"`
}

// draftSection is the payload of "draft.market".
type draftSection struct {
	Mode          string      `json:"mode"`
	Budget        int         `json:"budget"`
	Roster        int         `json:"roster"`
	Cards         []draftCard `json:"cards"`
	Tasks         []draftTask `json:"tasks"`
	FeasibleTasks int         `json:"feasibleTasks"`
}

func buildDraftTasks(cards []draftCard) []draftTask {
	distances := []string{"短", "英", "中", "长"}
	styles := []string{"逃", "先", "差", "追"}
	surfaces := []string{"草地", "泥地"}
	var tasks []draftTask
	for _, surface := range surfaces {
		for _, d := range distances {
			for _, s := range styles {
				n := 0
				for _, c := range cards {
					if c.Aptitudes.Surface[surface] == "" {
						continue
					}
					// A card may be assigned when its grade is not the worst two:
					// the task still needs four distinct characters that can fill
					// it, so count cards with a graded (non-empty) value.
					if c.Aptitudes.Distance[d] != "" && c.Aptitudes.Style[s] != "" {
						n++
					}
				}
				if n >= draftRosterSize {
					tasks = append(tasks, draftTask{
						TaskID:   fmt.Sprintf("%s-%s-%s", surface, d, s),
						Surface:  surface,
						Distance: d,
						Style:    s,
					})
				}
			}
		}
	}
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].TaskID < tasks[j].TaskID })
	return tasks
}

// evaluateDraftFeasibility counts, for each task, how many distinct complete
// rosters fit the budget, and returns how many tasks admit at least two.
//
// The count is an exact combination enumeration over cards that can fill the
// task, capped so a large pool cannot blow up the build.
func evaluateDraftFeasibility(cards []draftCard, tasks []draftTask) (feasibleTasks, maxRosters int) {
	const capRosters = 100000
	for _, task := range tasks {
		var eligible []draftCard
		for _, c := range cards {
			if c.Aptitudes.Surface[task.Surface] == "" {
				continue
			}
			if c.Aptitudes.Distance[task.Distance] == "" || c.Aptitudes.Style[task.Style] == "" {
				continue
			}
			eligible = append(eligible, c)
		}
		n := countRosters(eligible, draftRosterSize, draftBudget, capRosters)
		if n >= 2 {
			feasibleTasks++
		}
		if n > maxRosters {
			maxRosters = n
		}
	}
	return feasibleTasks, maxRosters
}

// countRosters counts size-combinations whose summed price fits the budget,
// stopping at cap.
func countRosters(cards []draftCard, size, budget, cap int) int {
	if len(cards) < size {
		return 0
	}
	count := 0
	var walk func(start, chosen, spent int)
	walk = func(start, chosen, spent int) {
		if count >= cap {
			return
		}
		if chosen == size {
			count++
			return
		}
		for i := start; i < len(cards); i++ {
			next := spent + cards[i].Price
			if next > budget {
				continue
			}
			if len(cards)-i < size-chosen {
				break
			}
			walk(i+1, chosen+1, next)
		}
	}
	walk(0, 0, 0)
	return count
}

func minPrice(cards []draftCard) int {
	if len(cards) == 0 {
		return 0
	}
	m := cards[0].Price
	for _, c := range cards {
		if c.Price < m {
			m = c.Price
		}
	}
	return m
}

func maxPrice(cards []draftCard) int {
	m := 0
	for _, c := range cards {
		if c.Price > m {
			m = c.Price
		}
	}
	return m
}

// ---------------------------------------------------------------------------
// race-rebuild (G04): order the recorded starters of one race
// ---------------------------------------------------------------------------

const (
	raceRebuildMinStarters = 4
	raceRebuildMaxStarters = 7
	raceRebuildMinRaces    = 8
)

func compileRaceRebuild(ctx *compileContext) (modeResult, error) {
	tally := RejectTally{}
	var races []raceRebuildRace
	// answers is the server-only half: the rank order per published raceId. It is
	// returned through the private sections, never through a public one.
	answers := privateRaceAnswers{Mode: ModeRaceRebuild}

	for _, r := range ctx.ds.Races {
		if r.Date == "" {
			tally.add(RejectRaceNoDate)
			continue
		}
		var ranked []rankedStarter
		for _, e := range r.Entries {
			switch {
			case e.CharacterID == "":
				tally.add(RejectRaceNoIdentity)
				continue
			case e.FinishRank == nil:
				// A null rank is not last place: the starter is dropped, and the
				// rest are NOT renumbered .
				tally.add(RejectRaceNoRank)
				continue
			}
			ranked = append(ranked, rankedStarter{
				CharacterID:   e.CharacterID,
				CharacterName: e.CharacterName,
				FinishRank:    *e.FinishRank,
				TieGroup:      tieGroupFor(r, *e.FinishRank),
			})
		}
		if len(ranked) < raceRebuildMinStarters {
			if len(ranked) > 0 {
				tally.add(RejectRaceTooSmall)
			}
			continue
		}
		if len(ranked) > raceRebuildMaxStarters {
			tally.add(RejectRaceTooLarge)
			continue
		}
		sort.Slice(ranked, func(i, j int) bool {
			if ranked[i].FinishRank != ranked[j].FinishRank {
				return ranked[i].FinishRank < ranked[j].FinishRank
			}
			return ranked[i].CharacterID < ranked[j].CharacterID
		})

		// The public projection keeps identity only; the starters are sorted by
		// character id so the published array order carries no hint of the result.
		starters := make([]raceStarter, 0, len(ranked))
		for _, rs := range ranked {
			starters = append(starters, raceStarter{
				CharacterID:   rs.CharacterID,
				CharacterName: rs.CharacterName,
			})
		}
		sort.Slice(starters, func(i, j int) bool { return starters[i].CharacterID < starters[j].CharacterID })

		races = append(races, raceRebuildRace{
			RaceID:       r.RaceID,
			Name:         r.Name,
			Date:         r.Date,
			Course:       r.Course,
			Surface:      r.Surface,
			Grade:        r.Grade,
			Distance:     r.Distance,
			Region:       r.Region,
			Starters:     starters,
			StarterCount: len(starters),
		})
		ra := privateRaceAnswer{RaceID: r.RaceID, Ranks: make(map[string]int)}
		rankGroups := make(map[int][]string)
		for _, rs := range ranked {
			ra.Order = append(ra.Order, rs.CharacterID)
			ra.Ranks[rs.CharacterID] = rs.FinishRank
			rankGroups[rs.FinishRank] = append(rankGroups[rs.FinishRank], rs.CharacterID)
		}
		// Starters dropped for a missing rank are recorded so a referee can see
		// that no renumbering happened .
		for _, e := range r.Entries {
			if e.FinishRank == nil && e.CharacterID != "" {
				ra.Dropped = append(ra.Dropped, e.CharacterID+":no_recorded_rank")
			}
		}
		ties := make(map[string][]string)
		for rank, ids := range rankGroups {
			if len(ids) > 1 {
				sort.Strings(ids)
				ties[fmt.Sprintf("%d", rank)] = ids
			}
		}
		if len(ties) > 0 {
			ra.Ties = ties
		}
		answers.Races = append(answers.Races, ra)
	}
	sort.Slice(races, func(i, j int) bool {
		if races[i].Date != races[j].Date {
			return races[i].Date < races[j].Date
		}
		return races[i].RaceID < races[j].RaceID
	})

	notes := []string{
		fmt.Sprintf("%d raceIds hold %d recorded starts", len(ctx.ds.Races), countEntries(ctx.ds.Races)),
		fmt.Sprintf("%d races have %d-%d starters with a known rank and are usable", len(races), raceRebuildMinStarters, raceRebuildMaxStarters),
		"the public section carries the unordered starter list only; the finish ranks are the answer and live in the private race-rebuild.answers section",
		"starters without a recorded rank are dropped and the remainder are NOT renumbered, so rank gaps (1,2,3,5,7) are preserved",
		"the recorded starters are the characters whose prototype appears in the snapshot; this is not a full field list",
	}
	if len(races) < raceRebuildMinRaces {
		return unavailable(ModeRaceRebuild, SectionRaceRebuildRaces, raceRebuildMinRaces, tally, notes...), nil
	}

	sort.Slice(answers.Races, func(i, j int) bool { return answers.Races[i].RaceID < answers.Races[j].RaceID })
	return available(ModeRaceRebuild, len(races), raceRebuildMinRaces, tally, notes, map[string]any{
		SectionRaceRebuildRaces: races,
	}, map[string]any{PrivateRaceRebuildAnswers: answers}), nil
}

// raceRebuildRace is one usable race question as published to a client.
//
// The finish ranks are NOT here: they are the answer. The public section carries
// the race identity and the unordered starter list, so a client can build the
// ordering task without ever receiving the solution. Ranks live in the private
// race-rebuild.answers section .
type raceRebuildRace struct {
	RaceID   string `json:"raceId"`
	Name     string `json:"name"`
	Date     string `json:"date"`
	Course   string `json:"course"`
	Surface  string `json:"surface,omitempty"`
	Grade    string `json:"grade,omitempty"`
	Distance string `json:"distance,omitempty"`
	Region   string `json:"region,omitempty"`
	// Starters is the unordered set of recorded starters. The order in this array
	// carries no meaning and must not be read as the result.
	Starters []raceStarter `json:"starters"`
	// StarterCount is len(Starters), repeated so a client can size the board
	// without walking the array.
	StarterCount int `json:"starterCount"`
}

// raceStarter is one competitor in a race-rebuild question, without its rank.
type raceStarter struct {
	CharacterID   string `json:"characterId"`
	CharacterName string `json:"characterName,omitempty"`
}

// tieGroupFor returns a stable tie label when the rank is shared, else "".
func tieGroupFor(r Race, rank int) string {
	n := 0
	for _, e := range r.Entries {
		if e.FinishRank != nil && *e.FinishRank == rank {
			n++
		}
	}
	if n < 2 {
		return ""
	}
	return fmt.Sprintf("%s-tie-%d", r.RaceID, rank)
}

// rankedStarter is the internal shape used while compiling, before the rank is
// stripped from the public projection and kept for the private answer.
type rankedStarter struct {
	CharacterID   string
	CharacterName string
	FinishRank    int
	TieGroup      string
}

func countEntries(races []Race) int {
	n := 0
	for _, r := range races {
		n += len(r.Entries)
	}
	return n
}

// ---------------------------------------------------------------------------
// co-race-path (G05): shortest path over shared-race evidence
// ---------------------------------------------------------------------------

const (
	coRacePathMinNodes     = 8
	coRacePathMinEdges     = 12
	coRacePathMinComponent = 3
	coRacePathMaxEdges     = 6
)

func compileCoRacePath(ctx *compileContext) (modeResult, error) {
	tally := RejectTally{}

	// An edge exists when two characters' prototypes started in the same raceId.
	type edgeKey struct{ a, b string }
	edges := make(map[edgeKey]*coRaceEdge)
	nodeSeen := make(map[string]string) // character id -> display name

	for _, r := range ctx.ds.Races {
		var ids []string
		for _, e := range r.Entries {
			if e.CharacterID == "" {
				continue
			}
			if !containsString(ids, e.CharacterID) {
				ids = append(ids, e.CharacterID)
			}
			if _, ok := nodeSeen[e.CharacterID]; !ok {
				nodeSeen[e.CharacterID] = e.CharacterName
			}
		}
		sort.Strings(ids)
		for i := 0; i < len(ids); i++ {
			for j := i + 1; j < len(ids); j++ {
				k := edgeKey{ids[i], ids[j]}
				if ex, ok := edges[k]; ok {
					// Same pair, another race: keep the earliest evidence and count
					// the rest as additional shared races .
					if r.Date != "" && (ex.Date == "" || r.Date < ex.Date) {
						ex.RaceID = r.RaceID
						ex.RaceName = r.Name
						ex.Date = r.Date
					}
					ex.SharedRaces++
					continue
				}
				edges[k] = &coRaceEdge{
					From:        ids[i],
					To:          ids[j],
					RaceID:      r.RaceID,
					RaceName:    r.Name,
					Date:        r.Date,
					SharedRaces: 1,
				}
			}
		}
	}
	if len(nodeSeen) == 0 || len(edges) == 0 {
		tally.add(RejectNoRaceEvidence)
		return unavailable(ModeCoRacePath, SectionCoRacePathGraph, coRacePathMinEdges, tally,
			"no prototype race history in the snapshot, so no same-race edges exist"), nil
	}

	// Connected components decide whether a start/end pair is even selectable.
	parent := make(map[string]string, len(nodeSeen))
	for id := range nodeSeen {
		parent[id] = id
	}
	var find func(string) string
	find = func(x string) string {
		for parent[x] != x {
			parent[x] = parent[parent[x]]
			x = parent[x]
		}
		return x
	}
	union := func(a, b string) {
		ra, rb := find(a), find(b)
		if ra != rb {
			parent[ra] = rb
		}
	}
	for k := range edges {
		union(k.a, k.b)
	}
	comp := make(map[string][]string)
	for id := range nodeSeen {
		root := find(id)
		comp[root] = append(comp[root], id)
	}
	var components [][]string
	for _, members := range comp {
		sort.Strings(members)
		components = append(components, members)
	}
	sort.Slice(components, func(i, j int) bool {
		if len(components[i]) != len(components[j]) {
			return len(components[i]) > len(components[j])
		}
		return components[i][0] < components[j][0]
	})
	usable := 0
	for _, c := range components {
		if len(c) >= coRacePathMinComponent {
			usable++
		}
	}

	nodes := make([]coRaceNode, 0, len(nodeSeen))
	for id, name := range nodeSeen {
		nodes = append(nodes, coRaceNode{CharacterID: id, Name: name})
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].CharacterID < nodes[j].CharacterID })

	edgeList := make([]coRaceEdge, 0, len(edges))
	for _, e := range edges {
		edgeList = append(edgeList, *e)
	}
	sort.Slice(edgeList, func(i, j int) bool {
		if edgeList[i].From != edgeList[j].From {
			return edgeList[i].From < edgeList[j].From
		}
		return edgeList[i].To < edgeList[j].To
	})

	largest := 0
	if len(components) > 0 {
		largest = len(components[0])
	}

	notes := []string{
		fmt.Sprintf("%d character nodes and %d undirected same-race edges from %d raceIds", len(nodes), len(edgeList), len(ctx.ds.Races)),
		fmt.Sprintf("%d connected components; largest holds %d characters; %d components have at least %d nodes", len(components), largest, usable, coRacePathMinComponent),
		fmt.Sprintf("the graph is NOT fully connected, so a start/end pair must be drawn from one component; max chain length is %d edges", coRacePathMaxEdges),
		"edges come only from the snapshot's prototype race history; a historical relation absent from it cannot be asserted",
	}
	if len(edgeList) < coRacePathMinEdges || largest < coRacePathMinComponent {
		return unavailable(ModeCoRacePath, SectionCoRacePathGraph, coRacePathMinEdges, tally, notes...), nil
	}

	return available(ModeCoRacePath, len(edgeList), coRacePathMinEdges, tally, notes, map[string]any{
		SectionCoRacePathGraph: coRacePathSection{
			Mode:             ModeCoRacePath,
			MaxChainEdges:    coRacePathMaxEdges,
			Nodes:            nodes,
			Edges:            edgeList,
			Components:       components,
			LargestComponent: largest,
		},
	}), nil
}

// coRacePathSection is the payload of "co-race-path.graph".
type coRacePathSection struct {
	Mode             string       `json:"mode"`
	MaxChainEdges    int          `json:"maxChainEdges"`
	Nodes            []coRaceNode `json:"nodes"`
	Edges            []coRaceEdge `json:"edges"`
	Components       [][]string   `json:"components"`
	LargestComponent int          `json:"largestComponent"`
}

// coRaceNode is one graph node.
type coRaceNode struct {
	CharacterID string `json:"characterId"`
	Name        string `json:"name"`
}

// coRaceEdge is one undirected evidence edge.
type coRaceEdge struct {
	From        string `json:"from"`
	To          string `json:"to"`
	RaceID      string `json:"raceId"`
	RaceName    string `json:"raceName,omitempty"`
	Date        string `json:"date,omitempty"`
	SharedRaces int    `json:"sharedRaces"`
}

// ---------------------------------------------------------------------------
// sort (G06): order cards by height, birthday or a verified event date
// ---------------------------------------------------------------------------

const (
	sortMinCards = 6
	sortMaxCards = 6
	sortMinPool  = 12
)

func compileSort(ctx *compileContext) (modeResult, error) {
	tally := RejectTally{}
	var cards []sortCard

	// A verified event date needs an explicit region: an event date from a
	// non-Japanese course must not be labelled "JP" .
	eventDate := make(map[string]sortEventDate)
	for _, r := range ctx.ds.Races {
		if r.Date == "" || r.Region == "" {
			continue
		}
		for _, e := range r.Entries {
			if e.CharacterID == "" {
				continue
			}
			cur, ok := eventDate[e.CharacterID]
			if !ok || r.Date < cur.Date {
				eventDate[e.CharacterID] = sortEventDate{Date: r.Date, Region: r.Region, RaceID: r.RaceID, RaceName: r.Name}
			}
		}
	}

	for _, c := range ctx.playableCharacters() {
		if c.Profile.HeightCm == nil && (c.Profile.BirthdayMonth == nil || c.Profile.BirthdayDay == nil) {
			tally.add(RejectNoHeightOrBirthday)
			continue
		}
		// The sort board is a row of cards; an unverified avatar would be a blank
		// face, so it is excluded .
		if c.ImagePath == nil {
			tally.add(RejectNoAsset)
			continue
		}
		card := sortCard{
			CharacterID:    c.ID,
			Name:           c.Names.ZhHans,
			ImagePath:      c.ImagePath,
			RemoteImageURL: c.RemoteImageURL,
			HeightCm:       c.Profile.HeightCm,
			BirthMonth:     c.Profile.BirthdayMonth,
			BirthDay:       c.Profile.BirthdayDay,
		}
		if ed, ok := eventDate[c.ID]; ok {
			card.EventDate = &ed
		}
		cards = append(cards, card)
	}
	sort.Slice(cards, func(i, j int) bool { return cards[i].CharacterID < cards[j].CharacterID })

	// Non-degeneracy: a sortable pool must be able to yield a 6-card set whose
	// compared values are not all equal .
	distinctHeights := make(map[int]bool)
	distinctBirthdays := make(map[string]bool)
	withHeight, withBirthday := 0, 0
	withEventDate := 0
	for _, c := range cards {
		if c.HeightCm != nil {
			withHeight++
			distinctHeights[*c.HeightCm] = true
		}
		if c.BirthMonth != nil && c.BirthDay != nil {
			withBirthday++
			distinctBirthdays[fmt.Sprintf("%02d-%02d", *c.BirthMonth, *c.BirthDay)] = true
		}
		if c.EventDate != nil {
			withEventDate++
		}
	}
	sortable := 0
	keys := []string{}
	if withHeight >= sortMaxCards && len(distinctHeights) >= 2 {
		sortable++
		keys = append(keys, "heightCm")
	}
	if withBirthday >= sortMaxCards && len(distinctBirthdays) >= 2 {
		sortable++
		keys = append(keys, "birthday")
	}
	if withEventDate >= sortMaxCards && countDistinctEventDates(cards) >= 2 {
		sortable++
		keys = append(keys, "eventDate")
	}
	if sortable == 0 {
		tally.add(RejectAllValuesEqual)
	}

	notes := []string{
		fmt.Sprintf("%d characters have a height and/or birthday; %d distinct heights, %d distinct month-day birthdays", len(cards), len(distinctHeights), len(distinctBirthdays)),
		fmt.Sprintf("%d characters have a verified JP event date from the race snapshot; %d distinct event dates", withEventDate, countDistinctEventDates(cards)),
		fmt.Sprintf("default card count %d; birthdays compare by month/day and never mix the prototype birth year", sortMaxCards),
		"event dates are normalised to ISO YYYY-MM-DD and carry an explicit region; string-literal date sorting is not used",
	}
	if len(cards) < sortMinPool || withHeight < sortMaxCards || len(distinctHeights) < 2 {
		return unavailable(ModeSort, SectionSortCards, sortMinPool, tally, notes...), nil
	}

	return available(ModeSort, len(cards), sortMinPool, tally, notes, map[string]any{
		SectionSortCards: sortSection{
			Mode:            ModeSort,
			Cards:           cards,
			DefaultCards:    sortMaxCards,
			SortableKeys:    keys,
			DistinctHeights: len(distinctHeights),
		},
	}), nil
}

// sortSection is the payload of "sort.cards".
type sortSection struct {
	Mode            string     `json:"mode"`
	Cards           []sortCard `json:"cards"`
	DefaultCards    int        `json:"defaultCards"`
	SortableKeys    []string   `json:"sortableKeys"`
	DistinctHeights int        `json:"distinctHeights"`
}

// sortCard is one sortable card.
type sortCard struct {
	CharacterID    string         `json:"characterId"`
	Name           string         `json:"name"`
	ImagePath      *string        `json:"imagePath"`
	RemoteImageURL string         `json:"remoteImageUrl,omitempty"`
	HeightCm       *int           `json:"heightCm"`
	BirthMonth     *int           `json:"birthMonth"`
	BirthDay       *int           `json:"birthDay"`
	EventDate      *sortEventDate `json:"eventDate"`
}

// sortEventDate is a verified event date with its region.
type sortEventDate struct {
	Date     string `json:"date"`
	Region   string `json:"region"`
	RaceID   string `json:"raceId,omitempty"`
	RaceName string `json:"raceName,omitempty"`
}

func countDistinctEventDates(cards []sortCard) int {
	seen := make(map[string]bool)
	for _, c := range cards {
		if c.EventDate != nil {
			seen[c.EventDate.Date] = true
		}
	}
	return len(seen)
}

// ---------------------------------------------------------------------------
// memory (G07): avatar pairing
// ---------------------------------------------------------------------------

// memoryMinPairs is the smallest pair pool the mode accepts. The default board is
// 4x4 with 8 pairs, so a pool under 8 can never fill one board .
const memoryMinPairs = 8

// memoryAvatarWidth and memoryAvatarHeight are the real pixel size of the staged
// avatars (256x280 RGBA). They are published so a client can reserve the box and a
// slow image load cannot reveal the board early.
const (
	memoryAvatarWidth  = 256
	memoryAvatarHeight = 280
)

// memoryPair is one publishable memory pair, matching the contract the memory
// mode reads: {pairId, imagePath, label}.
//
// A pair is ONE image used for both cards of that pair, which is exactly what
// requires ("每对用相同已审核图"). The public section therefore publishes one
// record per pair, not one per board position.
//
// What it must NOT publish is which pairs a board uses or where they sit: that is
// the hidden mapping, which lives in the private memory.solution section and is
// derived per round from the seed.
type memoryPair struct {
	// PairID is an opaque pair token. It is a salted digest, so it encodes neither
	// a character id nor anything a client could read back into an answer.
	PairID string `json:"pairId"`
	// ImagePath is the pack-relative route to this pair's approved image. It is
	// built from the same constant as the file location, so it always resolves.
	ImagePath string `json:"imagePath"`
	// Label is an OPAQUE PLACEHOLDER, never the character's name.
	//
	// The mode is about remembering positions, not recognising characters ,
	// so publishing the real name would let a player look up "who is this image"
	// instead of remembering. It would also let anyone build an image-to-name table
	// for the whole library from the public payload (: secret material stays
	// server-side). The readable name is projected from the private
	// memory.solution section after reveal.
	Label string `json:"label"`
	// Width and Height let a client reserve the box before the image loads, so a
	// slow load cannot reveal the board early.
	Width  int `json:"width"`
	Height int `json:"height"`
}

func compileMemory(ctx *compileContext) (modeResult, error) {
	tally := RejectTally{}
	var pairs []memoryPair
	// digestOwner detects two characters sharing one image: such a pair is
	// visually indistinguishable from the other, so the duplicate is dropped
	// .
	digestOwner := make(map[string]string, len(ctx.ast.byCharacter))
	// pairLabels carries the readable name per pair. It is published ONLY in the
	// private solution section, so the reveal can show a name without the public
	// payload ever containing one.
	pairLabels := make(map[string]string, 64)
	duplicates := 0
	noAvatar := 0
	usable := 0

	for _, c := range ctx.playableCharacters() {
		usable++
		av, ok := ctx.ast.avatarFor(c.ID)
		if !ok {
			tally.add(RejectNoAsset)
			noAvatar++
			continue
		}
		if _, dup := digestOwner[av.SHA256]; dup {
			duplicates++
			tally.add(RejectDuplicateImage)
			continue
		}
		digestOwner[av.SHA256] = c.ID
		pairID := opaquePairID(c.ID, ctx.ds.DatasetVersion)
		pairs = append(pairs, memoryPair{
			PairID:    pairID,
			ImagePath: PackMemoryPairRoute(pairID),
			// The placeholder is deliberately meaningless: it must not narrow down
			// which character a face shows.
			Label:  MemoryLabelPlaceholder,
			Width:  memoryAvatarWidth,
			Height: memoryAvatarHeight,
		})
		pairLabels[pairID] = c.Names.ZhHans
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].PairID < pairs[j].PairID })

	// Every published imagePath must resolve to a file the pack will contain.
	// This is checked against the verified set, not assumed.
	unresolved := 0
	for _, p := range pairs {
		if !pairImageResolves(p, ctx) {
			unresolved++
		}
	}

	notes := []string{
		fmt.Sprintf("%d of %d playable characters have an avatar confirmed on disk, yielding %d usable pairs", len(pairs), usable, len(pairs)),
		fmt.Sprintf("avatar source is %s (%s), keyed by the canonical numeric character id; every file was confirmed with os.Stat and its digest computed", avatarDir, AvatarSourceCanonical),
		fmt.Sprintf("the legacy asset manifest declares %d paths but %d of them are absent from disk, and the cache index declares %d with %d absent; that set contributes nothing and its cached flag was not trusted",
			ctx.ast.manifestEntries, ctx.ast.manifestMissingOnDisk, ctx.ast.cacheIndexEntries, ctx.ast.cacheIndexMissingOnDisk),
		fmt.Sprintf("%d characters had no avatar file; %d were dropped because their image duplicated another character's", noAvatar, duplicates),
		fmt.Sprintf("%d published imagePath values failed to resolve to a verified file (must be 0)", unresolved),
		fmt.Sprintf("the canonical avatar directory holds %d avatars but only %d are used: %d belong to planned characters with no in-game data, so they are not answer-eligible",
			ctx.ast.stagingVerified, len(pairs), ctx.ast.stagingVerified-len(pairs)-noAvatar),
		"the section publishes one record per pair (one approved image per pair); it carries no board layout, so it does not reveal which pairs a round uses",
		fmt.Sprintf("label is the opaque placeholder %q in every record: the readable character name is NOT public, so the payload cannot be turned into an image-to-name table; the real name is in the private memory.solution pairLabels map and is projected only after reveal", MemoryLabelPlaceholder),
		"pairId is an opaque salted digest, and the pair-to-character mapping is not published anywhere in the public tree",
		"Avatar bytes are copied into the content pack so served files are self-contained",
	}
	if unresolved > 0 {
		return unavailable(ModeMemory, SectionMemoryPairs, memoryMinPairs, tally, notes...), nil
	}
	if len(pairs) < memoryMinPairs {
		return unavailable(ModeMemory, SectionMemoryPairs, memoryMinPairs, tally, notes...), nil
	}

	// The board layout is derived per round from the seed, so only the pair
	// identities are secret here .
	sol := privateMemorySolution{
		Mode:          ModeMemory,
		SeedNamespace: seedNamespace(ModeMemory, ctx.ds.DatasetVersion),
		PairLabels:    pairLabels,
	}
	for _, p := range pairs {
		sol.PairIDs = append(sol.PairIDs, p.PairID)
	}
	sort.Strings(sol.PairIDs)

	res := available(ModeMemory, len(pairs), memoryMinPairs, tally, notes, map[string]any{
		SectionMemoryPairs: pairs,
	}, map[string]any{PrivateMemorySolution: sol})
	// The pair-to-character index is needed to write each pair's image copy. It is
	// carried out of band and never published.
	res.pairSources = make(map[string]string, len(pairs))
	for _, p := range pairs {
		if id, ok := ctx.ast.pairCharacter[p.PairID]; ok {
			res.pairSources[p.PairID] = id
		}
	}
	return res, nil
}

// pairImageResolves reports whether a pair's published imagePath maps to a
// verified avatar, by looking the pair up in the verification index.
func pairImageResolves(p memoryPair, ctx *compileContext) bool {
	id, ok := ctx.ast.pairCharacter[p.PairID]
	if !ok {
		return false
	}
	_, ok = ctx.ast.avatarFor(id)
	return ok
}

// opaquePairID derives a card-face id that cannot be read back as a character id.
// It is a truncated salted digest, so a client sees an opaque token while the
// server keeps the mapping in the private solution section.
func opaquePairID(characterID, datasetVersion string) string {
	sum := sha256.Sum256([]byte("memory-pair\x00" + datasetVersion + "\x00" + characterID))
	return "mp-" + hex.EncodeToString(sum[:8])
}

// distinctAvatarDigests counts the unique image digests among verified avatars.
func distinctAvatarDigests(ast assetVerification) map[string]bool {
	out := make(map[string]bool, len(ast.byCharacter))
	for _, av := range ast.byCharacter {
		out[av.SHA256] = true
	}
	return out
}

// ---------------------------------------------------------------------------
// bluff (G08): one true fact plus distractors
// ---------------------------------------------------------------------------

const bluffMinFacts = 8

// bluffReviewedStatus must equal internal/modes/bluff.ReviewStatusReviewed. The two
// packages cannot share the constant (content must not import a mode implementation),
// so the value is duplicated here deliberately and pinned by a test.
const bluffReviewedStatus = "reviewed"

// bluffReviewIndex indexes the human review ledger by fact id.
//
// Only entries that name a reviewer are returned: a ledger with approvals but no
// reviewer is not auditable, so it must not count as review. This is deliberately
// strict — the cost of being wrong here is publishing a false claim.
func (ctx *compileContext) bluffReviewIndex() map[string]RawBluffReview {
	out := make(map[string]RawBluffReview)
	if ctx.raw == nil {
		return out
	}
	for _, r := range ctx.raw.BluffReviews.Facts {
		if r.FactID == "" {
			continue
		}
		out[r.FactID] = r
	}
	return out
}

// reviewerName returns the reviewer recorded for the pass, or "" when none.
func (ctx *compileContext) reviewerName() string {
	if ctx.raw == nil {
		return ""
	}
	return strings.TrimSpace(ctx.raw.BluffReviews.Reviewer)
}

func compileBluff(ctx *compileContext) (modeResult, error) {
	tally := RejectTally{}
	reviews := ctx.bluffReviewIndex()
	var facts []bluffFact
	for _, c := range ctx.playableCharacters() {
		if len(c.Secrets) == 0 {
			tally.add(RejectNoSecret)
			continue
		}
		for i, s := range c.Secrets {
			factID := fmt.Sprintf("%s-%d", c.ID, i+1)
			_ = factID
			// A fact is publishable only when a human recorded an approval for it in the
			// review ledger. The default is pending: absence of a decision is not
			// consent, and the mode asserts the fact is true, so an unreviewed claim
			// would be fabricated evidence rather than game content .
			review, reviewed := reviews[factID]
			// The pack must speak the mode's vocabulary. The mode admits a fact only
			// when reviewStatus equals its own ReviewStatusReviewed constant; emitting
			// "approved" here made every fact look unreviewed and the mode refuse to
			// start even though the ledger approved it.
			status := string(ReviewPending)
			source := s.Source
			answer := ""
			if reviewed && review.Approved && review.Source != "" && ctx.reviewerName() != "" {
				candidate := strings.TrimSpace(review.Answer)
				// The answer must actually occur in the fact: blankPhrase blanks that
				// substring, so an answer the text does not contain produces an
				// unplayable prompt. Verified here rather than at Prepare time so the
				// build reports the mismatch instead of the round failing to start.
				if candidate != "" && strings.Contains(strings.ToLower(s.Text), strings.ToLower(candidate)) && validBluffFallbacks(review.FallbackAnswers, candidate) {
					status = bluffReviewedStatus
					source = review.Source
					answer = candidate
				}
			}
			facts = append(facts, bluffFact{
				FactID:          factID,
				CharacterID:     c.ID,
				CharacterName:   c.Names.ZhHans,
				Text:            s.Text,
				Answer:          answer,
				Source:          source,
				ReviewStatus:    status,
				FallbackAnswers: append([]string(nil), review.FallbackAnswers...),
			})
		}
	}
	sort.Slice(facts, func(i, j int) bool { return facts[i].FactID < facts[j].FactID })

	// Rejections are counted per candidate fact, which is the honest denominator: a
	// fact that no human has approved is not material, it is an unverified claim.
	approved := 0
	for _, f := range facts {
		// Admissible means exactly what the mode will accept: the reviewed status, a
		// citation, and a non-empty answer. Counting an entry that the mode would then
		// reject is what made the build report available while the round failed to start.
		if f.ReviewStatus == bluffReviewedStatus && f.Source != "" && f.Text != "" && f.Answer != "" {
			approved++
			continue
		}
		tally.add(RejectUnreviewedFact)
	}

	notes := []string{
		fmt.Sprintf("%d secret facts over %d playable characters were found", len(facts), len(ctx.playableCharacters())),
		fmt.Sprintf("%d approved by a named reviewer with a citation", approved),
		"a fact is publishable only when the review ledger records approved=true, a citation, a reviewer name, an answer occurring in the prompt and 8-32 distinct reviewed false alternatives; the bluff mode asserts the fact is true, so an unreviewed claim would be fabricated evidence rather than game content",
	}
	if approved < bluffMinFacts {
		return unavailable(ModeBluff, "", bluffMinFacts, tally, notes...), nil
	}
	return available(ModeBluff, approved, bluffMinFacts, tally, notes, map[string]any{
		"bluff.facts": facts,
	}), nil
}

// bluffFact is one candidate true fact.
type bluffFact struct {
	FactID        string `json:"factId"`
	CharacterID   string `json:"characterId"`
	CharacterName string `json:"characterName,omitempty"`
	Text          string `json:"text"`
	// Answer is the phrase the fact is about. The mode requires it non-empty because
	// the ballot asks players to pick the true statement, and grading needs the
	// reference phrase. It comes from the review ledger, never from parsing the text.
	Answer          string   `json:"answer,omitempty"`
	FallbackAnswers []string `json:"fallbackAnswers,omitempty"`
	Source          string   `json:"source,omitempty"`
	ReviewStatus    string   `json:"reviewStatus"`
}

// ---------------------------------------------------------------------------
// clue-puzzle (G09)
// ---------------------------------------------------------------------------

// compileCluePuzzle reports unavailability: the puzzle needs a solver-verified
// constraint set with per-member private clues, and the snapshot has no clue
// inventory at all. Inventing one would fabricate content .

// ---------------------------------------------------------------------------
// strategy-race (G10)
// ---------------------------------------------------------------------------

// compileStrategyRace reports unavailability: the mode is a deterministic rule
// simulation over the rules package, not over scraped content. It needs a rules
// version and playtest evidence, neither of which this content build owns.
// ---------------------------------------------------------------------------
// strategy-race (G10): an authored entertainment-simulation baseline
// ---------------------------------------------------------------------------

// rulesTrackPath is where the authored race baseline lives.
const rulesTrackPath = "data/manual/rules-track.json"

// rulesTrackProvenance is the provenance block the baseline must carry.
//
// It extends the trade provenance shape with a `simulation` flag, because the baseline is an
// entertainment simulation rather than an official game formula . Requiring the flag
// makes it impossible to publish a baseline that claims to be official.
type rulesTrackProvenance struct {
	Kind            string `json:"kind"`
	NotOfficialData bool   `json:"notOfficialData"`
	Author          string `json:"author"`
	Basis           string `json:"basis"`
	Simulation      bool   `json:"simulation"`
}

// rulesTrackAction is one row of the published action table.
type rulesTrackAction struct {
	Action        string `json:"action"`
	Label         string `json:"label"`
	PositionGain  int    `json:"positionGain"`
	AlternateGain int    `json:"alternateGain,omitempty"`
	StaminaDelta  int    `json:"staminaDelta"`
	MinStamina    int    `json:"minStamina,omitempty"`
	LeaderGap     bool   `json:"leaderGap,omitempty"`
}

// rulesTrack is the published baseline of one race.
type rulesTrack struct {
	TotalTurns      int                  `json:"totalTurns"`
	DecisionSeconds int                  `json:"decisionSeconds"`
	StartPosition   int                  `json:"startPosition"`
	StartStamina    int                  `json:"startStamina"`
	StaminaCap      int                  `json:"staminaCap"`
	FollowMinGap    int                  `json:"followMinGap"`
	FollowMaxGap    int                  `json:"followMaxGap"`
	DefaultAction   string               `json:"defaultAction"`
	Actions         []rulesTrackAction   `json:"actions"`
	TerrainChanges  bool                 `json:"terrainChanges"`
	RandomEvents    bool                 `json:"randomEvents"`
	Provenance      rulesTrackProvenance `json:"provenance"`
}

// rulesTrackDoc is the file shape: the baseline under a `track` key.
type rulesTrackDoc struct {
	Track rulesTrack `json:"track"`
}

// validRulesTrackActions is the closed set of documented actions.
var validRulesTrackActions = map[string]struct{}{
	"steady": {}, "sprint": {}, "recover": {}, "follow": {},
}

// compileStrategyRace publishes the authored race baseline.
//
// Unlike the scraped modes this one has no source rows to reject: the baseline is designed,
// not derived. The admission question is whether the baseline is complete and internally
// consistent, because an incomplete baseline cannot start a round — the mode refuses a pack
// whose actions table is missing a documented action.
//
// The mode reads this section at Prepare time and applies its numbers, so publishing it is
// what makes the mode playable rather than merely listed.
func compileStrategyRace(ctx *compileContext) (modeResult, error) {
	tally := RejectTally{}

	raw, err := os.ReadFile(filepath.Join(ctx.rootDir(), filepath.FromSlash(rulesTrackPath)))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return unavailable(ModeStrategyRace, "", 1, tally,
				fmt.Sprintf("%s is missing; the race baseline is authored, so it has no scraped source to fall back on", rulesTrackPath)), nil
		}
		return modeResult{}, fmt.Errorf("content: read %s: %w", rulesTrackPath, err)
	}
	var doc rulesTrackDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		return modeResult{}, fmt.Errorf("content: parse %s: %w", rulesTrackPath, err)
	}
	t := doc.Track

	// Every number the mode applies must be present and sane. A zero here would make the
	// race degenerate (a steady action that moves nobody), so it is a build error rather
	// than a silent default.
	if t.TotalTurns < 1 || t.TotalTurns > 32 {
		return modeResult{}, fmt.Errorf("content: %s has totalTurns %d outside [1,32]", rulesTrackPath, t.TotalTurns)
	}
	if t.DecisionSeconds < 5 || t.DecisionSeconds > 300 {
		return modeResult{}, fmt.Errorf("content: %s has decisionSeconds %d outside [5,300]", rulesTrackPath, t.DecisionSeconds)
	}
	if t.StaminaCap < 1 {
		return modeResult{}, fmt.Errorf("content: %s has staminaCap %d, must be positive", rulesTrackPath, t.StaminaCap)
	}
	if t.StartStamina < 0 || t.StartStamina > t.StaminaCap {
		return modeResult{}, fmt.Errorf("content: %s has startStamina %d outside [0,%d]", rulesTrackPath, t.StartStamina, t.StaminaCap)
	}
	if t.FollowMinGap < 1 || t.FollowMaxGap < t.FollowMinGap {
		return modeResult{}, fmt.Errorf("content: %s has an invalid follow gap range [%d,%d]", rulesTrackPath, t.FollowMinGap, t.FollowMaxGap)
	}

	seen := make(map[string]struct{}, len(t.Actions))
	for _, a := range t.Actions {
		if _, ok := validRulesTrackActions[a.Action]; !ok {
			return modeResult{}, fmt.Errorf("content: %s has unknown action %q", rulesTrackPath, a.Action)
		}
		if _, dup := seen[a.Action]; dup {
			return modeResult{}, fmt.Errorf("content: %s lists action %q twice", rulesTrackPath, a.Action)
		}
		seen[a.Action] = struct{}{}
		if a.PositionGain < 0 {
			return modeResult{}, fmt.Errorf("content: %s action %q has a negative position gain", rulesTrackPath, a.Action)
		}
		if a.Action == "follow" && a.AlternateGain <= 0 {
			return modeResult{}, fmt.Errorf("content: %s follow row carries no alternateGain", rulesTrackPath)
		}
		if a.Action == "sprint" && a.MinStamina <= 0 {
			return modeResult{}, fmt.Errorf("content: %s sprint row carries no minStamina", rulesTrackPath)
		}
	}
	for _, required := range []string{"steady", "sprint", "recover", "follow"} {
		if _, ok := seen[required]; !ok {
			return modeResult{}, fmt.Errorf("content: %s omits the action %q", rulesTrackPath, required)
		}
	}
	if _, ok := validRulesTrackActions[t.DefaultAction]; !ok {
		return modeResult{}, fmt.Errorf("content: %s defaultAction %q is not a documented action", rulesTrackPath, t.DefaultAction)
	}

	// Provenance: same rules as trade, plus the simulation flag.
	p := t.Provenance
	if !validProvenanceKinds[p.Kind] {
		return modeResult{}, fmt.Errorf("content: %s provenance.kind %q is not accepted", rulesTrackPath, p.Kind)
	}
	if p.Kind != "official-data" && !p.NotOfficialData {
		return modeResult{}, fmt.Errorf("content: %s is marked %q but notOfficialData is false", rulesTrackPath, p.Kind)
	}
	if strings.TrimSpace(p.Author) == "" {
		return modeResult{}, fmt.Errorf("content: %s has an empty provenance.author", rulesTrackPath)
	}
	if strings.TrimSpace(p.Basis) == "" {
		return modeResult{}, fmt.Errorf("content: %s has an empty provenance.basis", rulesTrackPath)
	}
	if !p.Simulation {
		// The baseline is an entertainment simulation. Publishing one that claims otherwise
		// would present designed numbers as an official formula .
		return modeResult{}, fmt.Errorf("content: %s provenance.simulation must be true: the race baseline is an entertainment simulation, not an official game formula", rulesTrackPath)
	}

	notes := []string{
		fmt.Sprintf("%d-turn authored race baseline from %s", t.TotalTurns, rulesTrackPath),
		"these are DESIGN VALUES for an entertainment simulation, not an official game formula: provenance.kind=authored-design-value, notOfficialData=true, simulation=true",
		"the mode reads this section at Prepare and applies its numbers, so the pack is the single source of truth for the race",
		"the rule layer validates provenance.kind, notOfficialData and simulation; it cannot verify that provenance.basis names a real document, so that remains a human review item",
	}

	// QuestionCount is the usable pool size, not the turn count.
	//
	// ModeAdmission.QuestionCount means "how many usable items this build compiled", and
	// the room layer compares it against ContentRequirement.MinEntities, which is a pool
	// floor. One complete baseline is one usable item, so both sides say 1. Reporting the
	// turn count here conflated "how many items exist" with "how many turns a round runs",
	// and forced MinEntities up to 2 to keep the comparison true.
	return available(ModeStrategyRace, 1, 1, tally, notes, map[string]any{
		SectionRulesTrack: t,
	}), nil
}

// ---------------------------------------------------------------------------
// pedigree (G11 in plan numbering): independent prototype identity
// ---------------------------------------------------------------------------

const pedigreeMinEntries = 8

func compilePedigree(ctx *compileContext) (modeResult, error) {
	tally := RejectTally{}
	var entries []pedigreeEntry
	totalPrototypes := 0
	for i := range ctx.ds.Characters {
		c := &ctx.ds.Characters[i]
		if c.Prototype == nil {
			continue
		}
		totalPrototypes++
		if !c.Prototype.HasNKBID() {
			// Without an independent prototype id, a shared surname would be the
			// only link, which the plan explicitly forbids inferring from .
			tally.add(RejectNoNKBID)
			continue
		}
		if !c.Prototype.Pedigree.Complete() {
			tally.add(RejectNoPedigree)
			continue
		}
		entries = append(entries, pedigreeEntry{
			CharacterID:    c.ID,
			Name:           c.Names.ZhHans,
			NKBID:          c.Prototype.NKBID,
			Pedigree:       c.Prototype.Pedigree,
			PrototypeNames: c.Prototype.Names,
		})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].CharacterID < entries[j].CharacterID })

	missingNKB := 0
	for i := range ctx.ds.Characters {
		if p := ctx.ds.Characters[i].Prototype; p != nil && !p.HasNKBID() {
			missingNKB++
		}
	}

	notes := []string{
		fmt.Sprintf("%d prototype blocks in the snapshot, of which %d lack an independent NKB id", totalPrototypes, missingNKB),
		fmt.Sprintf("%d prototypes have an NKB id and a complete six-slot pedigree", len(entries)),
		"relations come only from explicit pedigree fields; a shared surname is never treated as kinship",
	}
	if len(entries) < pedigreeMinEntries {
		return unavailable(ModePedigree, "", pedigreeMinEntries, tally, notes...), nil
	}

	return available(ModePedigree, len(entries), pedigreeMinEntries, tally, notes, map[string]any{
		"pedigree.entries": entries,
	}), nil
}

// pedigreeEntry is one prototype with an independent identity and full pedigree.
type pedigreeEntry struct {
	CharacterID    string   `json:"characterId"`
	Name           string   `json:"name"`
	NKBID          string   `json:"nkbId"`
	Pedigree       Pedigree `json:"pedigree"`
	PrototypeNames Names    `json:"prototypeNames"`
}

// ---------------------------------------------------------------------------
// audio (G12): voiced lines
// ---------------------------------------------------------------------------

const audioMinClips = 6

func compileAudio(ctx *compileContext) (modeResult, error) {
	tally := RejectTally{}
	clips := audioClipsFromRaw(ctx)
	totalClips, missingChs, withLocal := 0, 0, 0
	for _, c := range clips {
		totalClips++
		if c.Chs == "" {
			missingChs++
		}
		if c.HasLocalFile {
			withLocal++
		}
	}
	tally[RejectNoAudioFile] += totalClips - withLocal

	notes := []string{
		fmt.Sprintf("%d voice rows in the snapshot over %d characters, %d of them without a simplified-Chinese transcript", totalClips, len(ctx.ds.Characters), missingChs),
		fmt.Sprintf("%d rows have a downloaded, non-empty local clip under %s", withLocal, audioDir),
		"audio mode requires a decodable local file: the wiki records remote URLs, so a clip counts only once its bytes are on disk and non-empty",
	}
	// Admissible means what the mode will actually accept. Counting files alone made the
	// build report available while every clip failed the mode's own usable() check.
	admitted := []audioClip{}
	ids, speakers := map[string]bool{}, map[string]bool{}
	duration, mediaBytes := 0, 0
	for _, c := range clips {
		if c.ClipID != "" && c.CharacterID != "" && c.CharacterName != "" &&
			c.AudioPath == audioRouteFor(c.AudioURL) && c.AudioPath != "" &&
			c.DurationMillis > 0 && c.ByteLength > 0 && len(c.SHA256) == 64 &&
			c.Decodable && c.SpeakerVerified && c.Reviewed && !ids[c.ClipID] && !speakers[c.CharacterID] && duration+c.DurationMillis <= 180_000 && mediaBytes+c.ByteLength <= 12_000_000 {
			admitted = append(admitted, c)
			duration += c.DurationMillis
			mediaBytes += c.ByteLength
			ids[c.ClipID], speakers[c.CharacterID] = true, true
		}
	}
	// A deliberately small curated pool, not the multi-gigabyte source gallery.
	if len(admitted) > 24 {
		admitted = admitted[:24]
	}
	usable := len(admitted)
	notes = append(notes, fmt.Sprintf("%d distinct reviewed speakers satisfy local media admission", usable))
	if usable < audioMinClips {
		return unavailable(ModeAudio, "", audioMinClips, tally, notes...), nil
	}

	return available(ModeAudio, usable, audioMinClips, tally, notes, map[string]any{
		"audio.clips": admitted,
	}), nil
}

// audioReviewIndex returns the set of clip ids a human has listened to.
//
// It reads data/manual/audio-clips.review.json when present. Like the bluff ledger it
// is optional, and an entry counts only when it names the reviewer: a claim of review
// with nobody behind it is not auditable.
func (ctx *compileContext) audioReviewIndex() map[string]RawAudioReview {
	out := make(map[string]RawAudioReview)
	if ctx.raw == nil {
		return out
	}
	reviewer := strings.TrimSpace(ctx.raw.AudioReviews.Reviewer)
	if reviewer == "" || strings.TrimSpace(ctx.raw.AudioReviews.ReviewedAt) == "" {
		return out
	}
	counts := map[string]int{}
	for _, r := range ctx.raw.AudioReviews.Clips {
		counts[r.ClipID]++
	}
	for _, r := range ctx.raw.AudioReviews.Clips {
		if counts[r.ClipID] == 1 && r.ClipID != "" && r.Listened && r.SpeakerVerified && r.TranscriptVerified && r.NoNameSpoiler && len(r.SHA256) == 64 {
			out[r.ClipID] = r
		}
	}
	return out
}

// audioClipIDFor derives a stable, opaque clip id.
//
// It must not be the raw URL: the mode publishes the id to every viewer before the
// reveal, and a URL contains the CDN hash which is also the file name. Pairing the
// character id with a short digest keeps the id unique and stable without handing out
// a path a player could guess the answer from.
func audioClipIDFor(characterID, rawURL string) string {
	name := rawURL
	if i := strings.LastIndexByte(name, '/'); i >= 0 {
		name = name[i+1:]
	}
	if i := strings.IndexByte(name, '?'); i >= 0 {
		name = name[:i]
	}
	if characterID == "" || name == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(characterID + "\x00" + name))
	return "clip_" + hex.EncodeToString(sum[:8])
}

// audioPublicDir is where downloaded clips are published inside the pack.
//
// It sits under public/assets so the existing static media handler serves it with no
// new route: the handler already strips the /assets prefix and resolves the rest
// against the pack's public tree.
const audioPublicDir = "assets/audio"

// audioRouteFor returns the served path for a clip.
//
// The name is the CDN hash, which is unique per clip and stable across a change of
// CDN host, so the published route does not break when the upstream moves.
func audioRouteFor(rawURL string) string {
	name := audioFileName(rawURL)
	if name == "" {
		return ""
	}
	return "/" + audioPublicDir + "/" + name
}

// audioDir is where downloaded voice clips live, relative to the repository root.
//
// The wiki snapshot records only remote CDN URLs. Audio mode needs a decodable local
// file, both because the game must work offline and because the licence covers
// redistribution of the clip, not hot-linking the wiki. A clip therefore counts only
// when the file is actually on disk.
const audioDir = "data/assets/audio/library"

// hasLocalAudio reports whether a downloaded clip exists for one recorded URL.
//
// The file is named after the URL's last path segment: the wiki stores every clip as
// <hash>.webm, derived from the source URL hash, so the key survives a change of
// CDN host. Only the presence of a non-empty file counts — a zero-byte placeholder
// from a failed download must not pass, or the mode would offer silence.
func (ctx *compileContext) hasLocalAudio(rawURL string) bool {
	name := audioFileName(rawURL)
	if name == "" {
		return false
	}
	info, err := os.Stat(filepath.Join(ctx.rootDir(), audioDir, name))
	if err != nil || info.IsDir() || info.Size() == 0 {
		return false
	}
	return true
}

// audioClip is one candidate voiced line.
type audioClip struct {
	CharacterID string `json:"characterId"`
	// CharacterName is the speaker's display name. The mode publishes it on the
	// candidate list so a player can identify who they are matching; without it the
	// board would show opaque ids .
	CharacterName string `json:"characterName,omitempty"`
	// AudioPath is the served route a browser fetches, e.g. /assets/audio/<hash>.webm.
	// It is deliberately NOT the upstream CDN address: requires the game to
	// work without reaching a third party, and a hot-linked clip would fail offline
	// and depend on a URL that can change or expire.
	AudioPath string `json:"audioPath,omitempty"`
	// AudioURL is the upstream address, kept only for provenance. It is not served.
	AudioURL     string `json:"audioUrl,omitempty"`
	Jp           string `json:"jp"`
	Chs          string `json:"chs,omitempty"`
	Cht          string `json:"cht,omitempty"`
	HasLocalFile bool   `json:"hasLocalFile"`
	// The mode reads these names, not hasLocalFile. Emitting only hasLocalFile made
	// every clip look unusable and the round refuse to start, even though the file was
	// present. One fact, one name.
	ClipID          string `json:"clipId,omitempty"`
	DurationMillis  int    `json:"durationMillis,omitempty"`
	ByteLength      int    `json:"byteLength,omitempty"`
	SHA256          string `json:"sha256,omitempty"`
	Transcript      string `json:"transcript,omitempty"`
	Decodable       bool   `json:"decodable"`
	SpeakerVerified bool   `json:"speakerVerified"`
	Reviewed        bool   `json:"reviewed"`
}

// audioClipsFromRaw enumerates source rows but publishes media only after byte-bound
// listening review and full decoding. Downloaded files alone do not pass review.
func audioClipsFromRaw(ctx *compileContext) []audioClip {
	if ctx.raw == nil {
		return nil
	}
	var out []audioClip
	reviews := ctx.audioReviewIndex()
	for i := range ctx.raw.Wiki.Characters {
		w := &ctx.raw.Wiki.Characters[i]
		for _, v := range w.Voices {
			if v.AudioURL == "" && v.Jp == "" {
				continue
			}
			local := ctx.hasLocalAudio(v.AudioURL)
			clip := audioClip{
				CharacterID:   w.ID,
				CharacterName: w.Names.ZhHans,
				AudioURL:      v.AudioURL,
				Jp:            v.Jp,
				Chs:           v.Chs,
				Cht:           v.Cht,
				HasLocalFile:  local,
			}
			clip.ClipID = audioClipIDFor(w.ID, v.AudioURL)
			clip.Transcript = v.Chs
			if clip.Transcript == "" {
				clip.Transcript = v.Jp
			}
			// Only explicitly reviewed files are decoded. Downloading the whole source
			// gallery does not authorize publication or 15k decoder invocations.
			if review, ok := reviews[clip.ClipID]; ok && local {
				info, err := inspectAudioFile(filepath.Join(ctx.rootDir(), audioDir, audioFileName(v.AudioURL)))
				if err == nil && info.SHA256 == review.SHA256 {
					clip.AudioPath = audioRouteFor(v.AudioURL)
					clip.DurationMillis, clip.ByteLength, clip.SHA256 = info.DurationMillis, info.ByteLength, info.SHA256
					clip.Decodable = true
					clip.SpeakerVerified = review.SpeakerVerified && review.TranscriptVerified && review.NoNameSpoiler && clip.Transcript != ""
					clip.Reviewed = true
				}
			}
			out = append(out, clip)
		}
	}
	sort.Slice(out, func(a, b int) bool {
		if out[a].CharacterID != out[b].CharacterID {
			return out[a].CharacterID < out[b].CharacterID
		}
		return out[a].AudioURL < out[b].AudioURL
	})
	return out
}

func containsString(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

// skillAttributionStats measures the documented skill-attribution gap: 343 rows
// over 41 characters, only 22 matching a definition name exactly .
type skillAttributionStats struct {
	Rows            int
	Characters      int
	ExactMatches    int
	UnmatchedRows   []string
	DefinitionCount int
}

func measureSkillAttribution(chars []RawWikiCharacter, defs []RawSkillDef) skillAttributionStats {
	names := make(map[string]bool, len(defs))
	for _, d := range defs {
		names[d.NameZh] = true
	}
	st := skillAttributionStats{DefinitionCount: len(defs)}
	for i := range chars {
		rows := chars[i].Game.Skills.Rows()
		if len(rows) > 0 {
			st.Characters++
		}
		for _, r := range rows {
			st.Rows++
			if names[r] {
				st.ExactMatches++
			} else if len(st.UnmatchedRows) < 20 {
				st.UnmatchedRows = append(st.UnmatchedRows, r)
			}
		}
	}
	sort.Strings(st.UnmatchedRows)
	return st
}

// goalStats measures the training-goal parse gap: the scrape only ever yields
// indices 1-7, so the list must never be called complete .
type goalStats struct {
	Total      int
	Characters int
	MaxIndex   int
	Indices    []int
}

func measureGoals(chars []RawWikiCharacter) goalStats {
	idx := make(map[int]bool)
	var st goalStats
	for i := range chars {
		goals := chars[i].Game.Goals
		if len(goals) > 0 {
			st.Characters++
		}
		for _, g := range goals {
			st.Total++
			idx[g.Index] = true
			if g.Index > st.MaxIndex {
				st.MaxIndex = g.Index
			}
		}
	}
	for k := range idx {
		st.Indices = append(st.Indices, k)
	}
	sort.Ints(st.Indices)
	return st
}

// translationStats measures the flat translation table the snapshot exposes.
type translationStats struct {
	Rows int
}

func measureTranslations(chars []RawWikiCharacter) translationStats {
	slots := 11
	var st translationStats
	for i := range chars {
		n := &chars[i].Names
		vals := []string{n.Ja, n.ZhHans, n.ZhHant, n.En, n.GameZhHans, n.GameZhHant,
			n.WikiTitle, n.NicknameJp, n.MoegirlTitle, n.MoegirlZh, n.MoegirlJa}
		for j := 0; j < slots; j++ {
			if vals[j] != "" {
				st.Rows++
			}
		}
		st.Rows += len(n.ZhAll)
	}
	return st
}

var _ = strings.TrimSpace

// ---------------------------------------------------------------------------
// trade (G13): an authored card catalogue and objective set
// ---------------------------------------------------------------------------

// tradeMinCards is the smallest card-kind catalogue the mode can deal from.
//
// A hand is four cards; a catalogue smaller than that would repeat a kind inside one
// hand, making every two-kind objective unsatisfiable.
const tradeMinCards = 4

// tradeProvenance is the provenance block every authored entry carries.
//
// The fields are structural on purpose. A free-text "source" cannot be validated by a
// program — this project already published a review ledger whose "source" held a
// character name instead of a citation. These fields can be checked: kind must be a
// known enum value, and notOfficialData must be true for anything this project designed.
type tradeProvenance struct {
	Kind            string `json:"kind"`
	NotOfficialData bool   `json:"notOfficialData"`
	Author          string `json:"author"`
	Basis           string `json:"basis"`
}

// tradeCard is one authored card kind.
type tradeCard struct {
	CardID     string          `json:"cardId"`
	Label      string          `json:"label"`
	Provenance tradeProvenance `json:"provenance"`
}

// tradeObjective is one authored objective.
type tradeObjective struct {
	ObjectiveID string          `json:"objectiveId"`
	Text        string          `json:"text"`
	Kinds       []string        `json:"kinds"`
	Points      int             `json:"points"`
	Provenance  tradeProvenance `json:"provenance"`
}

// tradeContent is the authored content file, read from data/manual/trade-content.json.
//
// The content lives in a data file rather than in Go constants so there is exactly one
// copy. Keeping a second copy in code would guarantee drift, and this project has already
// been bitten by the same value existing in two places.
type tradeContent struct {
	SchemaVersion string           `json:"schemaVersion"`
	Cards         []tradeCard      `json:"cards"`
	Objectives    []tradeObjective `json:"objectives"`
}

// tradeContentPath is where the authored trade content lives.
const tradeContentPath = "data/manual/trade-content.json"

// validProvenanceKinds are the only provenance kinds this project accepts.
var validProvenanceKinds = map[string]bool{
	"authored-design-value": true,
	"derived-from-doc":      true,
	"official-data":         true,
}

// checkProvenance rejects an entry whose provenance cannot be verified structurally.
//
// It cannot verify that Basis names a real document — that is a human review item, and
// claiming otherwise would be the same mistake as accepting a review ledger nobody wrote.
func checkProvenance(what string, p tradeProvenance) error {
	if !validProvenanceKinds[p.Kind] {
		return fmt.Errorf("content: %s has provenance.kind %q, which is not one of the accepted values", what, p.Kind)
	}
	if p.Kind != "official-data" && !p.NotOfficialData {
		return fmt.Errorf("content: %s is marked %q but notOfficialData is false; designed values must declare they are not official data", what, p.Kind)
	}
	if strings.TrimSpace(p.Author) == "" {
		return fmt.Errorf("content: %s has an empty provenance.author", what)
	}
	if strings.TrimSpace(p.Basis) == "" {
		return fmt.Errorf("content: %s has an empty provenance.basis", what)
	}
	return nil
}

// compileTrade publishes the authored card catalogue and objective set.
//
// Unlike the scraped modes this one has no source rows to reject: the content is designed,
// not derived, so the only admission question is whether the catalogue is large enough and
// internally consistent. An inconsistency is a build error, not a rejection tally, because
// a designer wrote it and a mismatch means the design itself is broken.
func compileTrade(ctx *compileContext) (modeResult, error) {
	tally := RejectTally{}

	raw, err := os.ReadFile(filepath.Join(ctx.rootDir(), filepath.FromSlash(tradeContentPath)))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return unavailable(ModeTrade, "", tradeMinCards, tally,
				fmt.Sprintf("%s is missing; trade content is authored, so it has no scraped source to fall back on", tradeContentPath)), nil
		}
		return modeResult{}, fmt.Errorf("content: read %s: %w", tradeContentPath, err)
	}
	var doc tradeContent
	if err := json.Unmarshal(raw, &doc); err != nil {
		return modeResult{}, fmt.Errorf("content: parse %s: %w", tradeContentPath, err)
	}

	cards := make([]tradeCard, 0, len(doc.Cards))
	seenKind := make(map[string]struct{}, len(doc.Cards))
	for _, c := range doc.Cards {
		if c.CardID == "" || c.Label == "" {
			return modeResult{}, fmt.Errorf("content: trade card has an empty cardId or label")
		}
		if _, dup := seenKind[c.CardID]; dup {
			return modeResult{}, fmt.Errorf("content: trade card kind %q appears twice", c.CardID)
		}
		if err := checkProvenance("trade card "+c.CardID, c.Provenance); err != nil {
			return modeResult{}, err
		}
		seenKind[c.CardID] = struct{}{}
		cards = append(cards, c)
	}

	objectives := make([]tradeObjective, 0, len(doc.Objectives))
	seenObj := make(map[string]struct{}, len(doc.Objectives))
	kindUse := make(map[string]int, len(doc.Cards))
	for _, o := range doc.Objectives {
		if o.ObjectiveID == "" || o.Text == "" {
			return modeResult{}, fmt.Errorf("content: trade objective has an empty objectiveId or text")
		}
		if _, dup := seenObj[o.ObjectiveID]; dup {
			return modeResult{}, fmt.Errorf("content: trade objective %q appears twice", o.ObjectiveID)
		}
		if len(o.Kinds) != 2 {
			return modeResult{}, fmt.Errorf("content: trade objective %q requires %d kinds; exactly two are required", o.ObjectiveID, len(o.Kinds))
		}
		if o.Kinds[0] == o.Kinds[1] {
			// A repeated kind would be satisfied by a single card .
			return modeResult{}, fmt.Errorf("content: trade objective %q repeats the kind %q", o.ObjectiveID, o.Kinds[0])
		}
		for _, kind := range o.Kinds {
			if _, ok := seenKind[kind]; !ok {
				return modeResult{}, fmt.Errorf("content: trade objective %q requires kind %q which no card provides", o.ObjectiveID, kind)
			}
			kindUse[kind]++
		}
		if o.Points <= 0 {
			return modeResult{}, fmt.Errorf("content: trade objective %q has a non-positive points value", o.ObjectiveID)
		}
		if err := checkProvenance("trade objective "+o.ObjectiveID, o.Provenance); err != nil {
			return modeResult{}, err
		}
		seenObj[o.ObjectiveID] = struct{}{}
		objectives = append(objectives, o)
	}

	// Every kind must be reachable by at least one objective, or it is dead content: a
	// card nobody can be asked to hold has no trade value.
	for _, c := range cards {
		if kindUse[c.CardID] == 0 {
			return modeResult{}, fmt.Errorf("content: trade card kind %q appears in no objective", c.CardID)
		}
	}

	notes := []string{
		fmt.Sprintf("%d authored card kinds, %d authored objectives from %s", len(cards), len(objectives), tradeContentPath),
		"these are DESIGN VALUES, not official game data: every entry carries provenance.kind=authored-design-value and notOfficialData=true",
		fmt.Sprintf("the objective count must reach %d because the mode allows up to 8 players holding 2 objectives each; the mode refuses a smaller pool rather than repeating an objective", tradeMaxPlayers*tradeObjectivesPerPlayer),
		"the rule layer validates provenance.kind and notOfficialData; it cannot verify that provenance.basis names a real document, so that remains a human review item",
	}
	if len(cards) < tradeMinCards {
		return unavailable(ModeTrade, "", tradeMinCards, tally, notes...), nil
	}
	if len(objectives) < tradeMaxPlayers*tradeObjectivesPerPlayer {
		return unavailable(ModeTrade, "", tradeMaxPlayers*tradeObjectivesPerPlayer, tally, notes...), nil
	}

	return available(ModeTrade, len(objectives), tradeMaxPlayers*tradeObjectivesPerPlayer,
		tally, notes, map[string]any{
			SectionTradeCards:      cards,
			SectionTradeObjectives: objectives,
		}), nil
}

// tradeObjectivePoints and tradeObjectivesPerPlayer mirror the mode's published constants.
// They are duplicated because the content package must not import a mode package to read a
// number; the mode's own tests assert it refuses a pool that violates them.
const (
	tradeObjectivesPerPlayer = 2
	tradeMaxPlayers          = 8
)

// ---------------------------------------------------------------------------
// creative (G12): authored scene and constraint prompts
// ---------------------------------------------------------------------------

// creativePromptsPath is where the authored prompt pool lives.
const creativePromptsPath = "data/manual/creative-prompts.json"

// creativePromptProvenance is the provenance block an authored prompt must carry.
type creativePromptProvenance struct {
	Kind            string `json:"kind"`
	NotOfficialData bool   `json:"notOfficialData"`
	Author          string `json:"author"`
	Basis           string `json:"basis"`
}

// creativeScene is one authored situation.
type creativeScene struct {
	SceneID    string                   `json:"sceneId"`
	Text       string                   `json:"text"`
	Provenance creativePromptProvenance `json:"provenance"`
}

// creativeConstraint is one authored formal restriction.
type creativeConstraint struct {
	ConstraintID string                   `json:"constraintId"`
	Text         string                   `json:"text"`
	Provenance   creativePromptProvenance `json:"provenance"`
}

// creativePromptsDoc is the file shape.
type creativePromptsDoc struct {
	Scenes      []creativeScene      `json:"scenes"`
	Constraints []creativeConstraint `json:"constraints"`
}

// checkCreativeProvenance validates one prompt's provenance block.
func checkCreativeProvenance(what string, p creativePromptProvenance) error {
	if !validProvenanceKinds[p.Kind] {
		return fmt.Errorf("content: %s has provenance.kind %q, which is not one of the accepted values", what, p.Kind)
	}
	if p.Kind != "official-data" && !p.NotOfficialData {
		return fmt.Errorf("content: %s is marked %q but notOfficialData is false; designed values must declare they are not official data", what, p.Kind)
	}
	if strings.TrimSpace(p.Author) == "" {
		return fmt.Errorf("content: %s has an empty provenance.author", what)
	}
	if strings.TrimSpace(p.Basis) == "" {
		return fmt.Errorf("content: %s has an empty provenance.basis", what)
	}
	return nil
}

// compileCreative publishes the authored scene and constraint pool.
//
// The mode draws a scene and a constraint per seat and takes the character from the public
// name index, which it also declares. This compiler publishes only the authored half: the name
// index already exists as PublicIndexSection and must not be duplicated.
//
// Admission question: is the pool large enough to give every possible team a prompt? The mode
// allows up to 8 teams and draws per seat, so a pool smaller than 8 would force repeats. Repeats
// are not forbidden by the rules (two teams may share a scene), but a pool of 1 would make the
// round degenerate, so the floor is the mode's maximum team count.
func compileCreative(ctx *compileContext) (modeResult, error) {
	tally := RejectTally{}

	raw, err := os.ReadFile(filepath.Join(ctx.rootDir(), filepath.FromSlash(creativePromptsPath)))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return unavailable(ModeCreative, "", creativeMinScenes, tally,
				fmt.Sprintf("%s is missing; creative prompts are authored, so they have no scraped source to fall back on", creativePromptsPath)), nil
		}
		return modeResult{}, fmt.Errorf("content: read %s: %w", creativePromptsPath, err)
	}
	var doc creativePromptsDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		return modeResult{}, fmt.Errorf("content: parse %s: %w", creativePromptsPath, err)
	}

	seenScene := make(map[string]struct{}, len(doc.Scenes))
	for _, sc := range doc.Scenes {
		if sc.SceneID == "" || strings.TrimSpace(sc.Text) == "" {
			return modeResult{}, fmt.Errorf("content: %s has a scene with an empty sceneId or text", creativePromptsPath)
		}
		if _, dup := seenScene[sc.SceneID]; dup {
			return modeResult{}, fmt.Errorf("content: %s repeats the scene %q", creativePromptsPath, sc.SceneID)
		}
		if err := checkCreativeProvenance("creative scene "+sc.SceneID, sc.Provenance); err != nil {
			return modeResult{}, err
		}
		seenScene[sc.SceneID] = struct{}{}
	}

	seenConstraint := make(map[string]struct{}, len(doc.Constraints))
	for _, ct := range doc.Constraints {
		if ct.ConstraintID == "" || strings.TrimSpace(ct.Text) == "" {
			return modeResult{}, fmt.Errorf("content: %s has a constraint with an empty constraintId or text", creativePromptsPath)
		}
		if _, dup := seenConstraint[ct.ConstraintID]; dup {
			return modeResult{}, fmt.Errorf("content: %s repeats the constraint %q", creativePromptsPath, ct.ConstraintID)
		}
		if err := checkCreativeProvenance("creative constraint "+ct.ConstraintID, ct.Provenance); err != nil {
			return modeResult{}, err
		}
		seenConstraint[ct.ConstraintID] = struct{}{}
	}

	notes := []string{
		fmt.Sprintf("%d authored scenes and %d authored constraints from %s", len(doc.Scenes), len(doc.Constraints), creativePromptsPath),
		"these are DESIGN VALUES for player-written pitches, not official game data: provenance.kind=authored-design-value, notOfficialData=true",
		"the character a team draws comes from characters.index, which the mode also declares; this compiler publishes only the authored pool",
		"the rule layer validates provenance.kind and notOfficialData; it cannot verify that provenance.basis names a real document, so that remains a human review item",
	}
	if len(doc.Scenes) < creativeMinScenes || len(doc.Constraints) < creativeMinConstraints {
		return unavailable(ModeCreative, "", creativeMinScenes, tally, notes...), nil
	}

	// QuestionCount is the number of DISTINCT prompts available, which is what the mode needs to
	// give each of up to 8 teams a different draw. Reporting the scene count alone would ignore
	// the constraint pool, so the smaller of the two is the honest figure.
	count := len(doc.Scenes)
	if len(doc.Constraints) < count {
		count = len(doc.Constraints)
	}
	return available(ModeCreative, count, creativeMinScenes, tally, notes, map[string]any{
		SectionCreativePrompts: doc,
	}), nil
}

// creativeMinScenes and creativeMinConstraints are the floors the pool must clear.
//
// The mode allows up to 8 teams and draws per seat, so a pool below 8 would make every round
// repeat the same prompt. Repeats are allowed by the rules but a degenerate pool is not content.
const (
	creativeMinScenes      = 8
	creativeMinConstraints = 8
)

// ---------------------------------------------------------------------------
// rapport (G11): authored preference questions
// ---------------------------------------------------------------------------

// rapportQuestionsPath is where the authored question bank lives.
const rapportQuestionsPath = "data/manual/rapport-questions.json"

// rapportQuestionProvenance is the provenance block an authored question must carry.
type rapportQuestionProvenance struct {
	Kind            string `json:"kind"`
	NotOfficialData bool   `json:"notOfficialData"`
	Author          string `json:"author"`
	Basis           string `json:"basis"`
}

// rapportQuestion is one authored preference question.
type rapportQuestion struct {
	QuestionID string                    `json:"questionId"`
	Prompt     string                    `json:"prompt"`
	Options    []string                  `json:"options"`
	Provenance rapportQuestionProvenance `json:"provenance"`
}

// rapportQuestionsDoc is the file shape.
type rapportQuestionsDoc struct {
	Questions []rapportQuestion `json:"questions"`
}

// checkRapportProvenance validates one question's provenance block.
func checkRapportProvenance(what string, p rapportQuestionProvenance) error {
	if !validProvenanceKinds[p.Kind] {
		return fmt.Errorf("content: %s has provenance.kind %q, which is not one of the accepted values", what, p.Kind)
	}
	if p.Kind != "official-data" && !p.NotOfficialData {
		return fmt.Errorf("content: %s is marked %q but notOfficialData is false; designed values must declare they are not official data", what, p.Kind)
	}
	if strings.TrimSpace(p.Author) == "" {
		return fmt.Errorf("content: %s has an empty provenance.author", what)
	}
	if strings.TrimSpace(p.Basis) == "" {
		return fmt.Errorf("content: %s has an empty provenance.basis", what)
	}
	return nil
}

// compileRapport publishes the authored preference question bank.
//
// The mode draws DefaultQuestions (5) questions from this bank and rotates the chooser so every
// participant chooses at least once, so the bank must be at least as large as the largest table
// (8) or a full round cannot give everyone a turn.
//
// Every question needs exactly four distinct options: a question with fewer cannot be answered
// as designed, and duplicate labels would make two choices identical.
func compileRapport(ctx *compileContext) (modeResult, error) {
	tally := RejectTally{}

	raw, err := os.ReadFile(filepath.Join(ctx.rootDir(), filepath.FromSlash(rapportQuestionsPath)))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return unavailable(ModeRapport, "", rapportMinQuestions, tally,
				fmt.Sprintf("%s is missing; rapport questions are authored, so they have no scraped source to fall back on", rapportQuestionsPath)), nil
		}
		return modeResult{}, fmt.Errorf("content: read %s: %w", rapportQuestionsPath, err)
	}
	var doc rapportQuestionsDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		return modeResult{}, fmt.Errorf("content: parse %s: %w", rapportQuestionsPath, err)
	}

	seenID := make(map[string]struct{}, len(doc.Questions))
	for _, q := range doc.Questions {
		if q.QuestionID == "" || strings.TrimSpace(q.Prompt) == "" {
			return modeResult{}, fmt.Errorf("content: %s has a question with an empty questionId or prompt", rapportQuestionsPath)
		}
		if _, dup := seenID[q.QuestionID]; dup {
			return modeResult{}, fmt.Errorf("content: %s repeats the question %q", rapportQuestionsPath, q.QuestionID)
		}
		seenID[q.QuestionID] = struct{}{}
		if len(q.Options) != rapportOptionCount {
			return modeResult{}, fmt.Errorf("content: %s question %q carries %d options; exactly %d are required", rapportQuestionsPath, q.QuestionID, len(q.Options), rapportOptionCount)
		}
		seenOpt := make(map[string]struct{}, len(q.Options))
		for _, label := range q.Options {
			if strings.TrimSpace(label) == "" {
				return modeResult{}, fmt.Errorf("content: %s question %q carries an empty option", rapportQuestionsPath, q.QuestionID)
			}
			if _, dup := seenOpt[label]; dup {
				return modeResult{}, fmt.Errorf("content: %s question %q repeats the option %q", rapportQuestionsPath, q.QuestionID, label)
			}
			seenOpt[label] = struct{}{}
		}
		if err := checkRapportProvenance("rapport question "+q.QuestionID, q.Provenance); err != nil {
			return modeResult{}, err
		}
	}

	notes := []string{
		fmt.Sprintf("%d authored preference questions from %s", len(doc.Questions), rapportQuestionsPath),
		"these are DESIGN VALUES about player preferences, not official personality or popularity answers: provenance.kind=authored-design-value, notOfficialData=true",
		"the rule layer validates provenance.kind and notOfficialData; it cannot verify that provenance.basis names a real document, so that remains a human review item",
	}
	if len(doc.Questions) < rapportMinQuestions {
		return unavailable(ModeRapport, "", rapportMinQuestions, tally, notes...), nil
	}

	// QuestionCount is the usable POOL size (the bank), not the number of questions one round
	// draws. ModeAdmission.QuestionCount is compared against ContentRequirement.MinEntities,
	// which is also a pool floor.
	return available(ModeRapport, len(doc.Questions), rapportMinQuestions, tally, notes, map[string]any{
		SectionRapportQuestions: doc,
	}), nil
}

// rapportMinQuestions is the smallest bank the mode accepts.
//
// The mode runs DefaultQuestions (5) per round, so a bank below 5 cannot fill one round.
const rapportMinQuestions = 5

// rapportOptionCount is the fixed number of options per question .
const rapportOptionCount = 4

// rapportOptionCount is the fixed number of options per question .

// ---------------------------------------------------------------------------
// clue-puzzle (G09): an authored card deck
// ---------------------------------------------------------------------------

// cluePuzzleDeckPath is where the authored deck lives.
const cluePuzzleDeckPath = "data/manual/cluepuzzle-deck.json"

// clueDeckProvenance is the provenance block an authored card must carry.
type clueDeckProvenance struct {
	Kind            string `json:"kind"`
	NotOfficialData bool   `json:"notOfficialData"`
	Author          string `json:"author"`
	Basis           string `json:"basis"`
}

// clueDeckCard is one authored puzzle card.
type clueDeckCard struct {
	CardID     string             `json:"cardId"`
	Label      string             `json:"label"`
	Provenance clueDeckProvenance `json:"provenance"`
}

// clueDeckDoc is the file shape.
type clueDeckDoc struct {
	Cards []clueDeckCard `json:"cards"`
}

// checkClueDeckProvenance validates one card's provenance block.
func checkClueDeckProvenance(what string, p clueDeckProvenance) error {
	if !validProvenanceKinds[p.Kind] {
		return fmt.Errorf("content: %s has provenance.kind %q, which is not one of the accepted values", what, p.Kind)
	}
	if p.Kind != "official-data" && !p.NotOfficialData {
		return fmt.Errorf("content: %s is marked %q but notOfficialData is false; designed values must declare they are not official data", what, p.Kind)
	}
	if strings.TrimSpace(p.Author) == "" {
		return fmt.Errorf("content: %s has an empty provenance.author", what)
	}
	if strings.TrimSpace(p.Basis) == "" {
		return fmt.Errorf("content: %s has an empty provenance.basis", what)
	}
	return nil
}

// compileCluePuzzle publishes the authored card deck.
//
// The mode draws 4–6 cards per round from this pool and generates constraints that pin the
// order, so the pool must be larger than the largest round (6) or every round would use the
// whole deck and the draw would be pointless.
//
// QuestionCount is counted in DECK CARDS because ContentRequirement.MinEntities is also counted
// in cards. It used to report the generated constraint count, which is a different unit: the
// availability gate compared constraints against a card floor.
func compileCluePuzzle(ctx *compileContext) (modeResult, error) {
	tally := RejectTally{}

	raw, err := os.ReadFile(filepath.Join(ctx.rootDir(), filepath.FromSlash(cluePuzzleDeckPath)))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return unavailable(ModeCluePuzzle, "", cluePuzzleMinCards, tally,
				fmt.Sprintf("%s is missing; the puzzle deck is authored, so it has no scraped source to fall back on", cluePuzzleDeckPath)), nil
		}
		return modeResult{}, fmt.Errorf("content: read %s: %w", cluePuzzleDeckPath, err)
	}
	var doc clueDeckDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		return modeResult{}, fmt.Errorf("content: parse %s: %w", cluePuzzleDeckPath, err)
	}

	seenID := make(map[string]struct{}, len(doc.Cards))
	seenLabel := make(map[string]struct{}, len(doc.Cards))
	for _, c := range doc.Cards {
		if c.CardID == "" || strings.TrimSpace(c.Label) == "" {
			return modeResult{}, fmt.Errorf("content: %s has a card with an empty cardId or label", cluePuzzleDeckPath)
		}
		if _, dup := seenID[c.CardID]; dup {
			return modeResult{}, fmt.Errorf("content: %s repeats the card id %q", cluePuzzleDeckPath, c.CardID)
		}
		seenID[c.CardID] = struct{}{}
		// A duplicate label would make two cards indistinguishable on the board, so the puzzle
		// would have two valid answers that look identical.
		if _, dup := seenLabel[c.Label]; dup {
			return modeResult{}, fmt.Errorf("content: %s repeats the card label %q", cluePuzzleDeckPath, c.Label)
		}
		seenLabel[c.Label] = struct{}{}
		if err := checkClueDeckProvenance("clue-puzzle card "+c.CardID, c.Provenance); err != nil {
			return modeResult{}, err
		}
	}

	notes := []string{
		fmt.Sprintf("%d authored puzzle cards from %s", len(doc.Cards), cluePuzzleDeckPath),
		"these are DESIGN VALUES for an abstract ordering puzzle, not official game data: provenance.kind=authored-design-value, notOfficialData=true",
		"the round draws 4-6 cards from this pool and generates solver-verified constraints; the pool is larger than the largest round so the draw is meaningful",
		"the rule layer validates provenance.kind and notOfficialData; it cannot verify that provenance.basis names a real document, so that remains a human review item",
	}
	if len(doc.Cards) < cluePuzzleMinCards {
		return unavailable(ModeCluePuzzle, "", cluePuzzleMinCards, tally, notes...), nil
	}

	return available(ModeCluePuzzle, len(doc.Cards), cluePuzzleMinCards, tally, notes, map[string]any{
		SectionCluePuzzleDeck: doc,
	}), nil
}

// cluePuzzleMinCards is the smallest deck the mode accepts.
//
// The mode's own MinEntities is MinCards (4), and a round draws at most 6, so a pool below 4
// cannot fill even the smallest round.
const cluePuzzleMinCards = 4

// Eight reviewed alternatives cover the maximum missing roster without inventing
// content at runtime. Exact normalisation matches the mode's equality rule.
func validBluffFallbacks(values []string, truth string) bool {
	if len(values) < 8 || len(values) > 32 {
		return false
	}
	key := func(s string) string {
		s = norm.NFC.String(s)
		var b strings.Builder
		for _, r := range s {
			if r >= 0xFF01 && r <= 0xFF5E {
				r -= 0xFEE0
			}
			b.WriteRune(r)
		}
		return strings.ToLower(strings.Join(strings.Fields(b.String()), " "))
	}
	seen := map[string]bool{key(truth): true}
	for _, raw := range values {
		text := norm.NFC.String(strings.TrimSpace(raw))
		n := utf8.RuneCountInString(text)
		k := key(text)
		if n < 2 || n > 40 || seen[k] {
			return false
		}
		seen[k] = true
	}
	return true
}
