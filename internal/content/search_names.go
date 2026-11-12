package content

import (
	"strings"
	"unicode/utf8"

	"github.com/bhcn/umaguessinggame/internal/characternames"
)

// Only explicit name slots and the legacy community-alias source are used.
// nicknameJp is an outfit title, not a community nickname.
func searchNamesFromSources(w *RawWikiCharacter, legacy *legacyEntry) *characternames.Names {
	n := &characternames.Names{English: strings.TrimSpace(w.Names.En)}
	if w.Names.Ja != "" && namesFromWiki(w).JaConfidence == ConfidenceConfirmed {
		n.Japanese = strings.TrimSpace(w.Names.Ja)
	}
	seen := map[string]bool{}
	add := func(target *[]string, value string) {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] || utf8.RuneCountInString(value) > 160 || len(*target) >= 40 {
			return
		}
		seen[value] = true
		*target = append(*target, value)
	}
	for _, name := range append([]string{w.Names.ZhHans, w.Names.ZhHant, w.Names.GameZhHans, w.Names.GameZhHant, w.Names.MoegirlZh}, w.Names.ZhAll...) {
		add(&n.Translations, name)
	}
	seen[w.Names.Ja], seen[w.Names.En], seen[w.Names.NicknameJp] = true, true, true
	if legacy != nil && legacy.norm != nil {
		for _, nickname := range legacy.norm.Raw.MoegirlDetail.Aliases {
			add(&n.Nicknames, nickname)
		}
	}
	return n
}
