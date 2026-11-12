package cluepuzzle

// This file holds the puzzle generator and its solver for G09 团队情报拼图.
//
// The plan's rule is "生成器先生成有效方案与约束，再求解" : the generator
// first commits to a solution, then emits constraints that solution satisfies, and
// only then does an independent solver check the result. That order matters —
// generating constraints and hoping a solution exists is how a puzzle ships
// unsolvable.
//
// The solver is a plain exhaustive search over assignments. The search space is
// small by construction (4–6 cards, 4–6 slots), so an exact count of solutions is
// cheap and the "唯一解" requirement can be verified rather than assumed.

import (
	"fmt"
	"sort"
	"strings"

	"github.com/bhcn/umaguessinggame/internal/modes/modeutil"
)

// Constraint kinds. Each kind has a checker below; an unknown kind makes the
// puzzle invalid rather than being skipped, so a generator bug cannot silently
// reduce the constraint count.
const (
	// KindNotWith states that two cards are not adjacent in the answer order.
	KindNotWith = "not_with"
	// KindBefore states that one card is placed strictly before another.
	KindBefore = "before"
	// KindPosition states that a card occupies an exact slot index (1-based).
	KindPosition = "position"
	// KindAdjacent states that two cards are adjacent, in either order.
	KindAdjacent = "adjacent"
	// KindGroup states that a set of cards occupies one contiguous block.
	KindGroup = "group"
)

// constraint is one public clue. Its Text is what the players read; its
// parameters are what the solver checks, so the two can never drift apart.
type constraint struct {
	ConstraintID string `json:"constraintId"`
	Kind         string `json:"kind"`
	// Text is the human-readable statement shown in the public area.
	Text string `json:"text"`
	// Cards is the card set the constraint refers to.
	Cards []string `json:"cards"`
	// Position is the 1-based slot for KindPosition.
	Position int `json:"position,omitempty"`
	// Confirmatory marks a constraint that does not narrow the solution set
	// further, because the puzzle was already uniquely determined when it was
	// added. Such a clue is still a true statement about the answer, and it exists
	// so every participant can hold a clue nobody else holds — the alternative
	// would be to run a 4-player round on a 4-card board with only three
	// informative clues, which cannot satisfy the documented per-player rule.
	//
	// The field is recorded rather than hidden so the evidence and the reveal can
	// state how many clues were load-bearing.
	Confirmatory bool `json:"confirmatory,omitempty"`
}

// solution is one complete assignment: slot index (1-based) → card id.
type solution map[int]string

// clone copies a solution.
func (s solution) clone() solution {
	out := make(solution, len(s))
	for k, v := range s {
		out[k] = v
	}
	return out
}

// key renders a solution as a canonical string, used for de-duplication.
func (s solution) key() string {
	slots := make([]int, 0, len(s))
	for slot := range s {
		slots = append(slots, slot)
	}
	sort.Ints(slots)
	var b strings.Builder
	for _, slot := range slots {
		fmt.Fprintf(&b, "%d=%s;", slot, s[slot])
	}
	return b.String()
}

// slotOf finds the slot a card occupies, 0 when unplaced.
func (s solution) slotOf(card string) int {
	for slot, id := range s {
		if id == card {
			return slot
		}
	}
	return 0
}

