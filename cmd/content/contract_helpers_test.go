package main

import "encoding/json"

// jsonUnmarshalForTest keeps the test file's imports minimal.
func jsonUnmarshalForTest(b []byte, v any) error { return json.Unmarshal(b, v) }

// jsonMarshalForTest renders a mutated document.
func jsonMarshalForTest(v any) ([]byte, error) { return json.Marshal(v) }
