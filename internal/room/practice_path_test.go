package room

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	"github.com/bhcn/umaguessinggame/internal/deployment"
)

type pathPracticeIndex struct{ practiceIndex }

func (p *pathPracticeIndex) Section(name string) (json.RawMessage, bool) {
	p.requested = append(p.requested, name)
	if name != "co-race-path.graph" {
		return nil, false
	}
	return json.RawMessage(`{"nodes":[{"characterId":"a","name":"甲"},{"characterId":"b","name":"乙"}],"edges":[{"from":"a","to":"b","raceId":"r1","raceName":"锦标","date":"2026-01-01","secret":"DO_NOT_EXPORT"}],"seed":"DO_NOT_EXPORT"}`), true
}
func TestPathPracticeCacheWhitelistAndPolicy(t *testing.T) {
	index := &pathPracticeIndex{}
	s := New(nil, Config{}, Options{Content: index})
	defer s.Close()
	p, err := s.PathPracticePack()
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(p.ContentJSON))
	if p.ModeID != "co-race-path" || p.SHA256 != hex.EncodeToString(sum[:]) || p.ByteLength != len(p.ContentJSON) || strings.Contains(p.ContentJSON, "DO_NOT_EXPORT") {
		t.Fatal("invalid practice envelope")
	}
	if len(index.requested) != 1 || index.requested[0] != "co-race-path.graph" {
		t.Fatal("read unrelated section", index.requested)
	}
	var content struct {
		Nodes []pathPracticeNode `json:"nodes"`
		Edges []struct {
			From string `json:"from"`
			To   string `json:"to"`
		} `json:"edges"`
	}
	if err := json.Unmarshal([]byte(p.ContentJSON), &content); err != nil {
		t.Fatal(err)
	}
	if len(content.Nodes) != 2 || content.Nodes[0].CharacterID != "a" || content.Nodes[0].LocalizedNames["ja"] != "スペシャルウィーク" || len(content.Edges) != 1 || content.Edges[0].From != "a" || content.Edges[0].To != "b" {
		t.Fatal("regional names changed graph identities or edges")
	}
	if _, err = s.PathPracticePack(); err != nil || len(index.requested) != 1 {
		t.Fatal("cache miss")
	}
	s.cfg.Policy = deployment.Policy{Forms: map[string]bool{"local": false}}
	if _, err = s.PathPracticePack(); err == nil {
		t.Fatal("cache bypassed policy")
	}
}
