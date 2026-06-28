// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package schema

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/replay/cassette"
)

// ValidateCassetteBytes checks one cassette document's envelope shape entirely
// offline, in microseconds, with no Docker and no graph. It enforces the same
// contract the committed cassette-format.v1 JSON Schema declares:
//
//  1. structural validation via the canonical loader (cassette.ParseAndValidate):
//     required fields, the supported schema_version, and field types; and
//  2. additionalProperties:false at every documented object level — any field
//     name the cassette structs do not declare is rejected, which catches the
//     typo class JSON decoding silently drops (e.g. "source_ur" for
//     "source_uri"), the exact failure that otherwise only surfaces deep inside
//     a CI gate.
//
// name is used only for error context (typically the cassette's file path).
// Errors are field-level: a missing required field or an unknown field names the
// JSON path at which it occurs.
func ValidateCassetteBytes(name string, data []byte) error {
	if _, err := cassette.ParseAndValidate(data); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	if unknown := unknownFields(data); len(unknown) > 0 {
		return fmt.Errorf("%s: %s", name, strings.Join(unknown, "; "))
	}
	return nil
}

// unknownFields decodes the (already-valid) document and reports every object
// field not declared by the cassette structs, as field-level path messages.
// Because ValidateCassetteBytes calls ParseAndValidate first, the bytes are
// known to decode; a decode error here is therefore returned as a single
// message rather than panicking.
func unknownFields(data []byte) []string {
	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		// Top-level is not a JSON object (e.g. an array or scalar). The loader
		// would already have reported the type error; nothing further to add.
		return nil
	}

	var out []string
	out = append(out, unknownIn("", root, fileKeys)...)

	scopes, _ := root["scopes"].([]any)
	for i, raw := range scopes {
		scopeObj, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		prefix := fmt.Sprintf("scopes[%d]", i)
		out = append(out, unknownIn(prefix, scopeObj, scopeKeys)...)

		facts, _ := scopeObj["facts"].([]any)
		for j, rawFact := range facts {
			factObj, ok := rawFact.(map[string]any)
			if !ok {
				continue
			}
			out = append(out, unknownIn(fmt.Sprintf("%s.facts[%d]", prefix, j), factObj, factKeys)...)
		}
	}
	return out
}

// unknownIn returns a sorted, field-level message for every key in obj that is
// not in allowed. Keys are sorted so the message is deterministic.
func unknownIn(prefix string, obj map[string]any, allowed map[string]struct{}) []string {
	var bad []string
	for k := range obj {
		if _, ok := allowed[k]; !ok {
			bad = append(bad, k)
		}
	}
	if len(bad) == 0 {
		return nil
	}
	sort.Strings(bad)
	at := prefix
	if at == "" {
		at = "(root)"
	}
	msgs := make([]string, 0, len(bad))
	for _, k := range bad {
		msgs = append(msgs, fmt.Sprintf("%s: unknown field %q", at, k))
	}
	return msgs
}

// The allowed key sets are derived once from the cassette structs' JSON tags so
// they cannot drift from the format: a new field added to format.go is admitted
// automatically, and the schema_test cross-link gate proves the JSON Schema's
// declared properties match exactly the same sets.
var (
	fileKeys  = jsonKeys(reflect.TypeOf(cassette.File{}))
	scopeKeys = jsonKeys(reflect.TypeOf(cassette.Scope{}))
	factKeys  = jsonKeys(reflect.TypeOf(cassette.Fact{}))
)

// jsonKeys returns the set of JSON object key names a struct serializes to,
// honoring `json:"name,omitempty"` tags and skipping `json:"-"` fields.
func jsonKeys(t reflect.Type) map[string]struct{} {
	keys := make(map[string]struct{}, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		name := jsonFieldName(t.Field(i))
		if name == "" {
			continue
		}
		keys[name] = struct{}{}
	}
	return keys
}

// jsonFieldName extracts the JSON key a struct field serializes to, or "" when
// the field is unexported or tagged `json:"-"`.
func jsonFieldName(f reflect.StructField) string {
	if f.PkgPath != "" { // unexported
		return ""
	}
	tag := f.Tag.Get("json")
	if tag == "-" {
		return ""
	}
	name := f.Name
	if tag != "" {
		if comma := strings.IndexByte(tag, ','); comma >= 0 {
			tag = tag[:comma]
		}
		if tag != "" {
			name = tag
		}
	}
	return name
}
