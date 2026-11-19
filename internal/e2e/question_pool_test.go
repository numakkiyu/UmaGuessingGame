package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/bhcn/umaguessinggame/internal/modes"
	"github.com/bhcn/umaguessinggame/internal/modes/modeutil"
	"github.com/bhcn/umaguessinggame/internal/wiring"
)

func poolObject(t *testing.T, raw json.RawMessage) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}
func poolList(t *testing.T, object map[string]any, key string) []any {
	t.Helper()
	list, ok := object[key].([]any)
	if !ok || len(list) == 0 {
		t.Fatalf("missing nonempty %s", key)
	}
	return list
}
func poolIDs(t *testing.T, list []any, key string) map[string]bool {
	t.Helper()
	ids := map[string]bool{}
	for _, item := range list {
		id, _ := item.(map[string]any)[key].(string)
		if id == "" || ids[id] {
			t.Fatalf("empty or duplicate %s %q", key, id)
		}
		ids[id] = true
	}
	return ids
}

// TestPublishedQuestionPools binds the actual compiled dataset to every registered
// engine. Synthetic engine fixtures alone cannot catch a compiler/consumer mismatch.
func TestPublishedQuestionPools(t *testing.T) {
	root := repoRoot(t)
	if _, err := os.Stat(filepath.Join(root, "content", "current")); err != nil {
		t.Skip("no compiled content pack")
	}
	src, err := wiring.OpenContent(context.Background(), filepath.Join(root, "content"))
	if err != nil {
		t.Fatal(err)
	}
	set, err := wiring.BuildModes()
	if err != nil {
		t.Fatal(err)
	}
	type configCase struct{ mode, config, roster string }
	var cases []configCase
	for _, m := range set.Registry.All() {
		cases = append(cases, configCase{m.ID(), `{}`, "default"})
	}
	cases = append(cases,
		configCase{"sort", `{"key":"birthday"}`, "default"},
		configCase{"sort", `{"key":"eventDate","eventRegion":"JP"}`, "default"},
		configCase{"sort", `{"cardCount":2}`, "default"},
		configCase{"aptitude-grid", `{"rowAxis":"style"}`, "default"},
		configCase{"pedigree", `{"slotCount":6,"candidateCount":12}`, "default"},
		configCase{"clue-puzzle", `{"cards":5}`, "teams"},
		configCase{"rapport", `{"simultaneousTurns":true}`, "party"},
		configCase{"rapport", `{"simultaneousTurns":true}`, "teams"},
		configCase{"rapport", `{"simultaneousTurns":true}`, "full-event"},
		configCase{"trade", `{}`, "large"},
		configCase{"creative", `{}`, "large"},
		configCase{"draft", `{}`, "large"},
	)
	for _, c := range cases {
		t.Run(c.mode+"/"+c.config+"/"+c.roster, func(t *testing.T) {
			m, _ := set.Registry.Get(c.mode)
			n := m.MinSubjects()
			if c.roster == "large" {
				n = m.MaxSubjects()
			}
			if c.mode == "strategy-race" {
				n = 2
			}
			if c.mode == "clue-puzzle" {
				n = 3
			}
			var subjects []modes.Subject
			var participants []modes.Participant
			if c.roster == "teams" && c.mode == "clue-puzzle" {
				for team, size := range []int{3, 5} {
					subject := modes.Subject{ID: fmt.Sprintf("t%d", team), Kind: modes.SubjectTeam}
					subjects = append(subjects, subject)
					for i := 0; i < size; i++ {
						participants = append(participants, modes.Participant{MembershipID: fmt.Sprintf("t%d-p%d", team, i), Subject: subject})
					}
				}
			} else if c.roster != "default" && c.roster != "large" {
				sizes := []int{4}
				if c.roster == "teams" {
					sizes = []int{3, 4}
				}
				if c.roster == "full-event" {
					sizes = []int{8, 8, 8, 8, 8, 8, 8, 8}
				}
				for team, size := range sizes {
					for i := 0; i < size; i++ {
						id := fmt.Sprintf("t%d-p%d", team, i)
						subject := modes.Subject{ID: id, Kind: modes.SubjectPlayer}
						subjects = append(subjects, subject)
						p := modes.Participant{MembershipID: id, Subject: subject}
						if c.roster == "teams" || c.roster == "full-event" {
							p.TeamID = fmt.Sprintf("t%d", team)
						}
						participants = append(participants, p)
					}
				}
			} else {
				for i := 0; i < n; i++ {
					subjects = append(subjects, modes.Subject{ID: fmt.Sprintf("p%d", i), Kind: modes.SubjectPlayer})
				}
			}
			admission, ok := src.ModeAdmission(c.mode)
			for draw := 0; draw < 32; draw++ {
				scope := modes.Scope{ModeID: c.mode, RulesVersion: m.RulesVersion(), DatasetVersion: src.DatasetVersion(), ContentPackVersion: src.Version(), Seed: fmt.Sprintf("published-pool-%d", draw)}
				prep, err := m.Prepare(context.Background(), modes.PrepareInput{Scope: scope, Content: src, Subject: subjects, Participants: participants, Config: json.RawMessage(c.config)})
				if !ok || !admission.Available {
					if err == nil {
						t.Fatal("unavailable content could still prepare a round")
					}
					break
				}
				if err != nil {
					t.Fatalf("seed %d: %v", draw, err)
				}
				membership := subjects[0].ID
				if len(participants) > 0 {
					membership = participants[0].MembershipID
				}
				viewer := modes.Viewer{MembershipID: membership, Subject: subjects[0], Role: "player", CanControl: true}
				for _, phase := range []modes.Phase{modes.PhaseCollecting, modes.PhaseRevealed} {
					projection, err := m.Project(modes.ProjectInput{Scope: scope, Public: prep.Public, Private: prep.Private, States: prep.SubjectPrivate, Phase: phase, Viewer: viewer})
					if err != nil || !json.Valid(projection) {
						t.Fatalf("projection %s failed: %v", phase, err)
					}
				}
				exercisePool(t, m, scope, prep, subjects[0], membership, c.roster == "default" || c.roster == "large" || c.mode == "clue-puzzle")
			}
		})
	}
}

