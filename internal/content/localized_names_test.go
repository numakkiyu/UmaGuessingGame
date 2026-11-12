package content

import (
	"reflect"
	"testing"
)

func TestPublicNamesUseConfirmedRegionalSources(t *testing.T) {
	ds := &Dataset{DatasetVersion: "fixed", Characters: []Character{
		{ID: "b", Names: Names{ZhHans: "待确认", Ja: "推測名", JaConfidence: ConfidenceInferred}},
		{ID: "a", Names: Names{ZhHans: "特别周", ZhHant: "特別週", Ja: "スペシャルウィーク", JaConfidence: ConfidenceConfirmed}},
	}}
	rows := buildPublicIndex(ds)
	if rows[0].ID != "a" || !reflect.DeepEqual(rows[0].LocalizedNames, map[string]string{"zh-Hans": "特别周", "zh-Hant": "特別週", "ja": "スペシャルウィーク"}) {
		t.Fatal("confirmed names or stable identity lost", rows[0])
	}
	if !reflect.DeepEqual(rows[1].LocalizedNames, map[string]string{"zh-Hans": "待确认"}) {
		t.Fatal("missing or inferred regional names were advertised as confirmed", rows[1])
	}
	before, err := computePackVersion(ds, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	ds.Characters[1].Names.ZhHant = "來源更正"
	after, err := computePackVersion(ds, nil, nil, nil)
	if err != nil || before == after {
		t.Fatal("regional correction reused immutable pack identity", err)
	}
	stable, err := computePackVersion(ds, nil, nil, nil)
	if err != nil || after != stable {
		t.Fatal("identical names changed pack identity", err)
	}
}
