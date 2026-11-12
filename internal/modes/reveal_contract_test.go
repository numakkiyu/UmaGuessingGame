package modes_test

// Source-level checks detect omitted reveal fields across Go projections and TypeScript adapters.
// They do not validate the full runtime payload shape.

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// revealSource describes where a mode's reveal field lives in Go and which
// frontend file is expected to read it.
type revealSource struct {
	mode     string
	goFile   string
	field    string // the JSON name: "answer" or "reveal"
	feFile   string
	excluded string // non-empty: why the frontend need not read it yet
	// normMarker is a substring that must appear in the frontend file, proving
	// the field is actually READ from the payload rather than merely declared in
	// a type. A type-only declaration compiles and looks right while the value
	// stays undefined forever — that is exactly how the classic shadow region
	// stayed missing.
	normMarker string
}

func TestRevealFieldsAreReadByTheFrontend(t *testing.T) {
	cases := []revealSource{
		{mode: "cluepuzzle", goFile: "internal/modes/cluepuzzle/mode.go", field: "answer", feFile: "web/src/lib/api/modes-g09-g12.ts"},
		{mode: "pedigree", goFile: "internal/modes/pedigree/mode.go", field: "answer", feFile: "web/src/lib/api/pedigree.ts"},
		// rapport's reveal hangs off questionView, not the projection — the
		// frontend must therefore look inside each question.
		{mode: "rapport", goFile: "internal/modes/rapport/mode.go", field: "reveal", feFile: "web/src/lib/api/modes-g09-g12.ts"},
		{mode: "sort", goFile: "internal/modes/sort/mode.go", field: "answer", feFile: "web/src/lib/api/sort.ts"},
		{mode: "coracepath", goFile: "internal/modes/coracepath/mode.go", field: "answer", feFile: "web/src/lib/api/co-race-path.ts"},
		{mode: "memory", goFile: "internal/modes/memory/mode.go", field: "answer", feFile: "web/src/lib/api/memory.ts"},
		{mode: "racerebuild", goFile: "internal/modes/racerebuild/mode.go", field: "answer", feFile: "web/src/lib/api/race-rebuild.ts"},
		// classic's Answer is the secret character itself; its reveal is the
		// nine-field feedback on every guess, not a separate panel.
		{mode: "classic", goFile: "internal/modes/classic/mode.go", field: "answer", excluded: "reveal is the nine-field feedback, not a separate payload"},
		{mode: "audio", goFile: "internal/modes/audio/mode.go", field: "answer", excluded: "mode unavailable: no_local_audio_file"},
		{mode: "bluff", goFile: "internal/modes/bluff/mode.go", field: "reveal", excluded: "mode unavailable: unreviewed_fact_not_publishable"},
		// classic publishes TWO optional fields with different audiences:
		// answer — the secret character; the frontend reads it only at reveal,
		// and the normal guess flow uses the nine-field feedback.
		// shadows — opponent progress; requires its own region, so the
		// frontend MUST read it. Exempting classic as a whole for
		// `answer` silently skipped `shadows`, which is how the
		// shadow region went missing for seven rounds.
		// Exemptions are per FIELD, never per mode.
		{mode: "classic", goFile: "internal/modes/classic/mode.go", field: "shadows", feFile: "web/src/lib/api/classic.ts", normMarker: `publicState?.["shadows"]`},
	}

	// The repository root is two levels up from internal/modes.
	root := filepath.Join("..", "..")

	for _, tc := range cases {
		tc := tc
		t.Run(tc.mode, func(t *testing.T) {
			goSrc, err := os.ReadFile(filepath.Join(root, tc.goFile))
			if err != nil {
				t.Fatalf("read %s: %v", tc.goFile, err)
			}

			// Does the Go side declare this reveal at all?
			declPattern := regexp.MustCompile(`json:"` + tc.field + `,omitempty"`)
			if !declPattern.Match(goSrc) {
				// The mode genuinely has no reveal. Nothing to check.
				if tc.excluded != "" {
					t.Logf("%s: no %q field and excluded (%s) — consistent", tc.mode, tc.field, tc.excluded)
					return
				}
				t.Fatalf("%s: expected a `json:%q,omitempty` reveal field in %s, found none.\n"+
					"If the mode stopped publishing a reveal, remove it from this test so the intent stays explicit.",
					tc.mode, tc.field, tc.goFile)
			}

			if tc.excluded != "" {
				// A reveal exists but the frontend is not required to read it.
				// Log the reason so the exemption is visible, never silent.
				t.Logf("%s: publishes %q but frontend read is waived — %s", tc.mode, tc.field, tc.excluded)
				return
			}

			feSrc, err := os.ReadFile(filepath.Join(root, tc.feFile))
			if err != nil {
				t.Fatalf("read %s: %v", tc.feFile, err)
			}

			// The frontend must mention the field name. This is deliberately a
			// loose check: it catches "nobody wired the field at all", which is
			// exactly the defect that shipped four times. Mapping the shape
			// correctly still needs a human (or a browser test).
			if !strings.Contains(string(feSrc), tc.field) {
				t.Fatalf("%s publishes `%s` in %s, but %s never mentions %q.\n"+
					"The reveal panel will render empty while every other check passes.\n"+
					"Add the field to the frontend type and read it in normalize*, or list the mode as excluded with a reason.",
					tc.mode, tc.field, tc.goFile, tc.feFile, tc.field)
			}

			// A type declaration alone is not enough. The field must be read from
			// the payload, or the value stays undefined forever while the type
			// says it exists.
			if tc.normMarker != "" && !strings.Contains(string(feSrc), tc.normMarker) {
				t.Fatalf("%s: %s mentions %q but never reads it from the payload.\n"+
					"Expected to find %q in the normalizer.\n"+
					"Without that read the field is permanently undefined and the region renders empty.",
					tc.mode, tc.feFile, tc.field, tc.normMarker)
			}
		})
	}
}

