// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import "encoding/json"

const (
	schemaDraft2020 = "https://json-schema.org/draft/2020-12/schema"
	schemaID        = "https://eshu.dev/schemas/collector-sdk/v1alpha1/result.schema.json"
)

// JSONSchema returns the collector-sdk/v1alpha1 result JSON Schema.
func JSONSchema() ([]byte, error) {
	raw, err := json.MarshalIndent(resultSchema(), "", "  ")
	if err != nil {
		return nil, err
	}
	return append(raw, '\n'), nil
}

func resultSchema() map[string]any {
	return map[string]any{
		"$schema":              schemaDraft2020,
		"$id":                  schemaID,
		"title":                "Eshu Collector SDK Result",
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"protocol_version", "state", "claim", "generation"},
		"properties": map[string]any{
			"protocol_version": map[string]any{"const": ProtocolVersionV1Alpha1},
			"state": map[string]any{
				"enum": []string{
					string(ResultComplete),
					string(ResultUnchanged),
					string(ResultPartial),
					string(ResultRetryable),
					string(ResultTerminal),
				},
			},
			"claim":      ref("#/$defs/claim"),
			"generation": ref("#/$defs/generation"),
			"facts":      arrayOf(ref("#/$defs/fact")),
			"statuses":   arrayOf(ref("#/$defs/status")),
		},
		"$defs": map[string]any{
			"claim":      claimSchema(),
			"scope":      scopeSchema(),
			"generation": generationSchema(),
			"fact":       factSchema(),
			"source_ref": sourceRefSchema(),
			"redaction":  redactionSchema(),
			"status":     statusSchema(),
		},
	}
}

func claimSchema() map[string]any {
	return objectSchema(
		[]string{
			"component_id",
			"instance_id",
			"collector_kind",
			"source_system",
			"scope",
			"source_run_id",
			"generation_id",
			"work_item_id",
			"fencing_token",
			"attempt",
			"deadline",
			"config_handle",
		},
		map[string]any{
			"component_id":   nonEmptyString(),
			"instance_id":    nonEmptyString(),
			"collector_kind": nonEmptyString(),
			"source_system":  nonEmptyString(),
			"scope":          ref("#/$defs/scope"),
			"source_run_id":  nonEmptyString(),
			"generation_id":  nonEmptyString(),
			"work_item_id":   nonEmptyString(),
			"fencing_token":  nonEmptyString(),
			"attempt":        map[string]any{"type": "integer", "minimum": 1},
			"deadline":       map[string]any{"type": "string", "format": "date-time"},
			"config_handle":  nonEmptyString(),
		},
	)
}

func scopeSchema() map[string]any {
	return objectSchema(
		[]string{"id", "kind"},
		map[string]any{
			"id":   nonEmptyString(),
			"kind": nonEmptyString(),
		},
	)
}

func generationSchema() map[string]any {
	return objectSchema(
		[]string{"id", "observed_at"},
		map[string]any{
			"id":             nonEmptyString(),
			"observed_at":    map[string]any{"type": "string", "format": "date-time"},
			"freshness_hint": map[string]any{"type": "string"},
		},
	)
}

func factSchema() map[string]any {
	return objectSchema(
		[]string{
			"kind",
			"schema_version",
			"stable_key",
			"source_confidence",
			"observed_at",
			"source_ref",
			"payload",
		},
		map[string]any{
			"kind":              nonEmptyString(),
			"schema_version":    map[string]any{"type": "string", "pattern": `^[0-9]+\.[0-9]+\.[0-9]+$`},
			"stable_key":        nonEmptyString(),
			"source_confidence": map[string]any{"enum": sourceConfidenceValues()},
			"observed_at":       map[string]any{"type": "string", "format": "date-time"},
			"tombstone":         map[string]any{"type": "boolean"},
			"source_ref":        ref("#/$defs/source_ref"),
			"payload":           map[string]any{"type": "object"},
			"redactions":        arrayOf(ref("#/$defs/redaction")),
		},
	)
}

func sourceRefSchema() map[string]any {
	return objectSchema(
		[]string{
			"source_system",
			"scope_id",
			"generation_id",
			"fact_key",
			"uri",
			"record_id",
		},
		map[string]any{
			"source_system": nonEmptyString(),
			"scope_id":      nonEmptyString(),
			"generation_id": nonEmptyString(),
			"fact_key":      nonEmptyString(),
			"uri":           nonEmptyString(),
			"record_id":     nonEmptyString(),
		},
	)
}

func redactionSchema() map[string]any {
	return objectSchema(
		[]string{"field", "reason"},
		map[string]any{
			"field":  nonEmptyString(),
			"reason": nonEmptyString(),
		},
	)
}

func statusSchema() map[string]any {
	return objectSchema(
		[]string{"class"},
		map[string]any{
			"class": map[string]any{
				"enum": []string{
					string(StatusProgress),
					string(StatusWarning),
					string(StatusFailure),
					string(StatusComplete),
				},
			},
			"failure_class":       map[string]any{"type": "string"},
			"retry_after_seconds": map[string]any{"type": "integer", "minimum": 0},
			"partial":             map[string]any{"type": "boolean"},
			"warning_count":       map[string]any{"type": "integer", "minimum": 0},
			"fact_count":          map[string]any{"type": "integer", "minimum": 0},
			"source_latency_ms":   map[string]any{"type": "integer", "minimum": 0},
		},
	)
}

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

func arrayOf(item map[string]any) map[string]any {
	return map[string]any{"type": "array", "items": item}
}

func sourceConfidenceValues() []string {
	return []string{
		string(SourceConfidenceObserved),
		string(SourceConfidenceReported),
		string(SourceConfidenceInferred),
		string(SourceConfidenceDerived),
	}
}
