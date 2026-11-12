package rapport

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// contractMapping reads the ModeAction discriminator mapping from the OpenAPI contract.
func contractMapping(t *testing.T) map[string]string {
	t.Helper()
	path := filepath.Join("..", "..", "..", "api", "openapi.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("contract not readable at %s: %v", path, err)
	}
	var doc struct {
		Components struct {
			Schemas map[string]json.RawMessage `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse contract: %v", err)
	}
	modeAction, ok := doc.Components.Schemas["ModeAction"]
	if !ok {
		t.Fatal("contract has no ModeAction schema")
	}
	var schema struct {
		Discriminator struct {
			Mapping map[string]string `json:"mapping"`
		} `json:"discriminator"`
	}
	if err := json.Unmarshal(modeAction, &schema); err != nil {
		t.Fatalf("parse ModeAction: %v", err)
	}
	return schema.Discriminator.Mapping
}

// TestContractKindsAreDiscriminators asserts every kind this mode accepts is a contract
// discriminator, so the HTTP layer can actually route it .
func TestContractKindsAreDiscriminators(t *testing.T) {
	mapping := contractMapping(t)
	kinds := New().ContractKinds()
	if len(kinds) == 0 {
		t.Fatal("rapport declares no contract kinds")
	}
	for _, kind := range kinds {
		if _, ok := mapping[kind]; !ok {
			t.Fatalf("rapport accepts kind %q which is not in the contract discriminator mapping", kind)
		}
		t.Logf("rapport kind %q -> %s", kind, mapping[kind])
	}
}
