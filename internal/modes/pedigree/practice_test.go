package pedigree

import (
	"context"
	"encoding/json"
	"github.com/bhcn/umaguessinggame/internal/modes"
	"os"
	"testing"
)

func TestSharedPracticeAndRoomAssignmentVectors(t *testing.T) {
	raw, err := os.ReadFile("../../../web/src/lib/domain/testdata/pedigree-assignments.json")
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		SlotIDs      []string          `json:"slotIds"`
		CandidateIDs []string          `json:"candidateIds"`
		Solution     map[string]string `json:"solution"`
		Cases        []struct {
			Name        string
			Assignments []assignmentRecord
			Accepted    bool
			ScoreUnits  int
		}
	}
	if err = json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	pub := publicState{Subjects: []rosterEntry{{SubjectID: "p1", SubjectKind: modes.SubjectPlayer}}}
	for _, id := range f.SlotIDs {
		pub.Slots = append(pub.Slots, puzzleSlot{SlotID: id})
	}
	for _, id := range f.CandidateIDs {
		pub.Candidates = append(pub.Candidates, candidate{HorseID: id})
	}
	public, _ := json.Marshal(pub)
	private, _ := json.Marshal(privateState{Solution: f.Solution})
	for _, tc := range f.Cases {
		t.Run(tc.Name, func(t *testing.T) {
			pairs := []string{}
			for _, a := range tc.Assignments {
				pairs = append(pairs, a.SlotID, a.HorseID)
			}
			dec, err := New().Apply(modes.ApplyInput{Subject: modes.Subject{ID: "p1", Kind: modes.SubjectPlayer}, Public: public, Private: private, Action: assignAction(pairs...)})
			if err != nil || dec.Accepted != tc.Accepted {
				t.Fatalf("accepted=%v err=%v code=%s", dec.Accepted, err, dec.Code)
			}
			if tc.Accepted {
				scores, err := New().Settle(modes.SettleInput{Public: public, Private: private, States: map[string]json.RawMessage{"p1": dec.State}})
				if err != nil || len(scores) != 1 || int(scores[0].ScoreUnits) != tc.ScoreUnits {
					t.Fatalf("scores=%+v err=%v", scores, err)
				}
			}
		})
	}
}
func TestPracticeFactsRefuseAmbiguousOrUnconfirmedIdentities(t *testing.T) {
	a, b := buildEntry("1001", "A"), buildEntry("1002", "B")
	marshal := func(v []entrySpec) json.RawMessage {
		raw, _ := json.Marshal(v)
		var records []map[string]any
		_ = json.Unmarshal(raw, &records)
		for _, r := range records {
			r["prototypeNames"].(map[string]any)["jaConfidence"] = "confirmed"
		}
		raw, _ = json.Marshal(records)
		return raw
	}
	source := fakeContent{"pedigree.entries": marshal([]entrySpec{a, b})}
	lib, err := PracticeFacts(source)
	if err != nil || len(lib.Families) != 2 || len(lib.Horses) != 12 {
		t.Fatalf("library=%+v err=%v", lib, err)
	}
	// A common prefix never merges A-父 and A-父父.
	if lib.Families[0].Relations["sire"] == lib.Families[0].Relations["sireSire"] {
		t.Fatal("prefix falsely merged")
	}
	for _, name := range []string{"duplicate id", "duplicate prototype", "duplicate name", "duplicate ancestor", "self ancestor", "incomplete", "unconfirmed"} {
		t.Run(name, func(t *testing.T) {
			raw := marshal([]entrySpec{a, b})
			var rows []map[string]any
			_ = json.Unmarshal(raw, &rows)
			switch name {
			case "duplicate id":
				rows[1]["characterId"] = rows[0]["characterId"]
			case "duplicate prototype":
				rows[1]["nkbId"] = rows[0]["nkbId"]
			case "duplicate name":
				rows[1]["prototypeNames"].(map[string]any)["ja"] = rows[0]["prototypeNames"].(map[string]any)["ja"]
			case "duplicate ancestor":
				for _, r := range rows {
					ped := r["pedigree"].(map[string]any)
					ped["dam"] = ped["sire"]
				}
			case "self ancestor":
				for _, r := range rows {
					r["pedigree"].(map[string]any)["sire"] = r["prototypeNames"].(map[string]any)["ja"]
				}
			case "incomplete":
				for _, r := range rows {
					r["pedigree"].(map[string]any)["damDam"] = ""
				}
			case "unconfirmed":
				for _, r := range rows {
					r["prototypeNames"].(map[string]any)["jaConfidence"] = "inferred"
				}
			}
			raw, _ = json.Marshal(rows)
			if _, err := New().Prepare(context.Background(), modes.PrepareInput{Scope: scope("conflicting-identities"), Subject: []modes.Subject{{ID: "p1", Kind: modes.SubjectPlayer}}, Content: fakeContent{"pedigree.entries": raw}}); err == nil {
				t.Fatal("room admitted conflicting identity or missing relations")
			}
			if _, err := PracticeFacts(fakeContent{"pedigree.entries": raw}); err == nil {
				t.Fatal("ambiguous or incomplete facts admitted")
			}
		})
	}
}
