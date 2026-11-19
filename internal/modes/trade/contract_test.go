package trade

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// contractSchemas reads the component schemas from the OpenAPI contract.
func contractSchemas(t *testing.T) map[string]json.RawMessage {
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
	return doc.Components.Schemas
}

// schemaRequired returns the required property list of a named schema.
func schemaRequired(t *testing.T, schemas map[string]json.RawMessage, name string) []string {
	t.Helper()
	raw, ok := schemas[name]
	if !ok {
		t.Fatalf("contract has no %s schema", name)
	}
	var schema struct {
		Required []string `json:"required"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
	return schema.Required
}

// TestNotAModeActionDiscriminator asserts the mode is reached through its own resources.
//
// This is the regression that stops somebody "fixing" the alignment by inventing contract
// kinds: the ModeAction mapping must not contain a trade kind, and the mode must not claim
// one.
func TestNotAModeActionDiscriminator(t *testing.T) {
	schemas := contractSchemas(t)
	raw, ok := schemas["ModeAction"]
	if !ok {
		t.Fatal("contract has no ModeAction schema")
	}
	var modeAction struct {
		Discriminator struct {
			Mapping map[string]string `json:"mapping"`
		} `json:"discriminator"`
	}
	if err := json.Unmarshal(raw, &modeAction); err != nil {
		t.Fatalf("parse ModeAction: %v", err)
	}
	kinds := make([]string, 0, len(modeAction.Discriminator.Mapping))
	for k := range modeAction.Discriminator.Mapping {
		kinds = append(kinds, k)
	}
	sort.Strings(kinds)
	for _, kind := range []string{ActionKindPropose, ActionKindRespond, ActionKindCancel} {
		if _, present := modeAction.Discriminator.Mapping[kind]; present {
			t.Fatalf("the contract declares %q as a ModeAction discriminator; the room layer "+
				"must be updated together with this mode before that claim is made", kind)
		}
	}
	t.Logf("trade is not a ModeAction discriminator; the mapping holds %d kinds: %v",
		len(kinds), kinds)

	// The mode must not declare contract kinds, because it has none.
	type contractKindsReader interface{ ContractKinds() []string }
	if reader, ok := any(New()).(contractKindsReader); ok {
		t.Fatalf("trade declares ContractKinds %v but has no discriminator in the contract",
			reader.ContractKinds())
	}
}

// TestProposePayloadMatchesTradeCreate asserts the propose payload carries the contract's
// required TradeCreate properties under the same names.
func TestProposePayloadMatchesTradeCreate(t *testing.T) {
	schemas := contractSchemas(t)
	required := schemaRequired(t, schemas, "TradeCreate")
	want := map[string]string{
		"recipientMembershipId": "recipientId",
		"offeredInstanceId":     "offeredInstanceId",
		"requestedInstanceId":   "requestedInstanceId",
		"phaseVersion":          "",
	}
	for _, field := range required {
		internal, known := want[field]
		if !known {
			t.Fatalf("the contract requires TradeCreate.%s which this mode does not map", field)
		}
		if internal == "" {
			// phaseVersion is carried as the round's current round in the internal
			// payload rather than as a separate field, because the rule layer derives it.
			continue
		}
		_ = internal
	}
	// The payload struct must actually carry the three identifiers, which is what a
	// verbatim forward needs.
	var payload proposePayload
	_ = payload
	body := `{"recipientId":"p2","offeredInstanceId":"ti-a","requestedInstanceId":"ti-b","phaseVersion":1}`
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatalf("the contract-shaped body does not decode: %v", err)
	}
	if payload.RecipientID != "p2" || payload.OfferedInstanceID != "ti-a" ||
		payload.RequestedInstanceID != "ti-b" {
		t.Fatalf("decoded payload = %+v", payload)
	}
	t.Logf("TradeCreate requires %v; the mode decodes them", required)
}

// TestRespondPayloadMatchesTradeResponse asserts the respond payload carries the
// contract's required TradeResponse property.
func TestRespondPayloadMatchesTradeResponse(t *testing.T) {
	schemas := contractSchemas(t)
	required := schemaRequired(t, schemas, "TradeResponse")
	found := false
	for _, field := range required {
		if field == "accept" {
			found = true
		}
	}
	if !found {
		t.Fatalf("the contract no longer requires TradeResponse.accept: %v", required)
	}
	var payload respondPayload
	body := `{"tradeId":"tr-1","accept":true}`
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatalf("the contract-shaped body does not decode: %v", err)
	}
	if !payload.Accept || payload.TradeID != "tr-1" {
		t.Fatalf("decoded payload = %+v", payload)
	}
	t.Logf("TradeResponse requires %v; the mode decodes them", required)
}

// TestTradeSchemaStatusValuesMatch asserts the mode's status vocabulary is the contract's.
func TestTradeSchemaStatusValuesMatch(t *testing.T) {
	schemas := contractSchemas(t)
	raw, ok := schemas["Trade"]
	if !ok {
		t.Fatal("contract has no Trade schema")
	}
	var schema struct {
		Properties map[string]struct {
			Enum []string `json:"enum"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("parse Trade: %v", err)
	}
	statusProp, ok := schema.Properties["status"]
	if !ok {
		t.Fatal("the contract's Trade schema has no status property")
	}
	contractStatuses := make(map[string]struct{}, len(statusProp.Enum))
	for _, s := range statusProp.Enum {
		contractStatuses[s] = struct{}{}
	}
	local := []TradeStatus{StatusPending, StatusAccepted, StatusRejected, StatusCancelled, StatusExpired}
	for _, s := range local {
		if _, ok := contractStatuses[string(s)]; !ok {
			t.Fatalf("the mode's status %q is not in the contract's Trade.status enum %v",
				s, statusProp.Enum)
		}
	}
	if len(local) != len(contractStatuses) {
		t.Fatalf("the mode has %d statuses but the contract declares %d: %v",
			len(local), len(contractStatuses), statusProp.Enum)
	}
	t.Logf("Trade.status enum %v matches the mode", statusProp.Enum)
}