// satisfies reports whether a complete solution satisfies one constraint.
//
// A constraint whose parameters are malformed is reported as an error, not as a
// silent failure: a puzzle whose constraints cannot be evaluated must be refused
// before it reaches a player.
func satisfies(c constraint, sol solution, cardCount int) (bool, error) {
	switch c.Kind {
	case KindNotWith:
		if len(c.Cards) != 2 {
			return false, fmt.Errorf("cluepuzzle: not_with needs 2 cards, got %d", len(c.Cards))
		}
		a, b := sol.slotOf(c.Cards[0]), sol.slotOf(c.Cards[1])
		if a == 0 || b == 0 {
			return false, fmt.Errorf("cluepuzzle: not_with names a card that is not placed")
		}
		// Adjacent means consecutive slots, in either order.
		delta := a - b
		if delta < 0 {
			delta = -delta
		}
		return delta != 1, nil

	case KindBefore:
		if len(c.Cards) != 2 {
			return false, fmt.Errorf("cluepuzzle: before needs 2 cards, got %d", len(c.Cards))
		}
		a, b := sol.slotOf(c.Cards[0]), sol.slotOf(c.Cards[1])
		if a == 0 || b == 0 {
			return false, fmt.Errorf("cluepuzzle: before names a card that is not placed")
		}
		return a < b, nil

	case KindPosition:
		if len(c.Cards) != 1 {
			return false, fmt.Errorf("cluepuzzle: position needs 1 card, got %d", len(c.Cards))
		}
		if c.Position < 1 || c.Position > cardCount {
			return false, fmt.Errorf(
				"cluepuzzle: position %d outside [1,%d]", c.Position, cardCount)
		}
		slot := sol.slotOf(c.Cards[0])
		if slot == 0 {
			return false, fmt.Errorf("cluepuzzle: position names a card that is not placed")
		}
		return slot == c.Position, nil

	case KindAdjacent:
		if len(c.Cards) != 2 {
			return false, fmt.Errorf("cluepuzzle: adjacent needs 2 cards, got %d", len(c.Cards))
		}
		a, b := sol.slotOf(c.Cards[0]), sol.slotOf(c.Cards[1])
		if a == 0 || b == 0 {
			return false, fmt.Errorf("cluepuzzle: adjacent names a card that is not placed")
		}
		delta := a - b
		if delta < 0 {
			delta = -delta
		}
		return delta == 1, nil

	case KindGroup:
		if len(c.Cards) < 2 {
			return false, fmt.Errorf("cluepuzzle: group needs at least 2 cards, got %d", len(c.Cards))
		}
		slots := make([]int, 0, len(c.Cards))
		for _, card := range c.Cards {
			slot := sol.slotOf(card)
			if slot == 0 {
				return false, fmt.Errorf("cluepuzzle: group names a card that is not placed")
			}
			slots = append(slots, slot)
		}
		sort.Ints(slots)
		// Contiguous when the slots form a run with no gap.
		for i := 1; i < len(slots); i++ {
			if slots[i] != slots[i-1]+1 {
				return false, nil
			}
		}
		return true, nil
	}
	return false, fmt.Errorf("cluepuzzle: unknown constraint kind %q", c.Kind)
}

// satisfiesCount returns how many of constraints the solution satisfies.
func satisfiesCount(constraints []constraint, sol solution, cardCount int) (int, error) {
	n := 0
	for _, c := range constraints {
		ok, err := satisfies(c, sol, cardCount)
		if err != nil {
			return 0, err
		}
		if ok {
			n++
		}
	}
	return n, nil
}

