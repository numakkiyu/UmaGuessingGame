// Package characternames holds public, source-backed search display metadata.
package characternames

// Names keeps translations and community nicknames distinct from outfit titles.
// Empty fields are omitted; unknown names are never inferred from script alone.
type Names struct {
	Translations []string `json:"translations,omitempty"`
	Japanese     string   `json:"japanese,omitempty"`
	English      string   `json:"english,omitempty"`
	Nicknames    []string `json:"nicknames,omitempty"`
}
