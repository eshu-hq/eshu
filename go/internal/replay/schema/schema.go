// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package schema

import (
	"encoding/json"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/replay/cassette"
)

const (
	schemaDraft2020 = "https://json-schema.org/draft/2020-12/schema"
	schemaID        = "https://eshu.dev/schemas/replay/cassette-format/v1/cassette.schema.json"
	schemaTitle     = "Eshu Replay Cassette Format v1"
)

// CassetteFormatV1 returns the JSON Schema (draft 2020-12) for the v1 cassette
// envelope format. The schema is built in Go from the cassette format contract
// (go/internal/replay/cassette/format.go) rather than hand-maintained, so the
// committed cassette-format.v1.schema.json cannot drift from the structs the
// loader actually reads. The bytes are deterministic (sorted keys, two-space
// indent, trailing newline) so the matches-golden gate is a byte comparison.
func CassetteFormatV1() ([]byte, error) {
	raw, err := json.MarshalIndent(cassetteFormatSchema(), "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal cassette schema: %w", err)
	}
	return append(raw, '\n'), nil
}

func cassetteFormatSchema() map[string]any {
	return map[string]any{
		"$schema":              schemaDraft2020,
		"$id":                  schemaID,
		"title":                schemaTitle,
		"type":                 "object",
		"additionalProperties": false,
		// collector is informational and not enforced by the loader, so it is a
		// documented property but not required. schema_version and scopes are.
		"required": []string{"schema_version", "scopes"},
		"properties": map[string]any{
			"collector":      map[string]any{"type": "string"},
			"schema_version": map[string]any{"const": cassette.SchemaVersionV1},
			"scopes": map[string]any{
				"type":     "array",
				"minItems": 1,
				"items":    ref("#/$defs/scope"),
			},
		},
		"$defs": map[string]any{
			"scope": scopeSchema(),
			"fact":  factSchema(),
		},
	}
}

func scopeSchema() map[string]any {
	return objectSchema(
		[]string{
			"scope_id",
			"source_system",
			"scope_kind",
			"collector_kind",
			"generation_id",
			"observed_at",
		},
		map[string]any{
			"scope_id":       nonEmptyString(),
			"source_system":  nonEmptyString(),
			"scope_kind":     nonEmptyString(),
			"collector_kind": nonEmptyString(),
			"partition_key":  map[string]any{"type": "string"},
			"metadata": map[string]any{
				"type":                 "object",
				"additionalProperties": map[string]any{"type": "string"},
			},
			"generation_id": nonEmptyString(),
			"observed_at":   map[string]any{"type": "string", "format": "date-time"},
			"trigger_kind":  map[string]any{"type": "string"},
			"facts": map[string]any{
				"type":  "array",
				"items": ref("#/$defs/fact"),
			},
		},
	)
}

func factSchema() map[string]any {
	return objectSchema(
		[]string{
			"fact_kind",
			"stable_fact_key",
			"schema_version",
			"payload",
		},
		map[string]any{
			"fact_kind":         nonEmptyString(),
			"stable_fact_key":   nonEmptyString(),
			"schema_version":    nonEmptyString(),
			"collector_kind":    map[string]any{"type": "string"},
			"fencing_token":     map[string]any{"type": "integer", "minimum": 0},
			"source_confidence": map[string]any{"type": "string"},
			"payload":           map[string]any{"type": "object"},
			"is_tombstone":      map[string]any{"type": "boolean"},
			"source_uri":        map[string]any{"type": "string"},
			"source_record_id":  map[string]any{"type": "string"},
		},
	)
}

// objectSchema builds a closed object schema. additionalProperties:false is
// load-bearing: it turns a misspelled field name (which JSON decoding silently
// drops) into a validation failure, which is the whole point of the offline
// author-time validator.
func objectSchema(required []string, properties map[string]any) map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             required,
		"properties":           properties,
	}
}

func nonEmptyString() map[string]any {
	return map[string]any{"type": "string", "minLength": 1}
}

func ref(path string) map[string]any {
	return map[string]any{"$ref": path}
}