// solveAll enumerates every complete assignment of cards to slots that satisfies
// every constraint. It is an exact search, not a heuristic: the caller needs the
// true count to decide whether the puzzle has a unique solution.
//
// The bound is the number of solutions the caller wants to know about; once it is
// reached the search stops, which keeps a badly generated puzzle from exploding.
func solveAll(cards []string, constraints []constraint, limit int) ([]solution, error) {
	if limit <= 0 {
		limit = 1
	}
	n := len(cards)
	if n == 0 {
		return nil, fmt.Errorf("cluepuzzle: no cards to place")
	}
	// A repeated card would make "assign each card once" ill-defined.
	seen := make(map[string]struct{}, n)
	for _, card := range cards {
		if _, dup := seen[card]; dup {
			return nil, fmt.Errorf("cluepuzzle: card %q appears twice", card)
		}
		seen[card] = struct{}{}
	}
	// Validate every constraint once up front, so an evaluator error surfaces
	// here instead of inside the search.
	for _, c := range constraints {
		for _, card := range c.Cards {
			if _, ok := seen[card]; !ok {
				return nil, fmt.Errorf(
					"cluepuzzle: constraint %q names card %q which is not in the deck",
					c.ConstraintID, card)
			}
		}
	}

	var out []solution
	current := make(solution, n)
	used := make(map[string]bool, n)

	// walk places one slot at a time. deckSize is captured explicitly so the
	// constraint evaluator always receives the true board size.
	deckSize := n
	var walk func(slot int) error
	walk = func(slot int) error {
		if len(out) >= limit {
			return nil
		}
		if slot > deckSize {
			ok, err := satisfiesCount(constraints, current, deckSize)
			if err != nil {
				return err
			}
			if ok == len(constraints) {
				out = append(out, current.clone())
			}
			return nil
		}
		for _, card := range cards {
			if used[card] {
				continue
			}
			used[card] = true
			current[slot] = card
			if err := walk(slot + 1); err != nil {
				return err
			}
			delete(current, slot)
			used[card] = false
			if len(out) >= limit {
				return nil
			}
		}
		return nil
	}
	if err := walk(1); err != nil {
		return nil, err
	}
	return out, nil
}

// countSolutions returns the exact number of solutions up to limit.
func countSolutions(cards []string, constraints []constraint, limit int) (int, error) {
	sols, err := solveAll(cards, constraints, limit)
	if err != nil {
		return 0, err
	}
	return len(sols), nil
}

// generateResult is the outcome of generation.
type generateResult struct {
	// Solution is the intended answer.
	Solution solution
	// Constraints are the public clues the solution satisfies.
	Constraints []constraint
	// SolutionCount is how many complete assignments satisfy all constraints.
	SolutionCount int
	// PrivateClueIDs maps participant id → the clue ids that participant holds.
	// Every participant must hold at least one clue that no other participant
	// holds, which is what makes the round cooperative .
	PrivateClueIDs map[string][]string
	// SharedConstraintIDs are the constraints published in the public area.
	SharedConstraintIDs []string
	// SharedEmptyReason explains why the shared area is empty, when it is. An empty shared
	// area is legitimate on a small board — every constraint is needed as somebody's private
	// clue — but a player must be told which case they are in, so the client can say "this
	// board has no shared starting clues" instead of rendering a silent gap.
	SharedEmptyReason string
}

