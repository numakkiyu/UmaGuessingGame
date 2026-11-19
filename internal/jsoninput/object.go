// Package jsoninput rejects ambiguous or excessive JSON before schema decoding.
package jsoninput

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"unicode/utf8"
)

var ErrObject = errors.New("expected one JSON object with unique fields and depth at most 32")

// ValidateObject includes nested objects and case aliases recognized by Go's
// struct decoder. Null values inside an object remain a schema-level decision.
func ValidateObject(raw []byte) error {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || raw[0] != '{' || !utf8.Valid(raw) {
		return ErrObject
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if err := value(d, 0); err != nil {
		return ErrObject
	}
	if _, err := d.Token(); err != io.EOF {
		return ErrObject
	}
	return nil
}

func value(d *json.Decoder, depth int) error {
	if depth > 32 {
		return ErrObject
	}
	t, err := d.Token()
	if err != nil {
		return err
	}
	switch t {
	case json.Delim('{'):
		seen := make(map[string]bool)
		for d.More() {
			key, err := d.Token()
			if err != nil {
				return err
			}
			name, ok := key.(string)
			if !ok {
				return ErrObject
			}
			name = strings.ToLower(name)
			if seen[name] {
				return ErrObject
			}
			seen[name] = true
			if err := value(d, depth+1); err != nil {
				return err
			}
		}
		end, err := d.Token()
		if err != nil || end != json.Delim('}') {
			return ErrObject
		}
	case json.Delim('['):
		for d.More() {
			if err := value(d, depth+1); err != nil {
				return err
			}
		}
		end, err := d.Token()
		if err != nil || end != json.Delim(']') {
			return ErrObject
		}
	}
	return nil
}