// TestRevealFieldNamesMatchTheFrontendGuards pins down the second half of the
// same defect class: the frontend can read the right *field name* at the wrong
// *level*.
//
// rapport shipped this. The frontend added `reveal` to the round type, but the
// server puts it on each question. Every check passed; the panel was empty.
//
// This test asserts the level explicitly, by requiring the Go field to sit
// inside the struct the frontend is expected to read.
func TestRevealFieldLevelsAreDocumented(t *testing.T) {
	root := filepath.Join("..", "..")

	// Each entry: mode → the struct that must contain the reveal field, and the
	// frontend file whose type must mirror that level.
	levels := []struct {
		mode      string
		goFile    string
		structNam string
		field     string
		feFile    string
		feType    string // the frontend type that must carry the field
	}{
		{
			mode: "rapport", goFile: "internal/modes/rapport/mode.go",
			structNam: "questionView", field: "reveal",
			feFile: "web/src/lib/api/modes-g09-g12.ts", feType: "RapportQuestion",
		},
		{
			mode: "cluepuzzle", goFile: "internal/modes/cluepuzzle/mode.go",
			structNam: "projection", field: "answer",
			feFile: "web/src/lib/api/modes-g09-g12.ts", feType: "CluePuzzleRound",
		},
		{
			mode: "pedigree", goFile: "internal/modes/pedigree/mode.go",
			structNam: "projection", field: "answer",
			feFile: "web/src/lib/api/pedigree.ts", feType: "PedigreeRound",
		},
		{
			mode: "sort", goFile: "internal/modes/sort/mode.go",
			structNam: "projection", field: "answer",
			feFile: "web/src/lib/api/sort.ts", feType: "SortRound",
		},
	}

	structPattern := func(name string) *regexp.Regexp {
		return regexp.MustCompile(`type ` + name + ` struct \{(?s:.*?)\n\}`)
	}

	for _, tc := range levels {
		tc := tc
		t.Run(tc.mode+"/"+tc.structNam, func(t *testing.T) {
			goSrc, err := os.ReadFile(filepath.Join(root, tc.goFile))
			if err != nil {
				t.Fatalf("read %s: %v", tc.goFile, err)
			}
			block := structPattern(tc.structNam).Find(goSrc)
			if block == nil {
				t.Fatalf("%s: struct %s not found in %s", tc.mode, tc.structNam, tc.goFile)
			}
			if !strings.Contains(string(block), `json:"`+tc.field+`,omitempty"`) {
				t.Fatalf("%s: struct %s does not declare json:%q.\n"+
					"If the reveal moved, update this table — the frontend type must move with it.",
					tc.mode, tc.structNam, tc.field)
			}

			// The frontend type must carry the field, not just the file.
			feSrc, err := os.ReadFile(filepath.Join(root, tc.feFile))
			if err != nil {
				t.Fatalf("read %s: %v", tc.feFile, err)
			}
			typeBlock := regexp.MustCompile(`export type ` + tc.feType + ` = \{(?s:.*?)\n\};`).Find(feSrc)
			if typeBlock == nil {
				t.Fatalf("%s: type %s not found in %s", tc.mode, tc.feType, tc.feFile)
			}
			if !strings.Contains(string(typeBlock), tc.field+"?:") {
				t.Fatalf("%s: type %s does not declare an optional %q field.\n"+
					"The server puts %q on %s; the frontend type must mirror that level.",
					tc.mode, tc.feType, tc.field, tc.field, tc.structNam)
			}
		})
	}
}
