// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package router

import (
	"encoding/json"
	"strings"
	"testing"
)

// validateFixtureSchema mirrors the shapes registered tools use: required
// fields, typed scalars with bounds, enums, arrays with item schemas, nested
// objects, pattern, minLength, maxItems, format, and additionalProperties.
func validateFixtureSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"repo_id": map[string]any{"type": "string", "description": "Repository id."},
			"limit": map[string]any{
				"type": "integer", "default": 25, "minimum": 1, "maximum": 200,
			},
			"score":     map[string]any{"type": "number", "minimum": 0.5, "maximum": float64(10)},
			"recursive": map[string]any{"type": "boolean", "default": false},
			"view":      map[string]any{"type": "string", "enum": []string{"summary", "full"}},
			"kind":      map[string]any{"type": "string", "enum": []any{"file", "entity"}},
			"query":     map[string]any{"type": "string", "minLength": 1, "pattern": `\S`},
			"since":     map[string]any{"type": "string", "format": "date-time"},
			"types": map[string]any{
				"type":  "array",
				"items": map[string]any{"type": "string", "enum": []string{"CALLS", "IMPORTS"}},
			},
			"handles": map[string]any{
				"type":     "array",
				"maxItems": 2,
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"start_line": map[string]any{"type": "integer"},
						"kind":       map[string]any{"type": "string", "enum": []string{"file", "entity"}},
					},
					"required": []any{"kind"},
				},
			},
			"inputs": map[string]any{
				"type":                 "object",
				"additionalProperties": map[string]any{"type": "string"},
			},
		},
		"required": []string{"repo_id"},
	}
}

func TestValidateArgumentsAcceptsValidShapes(t *testing.T) {
	t.Parallel()

	cases := map[string]map[string]any{
		"minimal":              {"repo_id": "r1"},
		"go int":               {"repo_id": "r1", "limit": 5},
		"json float integral":  {"repo_id": "r1", "limit": float64(200)},
		"int64":                {"repo_id": "r1", "limit": int64(1)},
		"json number":          {"repo_id": "r1", "limit": json.Number("7")},
		"number int":           {"repo_id": "r1", "score": 3},
		"number float":         {"repo_id": "r1", "score": 9.75},
		"enum string slice":    {"repo_id": "r1", "view": "full"},
		"enum any slice":       {"repo_id": "r1", "kind": "entity"},
		"pattern and length":   {"repo_id": "r1", "query": " a "},
		"date-time":            {"repo_id": "r1", "since": "2026-10-22T10:00:00Z"},
		"blank date-time":      {"repo_id": "r1", "since": "  "},
		"array any":            {"repo_id": "r1", "types": []any{"CALLS", "IMPORTS"}},
		"array string":         {"repo_id": "r1", "types": []string{"CALLS"}},
		"array of objects":     {"repo_id": "r1", "handles": []any{map[string]any{"kind": "file", "start_line": 4}}},
		"typed object slice":   {"repo_id": "r1", "handles": []map[string]any{{"kind": "entity"}}},
		"additional props":     {"repo_id": "r1", "inputs": map[string]any{"service_name": "api"}},
		"string map":           {"repo_id": "r1", "inputs": map[string]string{"service_name": "api"}},
		"unknown property":     {"repo_id": "r1", "not_in_schema": 12},
		"null optional":        {"repo_id": "r1", "limit": nil, "view": nil},
		"boolean":              {"repo_id": "r1", "recursive": true},
		"empty handles":        {"repo_id": "r1", "handles": []any{}},
		"nil array value":      {"repo_id": "r1", "types": nil},
		"description and dflt": {"repo_id": "r1", "limit": 25},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if errs := ValidateArguments(validateFixtureSchema(), args); len(errs) != 0 {
				t.Fatalf("ValidateArguments(%v) = %v, want no errors", args, errs)
			}
		})
	}
}

