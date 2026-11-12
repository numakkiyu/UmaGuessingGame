package room

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
)

// resolveModeConfig gives validation and preparation the same flat rule object.
// API requests may put per-mode rules inside config; older clients use top-level
// fields. Reject contradictions rather than choosing different rules per reader.
// Persist this result for NEW rounds only; existing stored rounds are not rewritten.
func resolveModeConfig(raw json.RawMessage) (json.RawMessage, error) {
	var outer map[string]json.RawMessage
	if err := json.Unmarshal(raw, &outer); err != nil || outer == nil {
		return nil, fmt.Errorf("modeConfig must be an object")
	}
	innerRaw, exists := outer["config"]
	if !exists {
		return raw, nil
	}
	var inner map[string]json.RawMessage
	if err := json.Unmarshal(innerRaw, &inner); err != nil || inner == nil {
		return nil, fmt.Errorf("modeConfig.config must be an object")
	}
	delete(outer, "config")
	for key, value := range inner {
		switch key {
		case "modeId", "rulesVersion", "contentPackId", "informationStyle", "config":
			return nil, fmt.Errorf("modeConfig.config cannot redefine %s", key)
		}
		if previous, exists := outer[key]; exists && !sameConfigValue(previous, value) {
			return nil, fmt.Errorf("modeConfig has conflicting values for %s", key)
		}
		outer[key] = value
	}
	return json.Marshal(outer)
}
func sameConfigValue(a, b json.RawMessage) bool {
	var left, right any
	da, db := json.NewDecoder(bytes.NewReader(a)), json.NewDecoder(bytes.NewReader(b))
	da.UseNumber()
	db.UseNumber()
	return da.Decode(&left) == nil && db.Decode(&right) == nil && reflect.DeepEqual(left, right)
}
