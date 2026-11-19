package pedigree

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/bhcn/umaguessinggame/internal/modes"
)

type PracticeFamily struct {
	Subject   prototypeSubject  `json:"subject"`
	Relations map[string]string `json:"relations"`
}
type PracticeLibrary struct {
	Disclaimer string           `json:"disclaimer"`
	Slots      []slotDef        `json:"slots"`
	Horses     []candidate      `json:"horses"`
	Families   []PracticeFamily `json:"families"`
}

// identityCounts is shared by independent preparation and room draws. Conflicting
// registered names or identifiers are excluded instead of choosing a first match.
func identityCounts(entries []entry) (map[string]int, map[string]int, map[string]int) {
	ids, prototypes, names := map[string]int{}, map[string]int{}, map[string]int{}
	for _, e := range entries {
		ids[e.CharacterID]++
		prototypes[e.NKBID]++
		names[normalizeName(e.PrototypeNames.Ja)]++
	}
	return ids, prototypes, names
}
func confirmedIdentity(e entry, ids, prototypes, names map[string]int) bool {
	return e.CharacterID != "" && strings.TrimSpace(e.Name) != "" && e.NKBID != "" && e.PrototypeNames.JaConfidence == "confirmed" && normalizeName(e.PrototypeNames.Ja) != "" && ids[e.CharacterID] == 1 && prototypes[e.NKBID] == 1 && names[normalizeName(e.PrototypeNames.Ja)] == 1
}

// PracticeFacts exports explicit historical facts, never a live room question or
// state. Names are compared exactly after the same country/space normalization
// used by the room mode. No shared-prefix or character-family inference is made.
func PracticeFacts(src modes.ContentSource) (PracticeLibrary, error) {
	entries, err := loadEntries(src)
	if err != nil {
		return PracticeLibrary{}, err
	}
	ids, prototypes, names := identityCounts(entries)
	out := PracticeLibrary{Disclaimer: Disclaimer, Slots: slotDefs, Horses: []candidate{}, Families: []PracticeFamily{}}
	horses := map[string]candidate{}
	for _, e := range entries {
		name := strings.TrimSpace(e.PrototypeNames.Ja)
		if !confirmedIdentity(e, ids, prototypes, names) || !e.complete() {
			continue
		}
		relations, seen := map[string]string{}, map[string]bool{}
		valid := true
		for _, slot := range slotDefs {
			norm := normalizeName(e.Pedigree[slot.Field])
			if seen[norm] || norm == normalizeName(name) {
				valid = false
				break
			}
			seen[norm] = true
			sum := sha256.Sum256([]byte("pedigree.fact\x00" + norm))
			id := "horse-" + hex.EncodeToString(sum[:16])
			relations[slot.SlotID] = id
		}
		if !valid {
			continue
		}
		for _, slot := range slotDefs {
			raw := strings.TrimSpace(e.Pedigree[slot.Field])
			norm := normalizeName(raw)
			id := relations[slot.SlotID]
			if _, ok := horses[id]; !ok {
				// Attach a prototype link only if the exact registered name identifies one
				// confirmed entity. Unknown ancestors keep their published name without a
				// fabricated identity or link.
				nkb := ""
				for _, other := range entries {
					if other.PrototypeNames.JaConfidence == "confirmed" && normalizeName(other.PrototypeNames.Ja) == norm && names[norm] == 1 && prototypes[other.NKBID] == 1 {
						nkb = other.NKBID
					}
				}
				horses[id] = candidate{HorseID: id, Name: raw, NKBID: nkb}
			}
		}
		out.Families = append(out.Families, PracticeFamily{Subject: prototypeSubject{CharacterID: e.CharacterID, CharacterName: e.Name, PrototypeName: name, NKBID: e.NKBID}, Relations: relations})
	}
	for _, horse := range horses {
		out.Horses = append(out.Horses, horse)
	}
	sort.Slice(out.Horses, func(i, j int) bool { return out.Horses[i].HorseID < out.Horses[j].HorseID })
	if len(out.Families) == 0 || len(out.Horses) < DefaultCandidates {
		return PracticeLibrary{}, fmt.Errorf("pedigree: insufficient unambiguous practice facts")
	}
	return out, nil
}
