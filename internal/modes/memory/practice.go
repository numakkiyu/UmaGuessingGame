package memory

import (
	"fmt"
	"regexp"

	"github.com/bhcn/umaguessinggame/internal/modes"
	"github.com/bhcn/umaguessinggame/internal/modes/modeutil"
)

// PracticeFace is a library image, never a prepared multiplayer board or mapping.
type PracticeFace struct {
	PairID    string `json:"pairId"`
	ImagePath string `json:"imagePath"`
}

var practiceFacePath = regexp.MustCompile(`^/assets/faces/[A-Za-z0-9_-]+\.(webp|png)$`)

// PracticeFaces deliberately reads only the public self-describing pair library.
// Split/private pair libraries require a separate reviewed adapter, not a secret export.
func PracticeFaces(src modes.ContentSource) ([]PracticeFace, error) {
	raw, err := modeutil.Section(src, "memory.pairs", "memory")
	if err != nil {
		return nil, err
	}
	records, err := modeutil.Records(raw, "pairs", "items")
	if err != nil {
		return nil, err
	}
	out := make([]PracticeFace, 0, len(records))
	ids, images := map[string]bool{}, map[string]bool{}
	for _, record := range records {
		id, image := record.Str("pairId"), record.Str("imagePath")
		if id == "" || len(id) > 100 || ids[id] || images[image] || !practiceFacePath.MatchString(image) {
			continue
		}
		ids[id], images[image] = true, true
		out = append(out, PracticeFace{id, image})
	}
	if len(out) < PairCount {
		return nil, fmt.Errorf("memory: fewer than eight public practice faces")
	}
	return out, nil
}
