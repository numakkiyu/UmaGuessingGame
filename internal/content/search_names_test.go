package content

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

func TestSearchNamesSeparateTranslationsCommunityAliasesAndOutfitTitles(t *testing.T) {
	var source RawNormalizedCharacter
	raw, err := os.ReadFile("../../data/normalized/characters/001_Special_Week.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &source); err != nil {
		t.Fatal(err)
	}
	w := &RawWikiCharacter{}
	w.Names.ZhHans, w.Names.ZhHant = "特别周", "特別週"
	w.Names.Ja, w.Names.En = "スペシャルウィーク", "Special Week"
	w.Names.NicknameJp = "スペシャルドリーマー"
	w.Names.ZhAll = []string{"特别周", "特別週"}
	n := searchNamesFromSources(w, &legacyEntry{norm: &source})
	if !reflect.DeepEqual(n.Translations, []string{"特别周", "特別週"}) || n.Japanese != w.Names.Ja || n.English != w.Names.En {
		t.Fatal(n)
	}
	if len(n.Nicknames) == 0 || n.Nicknames[0] != "小特" {
		t.Fatal("community aliases lost", n)
	}
	for _, nickname := range n.Nicknames {
		if nickname == w.Names.ZhHans || nickname == w.Names.NicknameJp {
			t.Fatal("canonical name or outfit title mislabelled", n)
		}
	}
	w.Names.JaConfidence = string(ConfidenceInferred)
	if got := searchNamesFromSources(w, nil); got.Japanese != "" || len(got.Nicknames) != 0 {
		t.Fatal("inferred name or invented nickname published", got)
	}
}