func exercisePool(t *testing.T, m modes.Mode, scope modes.Scope, prep modes.Prepared, subject modes.Subject, membership string, solve bool) {
	t.Helper()
	pub := poolObject(t, prep.Public)
	priv := poolObject(t, prep.Private)
	state := prep.SubjectPrivate[subject.ID]
	count := 0
	perfect := false
	apply := func(kind string, payload map[string]any) {
		t.Helper()
		payload["kind"] = kind
		raw, _ := json.Marshal(payload)
		input := modes.ApplyInput{Scope: scope, Subject: subject, MembershipID: membership, Public: prep.Public, Private: prep.Private, State: state, SubmissionCount: count, ReceivedAt: 100000 + int64(count)*2000, Action: modes.ModeAction{Kind: kind, Payload: raw}}
		decision, err := m.Apply(input)
		if err != nil || !decision.Accepted {
			t.Fatalf("published option rejected (%s, seed %s): %s %v", kind, scope.Seed, decision.Code, err)
		}
		if shared, ok := m.(modeutil.SharedStateMode); ok {
			prep.Public, prep.Private, err = shared.CommitShared(input, decision)
			if err != nil {
				t.Fatal(err)
			}
		}
		state = decision.State
		count++
	}
	switch m.ID() {
	case "aptitude-grid":
		round := pub["round"].(map[string]any)
		ids := poolIDs(t, poolList(t, round, "pool"), "id")
		cards := poolList(t, priv, "cards")
		cells := poolList(t, round, "cells")
		candidates := make([][]string, len(cells))
		for i, item := range cells {
			cell := item.(map[string]any)
			row, col := cell["rowCondition"].(map[string]any), cell["colCondition"].(map[string]any)
			for _, raw := range cards {
				card := raw.(map[string]any)
				id := card["id"].(string)
				if !ids[id] {
					t.Fatal("grid grading card absent from pool")
				}
				apt := card["aptitudes"].(map[string]any)
				if apt[row["axis"].(string)].(map[string]any)[row["value"].(string)] == "A" && apt[col["axis"].(string)].(map[string]any)[col["value"].(string)] == "A" {
					candidates[i] = append(candidates[i], id)
				}
			}
		}
		owners := map[string]int{}
		solution := make([]string, len(cells))
		var assign func(int, map[string]bool) bool
		assign = func(i int, seen map[string]bool) bool {
			for _, id := range candidates[i] {
				if seen[id] {
					continue
				}
				seen[id] = true
				previous, used := owners[id]
				if !used || assign(previous, seen) {
					owners[id] = i
					solution[i] = id
					return true
				}
			}
			return false
		}
		for i := range cells {
			if !assign(i, map[string]bool{}) {
				t.Fatal("published grid has no distinct-card solution")
			}
		}
		if solve {
			for i, id := range solution {
				apply("grid.claim", map[string]any{"cellId": cells[i].(map[string]any)["cellId"], "characterId": id})
			}
			perfect = true
		}
	case "classic":
		ids := poolIDs(t, poolList(t, pub, "pool"), "id")
		answer := priv["answerId"].(string)
		if !ids[answer] {
			t.Fatal("classic answer missing from published pool")
		}
		for id := range ids {
			raw, _ := json.Marshal(map[string]any{"kind": "classic.guess", "characterId": id})
			decision, err := m.Apply(modes.ApplyInput{Scope: scope, Subject: subject, Public: prep.Public, Private: prep.Private, Action: modes.ModeAction{Kind: "classic.guess", Payload: raw}})
			if err != nil || !decision.Accepted {
				t.Fatalf("classic published candidate %s rejected: %s %v", id, decision.Code, err)
			}
		}
		if solve {
			apply("classic.guess", map[string]any{"characterId": answer})
			perfect = true
		}
	case "sort", "race-rebuild":
		field, idKey, kind := "cards", "characterId", "sort.order"
		valuesKey := "values"
		if m.ID() == "race-rebuild" {
			field = "entries"
			kind = "race.order"
			valuesKey = "truth"
		}
		ids := poolIDs(t, poolList(t, pub, field), idKey)
		values := priv[valuesKey].(map[string]any)
		if len(ids) != len(values) {
			t.Fatal("ordering values disagree with published cards")
		}
		byValue := map[int][]string{}
		var keys []int
		for id, value := range values {
			if !ids[id] {
				t.Fatal("hidden ordering card absent from candidates")
			}
			n := int(value.(float64))
			if _, ok := byValue[n]; !ok {
				keys = append(keys, n)
			}
			byValue[n] = append(byValue[n], id)
		}
		sort.Ints(keys)
		var groups [][]string
		for _, key := range keys {
			sort.Strings(byValue[key])
			groups = append(groups, byValue[key])
		}
		if solve {
			apply(kind, map[string]any{"groups": groups})
			perfect = true
		}
	case "co-race-path":
		ids := poolIDs(t, poolList(t, pub, "nodes"), "characterId")
		path := poolList(t, priv, "shortestPath")
		for _, id := range path {
			if !ids[id.(string)] {
				t.Fatal("route contains unavailable node")
			}
		}
		if solve {
			apply("path.route", map[string]any{"characterIds": path})
			perfect = true
		}
	case "pedigree", "clue-puzzle":
		field, key, kind := "candidates", "horseId", "pedigree.assignment"
		if m.ID() == "clue-puzzle" {
			field = "cards"
			key = "cardId"
			kind = "clue.assignment"
		}
		ids := poolIDs(t, poolList(t, pub, field), key)
		solution := priv["solution"].(map[string]any)
		var assignments []map[string]any
		for slot, id := range solution {
			if !ids[id.(string)] {
				t.Fatal("solution contains unavailable candidate")
			}
			a := map[string]any{"slotId": slot, "horseId": id}
			if m.ID() == "clue-puzzle" {
				a = map[string]any{"slotId": slot, "characterId": id}
			}
			assignments = append(assignments, a)
		}
		if solve {
			apply(kind, map[string]any{"assignments": assignments})
			perfect = true
		}
	case "memory":
		ids := poolIDs(t, poolList(t, pub, "cards"), "cardId")
		pairs := map[string][]string{}
		for id, pair := range priv["pairOf"].(map[string]any) {
			if !ids[id] {
				t.Fatal("memory solution contains unknown card")
			}
			pairs[pair.(string)] = append(pairs[pair.(string)], id)
		}
		if len(ids) != 16 || len(pairs) != 8 {
			t.Fatal("invalid memory board")
		}
		for _, pair := range pairs {
			if len(pair) != 2 {
				t.Fatal("memory pair incomplete")
			}
			if solve {
				for _, id := range pair {
					apply("memory.flip", map[string]any{"cardId": id})
				}
			}
		}
		perfect = solve
	case "draft":
		ids := poolIDs(t, poolList(t, pub, "cards"), "id")
		tasks := poolList(t, pub, "tasks")
		lineups := poolList(t, priv, "referenceLineups")
		if len(lineups) < 2 {
			t.Fatal("market has fewer than two feasible lineups")
		}
		lineup := lineups[0].([]any)
		var assignments []map[string]any
		for i, id := range lineup {
			if !ids[id.(string)] {
				t.Fatal("lineup contains unavailable card")
			}
			assignments = append(assignments, map[string]any{"slotId": tasks[i].(map[string]any)["taskId"], "characterId": id})
		}
		if solve {
			apply("draft.lineup", map[string]any{"assignments": assignments})
		}
	}
	if count == 0 {
		return
	}
	states := map[string]json.RawMessage{}
	for id, raw := range prep.SubjectPrivate {
		states[id] = raw
	}
	if len(state) > 0 {
		states[subject.ID] = state
	}
	scores, err := m.Settle(modes.SettleInput{Scope: scope, Public: prep.Public, Private: prep.Private, States: states, Phase: modes.PhaseRevealed})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, score := range scores {
		if score.Subject.ID == subject.ID {
			found = true
		}
		if err := score.Validate(); err != nil {
			t.Fatal(err)
		}
		if perfect && score.Subject.ID == subject.ID && score.ScoreUnits != score.MaxScoreUnits {
			t.Fatalf("correct solution did not earn full score: %+v", score)
		}
	}
	if perfect && !found {
		t.Fatal("submitted solution has no scoring record")
	}
}