func TestValidateArgumentsRejectsInvalidShapes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		args        map[string]any
		wantPath    string
		wantKeyword string
	}{
		{"missing required", map[string]any{"limit": 5}, "repo_id", "required"},
		{"null required", map[string]any{"repo_id": nil}, "repo_id", "required"},
		{"wrong type string", map[string]any{"repo_id": 12}, "repo_id", "type"},
		{"wrong type integer", map[string]any{"repo_id": "r", "limit": "25"}, "limit", "type"},
		{"fractional integer", map[string]any{"repo_id": "r", "limit": 2.5}, "limit", "type"},
		{"wrong type boolean", map[string]any{"repo_id": "r", "recursive": "true"}, "recursive", "type"},
		{"above maximum", map[string]any{"repo_id": "r", "limit": 201}, "limit", "maximum"},
		{"below minimum", map[string]any{"repo_id": "r", "limit": 0}, "limit", "minimum"},
		{"number below minimum", map[string]any{"repo_id": "r", "score": 0.25}, "score", "minimum"},
		{"unknown enum", map[string]any{"repo_id": "r", "view": "verbose"}, "view", "enum"},
		{"unknown any enum", map[string]any{"repo_id": "r", "kind": "dir"}, "kind", "enum"},
		{"empty string minLength", map[string]any{"repo_id": "r", "query": ""}, "query", "minLength"},
		{"whitespace pattern", map[string]any{"repo_id": "r", "query": "   "}, "query", "pattern"},
		{"bad date-time", map[string]any{"repo_id": "r", "since": "yesterday"}, "since", "format"},
		{"array item enum", map[string]any{"repo_id": "r", "types": []any{"CALLS", "READS"}}, "types[1]", "enum"},
		{"array wrong type", map[string]any{"repo_id": "r", "types": "CALLS"}, "types", "type"},
		{"too many items", map[string]any{"repo_id": "r", "handles": []any{
			map[string]any{"kind": "file"}, map[string]any{"kind": "file"}, map[string]any{"kind": "file"},
		}}, "handles", "maxItems"},
		{"nested required", map[string]any{"repo_id": "r", "handles": []any{map[string]any{"start_line": 1}}}, "handles[0].kind", "required"},
		{"nested type", map[string]any{"repo_id": "r", "handles": []any{map[string]any{"kind": "file", "start_line": "1"}}}, "handles[0].start_line", "type"},
		{"additional property type", map[string]any{"repo_id": "r", "inputs": map[string]any{"n": 3}}, "inputs.n", "type"},
		{"object wrong type", map[string]any{"repo_id": "r", "inputs": []any{}}, "inputs", "type"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			errs := ValidateArguments(validateFixtureSchema(), tc.args)
			if len(errs) == 0 {
				t.Fatalf("ValidateArguments(%v) = no errors, want %s at %s", tc.args, tc.wantKeyword, tc.wantPath)
			}
			for _, e := range errs {
				if e.Path == tc.wantPath && e.Keyword == tc.wantKeyword {
					if strings.TrimSpace(e.Message) == "" {
						t.Fatalf("error %+v has an empty message", e)
					}
					return
				}
			}
			t.Fatalf("ValidateArguments(%v) = %+v, want %s at %s", tc.args, errs, tc.wantKeyword, tc.wantPath)
		})
	}
}

func TestValidateArgumentsOrdersErrorsDeterministically(t *testing.T) {
	t.Parallel()

	args := map[string]any{"limit": 0, "view": "x", "recursive": 1, "query": ""}
	first := ValidateArguments(validateFixtureSchema(), args)
	for range 20 {
		again := ValidateArguments(validateFixtureSchema(), args)
		if len(again) != len(first) {
			t.Fatalf("error count changed: %d then %d", len(first), len(again))
		}
		for i := range first {
			if first[i] != again[i] {
				t.Fatalf("error %d changed: %+v then %+v", i, first[i], again[i])
			}
		}
	}
	if len(first) < 5 {
		t.Fatalf("want every violation reported, got %+v", first)
	}
}

func TestValidateArgumentsNilArgumentsChecksRequired(t *testing.T) {
	t.Parallel()

	errs := ValidateArguments(validateFixtureSchema(), nil)
	if len(errs) != 1 || errs[0].Path != "repo_id" || errs[0].Keyword != "required" {
		t.Fatalf("ValidateArguments(nil) = %+v, want one required error at repo_id", errs)
	}
}