// generate builds one puzzle from a seed.
//
// The algorithm, in the order the plan requires:
//
// 1. Commit to a solution: shuffle the deck and assign it to slots.
// 2. Emit candidate constraints that the solution satisfies.
// 3. Keep only the constraints that reduce the solution count toward the target,
// so the puzzle is not solvable without them.
// 4. Verify with the solver: the final count must be 1 (unique) or, when the
// round allows multiple answers, the count must be > 0 and every constraint
// must be satisfied by at least one of the accepted solutions.
// 5. Distribute the kept constraints as private clues so every participant holds
// at least one clue, and publish the shared ones.
func generate(seed string, cards []string, cfg Config) (generateResult, error) {
	n := len(cards)
	if n < MinCards || n > MaxCards {
		return generateResult{}, fmt.Errorf(
			"cluepuzzle: %d cards outside [%d,%d]", n, MinCards, MaxCards)
	}
	if cfg.Participants < MinParticipants || cfg.Participants > MaxParticipants {
		return generateResult{}, fmt.Errorf(
			"cluepuzzle: %d participants outside [%d,%d]",
			cfg.Participants, MinParticipants, MaxParticipants)
	}

	// Step 1: the intended answer.
	stream := modeutil.NewStream(seed + "\x00cluepuzzle.solution")
	deck := make([]string, len(cards))
	copy(deck, cards)
	stream.Shuffle(len(deck), func(i, j int) { deck[i], deck[j] = deck[j], deck[i] })
	intended := make(solution, n)
	for i, card := range deck {
		intended[i+1] = card
	}

	// Step 2: candidate constraints the intended answer satisfies.
	candidates := candidateConstraints(deck, n, seed)

	// Step 3: select the constraints.
	//
	// A single greedy pass over the candidates can stall: accepting one constraint
	// may be what makes a later one informative, so a pass that only accepts
	// strict improvements can stop at a local minimum with several solutions left.
	// The selection therefore runs repeated passes until a pass makes no progress,
	// and only then relaxes to "never worsens" so it can cross a plateau.
	limit := n*factorial(n) + 1
	kept, err := selectConstraints(cards, candidates, intended, n, cfg, limit)
	if err != nil {
		return generateResult{}, err
	}
	if len(kept) == 0 {
		return generateResult{}, fmt.Errorf(
			"cluepuzzle: the seed produced no informative constraint")
	}

	// Top up so every participant can hold a private clue. The documented bounds
	// allow a 4-card board with 4 participants, and a 4-card board sometimes
	// becomes unique after only three informative clues; without this step that
	// combination could never satisfy "each participant holds one contributing
	// private clue" .
	//
	// The added clues are true statements the answer satisfies, and they are
	// marked Confirmatory so nobody can later mistake them for load-bearing
	// constraints. They are never duplicates: keptIDs is consulted on every add.
	needed := cfg.Participants
	keptIDs := make(map[string]struct{}, len(kept))
	for _, c := range kept {
		keptIDs[c.ConstraintID] = struct{}{}
	}
	for _, cand := range candidates {
		if len(kept) >= needed {
			break
		}
		if _, dup := keptIDs[cand.ConstraintID]; dup {
			continue
		}
		ok, err := satisfies(cand, intended, n)
		if err != nil {
			return generateResult{}, err
		}
		if !ok {
			continue
		}
		cand.Confirmatory = true
		kept = append(kept, cand)
		keptIDs[cand.ConstraintID] = struct{}{}
	}
	if len(kept) < needed {
		return generateResult{}, fmt.Errorf(
			"cluepuzzle: only %d true constraints exist for a %d-participant round",
			len(kept), needed)
	}
	// Step 4: verify with the solver.
	finalSols, err := solveAll(cards, kept, limit)
	if err != nil {
		return generateResult{}, err
	}
	if len(finalSols) == 0 {
		return generateResult{}, fmt.Errorf(
			"cluepuzzle: the generated constraint set has no solution")
	}
	if cfg.Unique && len(finalSols) != 1 {
		return generateResult{}, fmt.Errorf(
			"cluepuzzle: %d solutions satisfy the constraints but the round requires exactly 1",
			len(finalSols))
	}
	// Every accepted solution must satisfy every constraint, which is what the
	// search already guarantees; assert it so a future optimised solver cannot
	// quietly return a partial match.
	for _, sol := range finalSols {
		ok, err := satisfiesCount(kept, sol, n)
		if err != nil {
			return generateResult{}, err
		}
		if ok != len(kept) {
			return generateResult{}, fmt.Errorf(
				"cluepuzzle: solver returned a solution satisfying %d of %d constraints",
				ok, len(kept))
		}
	}
	// The intended answer must be among the accepted solutions.
	intendedAccepted := false
	for _, sol := range finalSols {
		if sol.key() == intended.key() {
			intendedAccepted = true
			break
		}
	}
	if !intendedAccepted {
		return generateResult{}, fmt.Errorf(
			"cluepuzzle: the intended answer does not satisfy the generated constraints")
	}

	// Step 5: distribute the clues.
	private, shared, sharedEmptyReason, err := distribute(kept, cfg, seed)
	if err != nil {
		return generateResult{}, err
	}

	return generateResult{
		Solution:            intended,
		Constraints:         kept,
		SolutionCount:       len(finalSols),
		PrivateClueIDs:      private,
		SharedConstraintIDs: shared,
		SharedEmptyReason:   sharedEmptyReason,
	}, nil
}

// selectConstraints picks the constraints a round keeps.
//
// Selection is best-first, not a single greedy pass. For every remaining candidate
// it evaluates the exact solution count the candidate would leave, and keeps the
// one that narrows the puzzle most. A single pass can stall at a local minimum —
// accepting one constraint is sometimes what makes a later one informative — so
// the loop repeats until the puzzle is uniquely determined or nothing improves.
//
// When no candidate strictly narrows the set, the loop accepts a candidate that
// merely does not widen it, which lets the search cross a plateau and continue.
// This matters because the "before" clues form a chain that pins the total order,
// and a chain member can be non-narrowing on its own while being essential to the
// next step.
//
// Position clues are capped: a board of them would hand the answer over without
// any deduction, and requires the clues to be necessary rather than merely
// sufficient.
func selectConstraints(cards []string, candidates []constraint, intended solution,
	n int, cfg Config, limit int) ([]constraint, error) {
	kept := make([]constraint, 0, cfg.ConstraintCount)
	keptIDs := make(map[string]struct{}, cfg.ConstraintCount)
	positionKept := 0
	currentCount, err := countSolutions(cards, nil, limit)
	if err != nil {
		return nil, err
	}

	// allowed reports whether cand may be considered at all.
	allowed := func(cand constraint) bool {
		if len(kept) >= cfg.ConstraintCount {
			return false
		}
		if _, dup := keptIDs[cand.ConstraintID]; dup {
			return false
		}
		if cand.Kind == KindPosition && positionKept >= maxPositionClues {
			return false
		}
		return true
	}

	// trial evaluates one candidate against the current kept set.
	trial := func(cand constraint) (int, bool, error) {
		ok, err := satisfies(cand, intended, n)
		if err != nil {
			return 0, false, err
		}
		if !ok {
			return 0, false, nil
		}
		next := append(append([]constraint{}, kept...), cand)
		count, err := countSolutions(cards, next, limit)
		if err != nil {
			return 0, false, err
		}
		if count > currentCount {
			// A true constraint can never widen the set; a widening result means
			// the evaluator and the intended answer disagree, which is a bug.
			return 0, false, fmt.Errorf(
				"cluepuzzle: constraint %q widened the solution count from %d to %d",
				cand.ConstraintID, currentCount, count)
		}
		return count, true, nil
	}

	// accept appends cand and updates the running state.
	accept := func(cand constraint, count int) {
		kept = append(kept, cand)
		keptIDs[cand.ConstraintID] = struct{}{}
		if cand.Kind == KindPosition {
			positionKept++
		}
		currentCount = count
	}

	// Iterations are bounded so a pathological deck cannot spin: each successful
	// step consumes a candidate, and the budget is the candidate count.
	maxSteps := len(candidates) + 1
	for step := 0; step < maxSteps; step++ {
		if currentCount <= 1 || len(kept) >= cfg.ConstraintCount {
			break
		}

		// Best-first: pick the candidate leaving the fewest solutions. Ties are
		// broken by constraint id so the choice is deterministic.
		bestID := ""
		bestCount := currentCount
		bestStrict := false
		for _, cand := range candidates {
			if !allowed(cand) {
				continue
			}
			count, ok, err := trial(cand)
			if err != nil {
				return nil, err
			}
			if !ok {
				continue
			}
			switch {
			case count < bestCount:
				bestID, bestCount, bestStrict = cand.ConstraintID, count, true
			case count == bestCount && !bestStrict && bestID == "":
				// A non-narrowing candidate is a plateau move; hold it as the
				// fallback so the loop can still make progress.
				bestID, bestCount = cand.ConstraintID, count
			}
		}
		if bestID == "" {
			// Nothing left that even keeps the set the same size.
			break
		}
		var chosen constraint
		found := false
		for _, cand := range candidates {
			if cand.ConstraintID == bestID {
				chosen, found = cand, true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("cluepuzzle: selected constraint %q vanished", bestID)
		}
		accept(chosen, bestCount)
	}

	return kept, nil
}

// maxPositionClues caps how many exact-position clues a puzzle may keep. A board
// made of them would hand the answer over without any deduction ( requires
// the clues to be necessary, not merely sufficient).
const maxPositionClues = 1

// factorial returns n! for a small n.
func factorial(n int) int {
	out := 1
	for i := 2; i <= n; i++ {
		out *= i
	}
	return out
}

// candidateConstraints enumerates the constraints the intended answer satisfies.
//
// The set is deliberately broad and the greedy step decides what to keep: a
// generator that only emitted constraints it already knew were useful would be
// harder to reason about, and the solver is the authority either way.
func candidateConstraints(deck []string, n int, seed string) []constraint {
	out := make([]constraint, 0, n*n*2)
	add := func(kind, text string, cards []string, position int) {
		out = append(out, constraint{
			ConstraintID: fmt.Sprintf("cc-%03d", len(out)+1),
			Kind:         kind,
			Text:         text,
			Cards:        cards,
			Position:     position,
		})
	}

	// Position clues: the strongest and the easiest to read.
	for slot := 1; slot <= n; slot++ {
		card := deck[slot-1]
		add(KindPosition, fmt.Sprintf("%s 在第 %d 位", card, slot), []string{card}, slot)
	}

	// Ordering clues between every pair, in the direction the answer satisfies.
	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			a, b := deck[i], deck[j]
			add(KindBefore, fmt.Sprintf("%s 排在 %s 前面", a, b), []string{a, b}, 0)
		}
	}

	// Adjacency clues for the neighbours in the answer.
	for i := 0; i+1 < n; i++ {
		a, b := deck[i], deck[i+1]
		add(KindAdjacent, fmt.Sprintf("%s 与 %s 相邻", a, b), []string{a, b}, 0)
	}

	// Non-adjacency clues between pairs that are not neighbours. These are the
	// weakest clues, which is why the greedy step will usually drop most of them.
	for i := 0; i < n; i++ {
		for j := i + 2; j < n; j++ {
			a, b := deck[i], deck[j]
			add(KindNotWith, fmt.Sprintf("%s 与 %s 不相邻", a, b), []string{a, b}, 0)
		}
	}

	// Group clues over a sliding window of three cards, so a clue can describe a
	// block rather than only a pair.
	for i := 0; i+3 <= n; i++ {
		window := []string{deck[i], deck[i+1], deck[i+2]}
		add(KindGroup, fmt.Sprintf("%s、%s、%s 连续排在一起",
			window[0], window[1], window[2]), window, 0)
	}

	// Shuffle so the greedy step is not biased toward position clues first: a
	// puzzle made only of position clues would hand the answer over outright.
	stream := modeutil.NewStream(seed + "\x00cluepuzzle.candidates")
	stream.Shuffle(len(out), func(i, j int) { out[i], out[j] = out[j], out[i] })
	return out
}

// distribute assigns the kept constraints to participants.
//
// The rule : every participant holds at least one clue that no other
// participant holds, so the team must actually talk. The allocation is round-robin
// over the kept constraints after a seeded shuffle, which guarantees a private
// clue for each participant as long as there are at least as many constraints as
// participants.
//
// A share of the constraints is also published in the public area, so the team has
// a shared starting point. Those are the weakest constraints: publishing a position
// clue would defeat the puzzle.
func distribute(constraints []constraint, cfg Config, seed string) (map[string][]string, []string, string, error) {
	participants := cfg.Participants
	if len(constraints) < participants {
		return nil, nil, "", fmt.Errorf(
			"cluepuzzle: %d constraints cannot give %d participants a private clue",
			len(constraints), participants)
	}

	// Publish a bounded share of the constraints as the shared area.
	//
	// The cap exists because every participant must keep at least one clue nobody else has,
	// otherwise the round would not require communication . But capping at
	// len(constraints)-participants has a degenerate outcome on small boards: a 4-card board
	// with 3 participants yields exactly 3 constraints, so the cap is 0 and the shared area is
	// ALWAYS empty. A player then sees a board with no common starting point at all, which is
	// not what the shared area is for.
	//
	// The fix keeps the privacy guarantee and still gives the team something to start from: the
	// cap becomes len(constraints)-participants when that is positive, and 0 only when there are
	// literally no spare constraints AND no participant could hold a private clue. Since
	// distribute() already refuses a constraint set smaller than the participant count, the
	// shared area is allowed to take one constraint when there is exactly one spare... and when
	// there is none, the shared area stays empty and the round is purely private. That is the
	// honest outcome; pretending otherwise would either drop a participant's private clue or
	// publish a constraint nobody could act on.
	sharedCount := cfg.SharedCount
	maxShared := len(constraints) - participants
	if maxShared < 0 {
		maxShared = 0
	}
	if sharedCount > maxShared {
		sharedCount = maxShared
	}
	if sharedCount < 0 {
		sharedCount = 0
	}
	// Record why the shared area is empty, so a caller can tell "no spare constraints" apart
	// from "the generator forgot to fill it".
	sharedCapReason := ""
	if sharedCount == 0 && cfg.SharedCount > 0 {
		sharedCapReason = fmt.Sprintf(
			"本局共 %d 条线索、%d 名队员，没有多余线索可以公开，因此共享区为空：所有线索都作为私有线索发放",
			len(constraints), participants)
	}

	// Prefer to share the weakest clues: not_with and group constraints reveal
	// less than an exact position. The sort is stable and keyed on a strength
	// rank, so the result is deterministic.
	ordered := make([]constraint, len(constraints))
	copy(ordered, constraints)
	sort.SliceStable(ordered, func(i, j int) bool {
		if constraintStrength(ordered[i]) != constraintStrength(ordered[j]) {
			return constraintStrength(ordered[i]) < constraintStrength(ordered[j])
		}
		return ordered[i].ConstraintID < ordered[j].ConstraintID
	})

	shared := make([]string, 0, sharedCount)
	sharedSet := make(map[string]struct{}, sharedCount)
	for i := 0; i < sharedCount; i++ {
		shared = append(shared, ordered[i].ConstraintID)
		sharedSet[ordered[i].ConstraintID] = struct{}{}
	}
	sort.Strings(shared)

	// The remaining constraints are dealt round-robin, in a seeded order so the
	// allocation differs per round but is reproducible.
	remaining := make([]constraint, 0, len(constraints)-sharedCount)
	for _, c := range constraints {
		if _, isShared := sharedSet[c.ConstraintID]; isShared {
			continue
		}
		remaining = append(remaining, c)
	}
	stream := modeutil.NewStream(seed + "\x00cluepuzzle.deal")
	stream.Shuffle(len(remaining), func(i, j int) { remaining[i], remaining[j] = remaining[j], remaining[i] })

	private := make(map[string][]string, participants)
	for i := 0; i < participants; i++ {
		private[participantID(i)] = []string{}
	}
	for i, c := range remaining {
		owner := participantID(i % participants)
		private[owner] = append(private[owner], c.ConstraintID)
	}
	// The guarantee: every participant holds at least one clue nobody else holds.
	for i := 0; i < participants; i++ {
		id := participantID(i)
		if len(private[id]) == 0 {
			return nil, nil, "", fmt.Errorf(
				"cluepuzzle: participant %d received no private clue", i+1)
		}
		sort.Strings(private[id])
	}
	return private, shared, sharedCapReason, nil
}

// constraintStrength ranks how much a constraint reveals. A lower number is a
// weaker clue, which is the one to publish.
func constraintStrength(c constraint) int {
	switch c.Kind {
	case KindNotWith:
		return 0
	case KindGroup:
		return 1
	case KindAdjacent:
		return 2
	case KindBefore:
		return 3
	case KindPosition:
		return 4
	}
	return 5
}

// participantID is the stable id of the i-th participant slot.
func participantID(i int) string {
	return fmt.Sprintf("seat-%d", i+1)
}
