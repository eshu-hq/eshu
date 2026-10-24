// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

const testAzureTagMarker = "redacted:hmac-sha256:" +
	"c78a3aed0e9e0e9662dc19a40eddcae8440730c99950d67b15453e8affde39a8"

type azureTagCustomJSON string

func (v azureTagCustomJSON) MarshalJSON() ([]byte, error) { return []byte(v), nil }

func azureTagValidationResult(t *testing.T) (Validator, Result) {
	t.Helper()
	result := readFixture(t, "complete")
	result.Facts[0].Kind = "azure_tag_observation"
	result.Facts[0].Payload = map[string]any{
		"tag_value_fingerprints": map[string]string{
			"env":     testAzureTagMarker,
			"api_key": testAzureTagMarker,
		},
	}
	contract := testContract()
	contract.Facts = append(contract.Facts, FactDeclaration{
		Kind: "azure_tag_observation", SchemaVersions: []string{"1.0.0"},
		SourceConfidence: []SourceConfidence{SourceConfidenceReported},
	})
	return NewValidator(contract), result
}

func TestAzureTagFingerprintValidationNativeAndWire(t *testing.T) {
	validator, native := azureTagValidationResult(t)
	for _, test := range []struct {
		name   string
		result Result
	}{
		{name: "native", result: native},
		{name: "wire", result: cloneResult(native)},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := validator.ValidateResult(test.result); err != nil {
				t.Fatalf("valid keyed tag evidence rejected: %v", err)
			}
		})
	}
	if err := ValidateShareSafeKeys(cloneResult(native).Facts[0].Payload); err == nil {
		t.Fatal("context-free share-safe validation accepted sensitive-shaped tag key")
	}
}

func TestAzureTagFingerprintValidationRejectsUnsafeValues(t *testing.T) {
	validator, valid := azureTagValidationResult(t)
	cases := []struct {
		name  string
		value any
	}{
		{"raw", "prod"},
		{"placeholder", "redacted:tag-env"},
		{"short digest", "redacted:hmac-sha256:" + strings.Repeat("a", 63)},
		{"upper hex", "redacted:hmac-sha256:" + strings.Repeat("A", 64)},
		{"non-hex", "redacted:hmac-sha256:" + strings.Repeat("g", 64)},
		{"null", nil},
		{"number", 7},
		{"array", []any{"raw"}},
		{"nested object", map[string]any{"env": "raw"}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			candidate := cloneResult(valid)
			candidate.Facts[0].Payload["tag_value_fingerprints"] = map[string]any{"env": test.value}
			if _, err := validator.ValidateResult(candidate); err == nil {
				t.Fatal("unsafe tag marker accepted")
			}
		})
	}
	for _, value := range []any{map[string]string(nil), map[string]any(nil), nil} {
		candidate := valid
		candidate.Facts = append([]Fact(nil), valid.Facts...)
		candidate.Facts[0].Payload = map[string]any{"tag_value_fingerprints": value}
		if _, err := validator.ValidateResult(candidate); err == nil {
			t.Fatalf("nil tag container %T accepted", value)
		}
	}
}

func TestAzureTagFingerprintValidationStaysAtExactFactPath(t *testing.T) {
	validator, valid := azureTagValidationResult(t)
	for _, test := range []struct {
		name   string
		mutate func(*Result)
	}{
		{"wrong kind", func(r *Result) { r.Facts[0].Kind = "dev.eshu.examples.scorecard.snapshot" }},
		{"nested path", func(r *Result) { r.Facts[0].Payload = map[string]any{"nested": r.Facts[0].Payload} }},
		{"other sensitive key", func(r *Result) { r.Facts[0].Payload["api_key"] = testAzureTagMarker }},
		{"typed map", func(r *Result) { r.Facts[0].Payload["probe"] = map[string]string{"api_key": "raw"} }},
		{"typed slice", func(r *Result) { r.Facts[0].Payload["probe"] = []map[string]string{{"api_key": "raw"}} }},
		{"custom JSON", func(r *Result) { r.Facts[0].Payload["probe"] = azureTagCustomJSON(`{"api_key":"raw"}`) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			candidate := cloneResult(valid)
			test.mutate(&candidate)
			if _, err := validator.ValidateResult(candidate); err == nil {
				t.Fatal("unsafe payload accepted")
			}
		})
	}
	for _, test := range []any{
		map[string]any{"when": time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)},
		azureTagCustomJSON(`{"safe":"ok"}`),
		json.Number("9007199254740993"),
	} {
		candidate := cloneResult(valid)
		candidate.Facts[0].Payload["probe"] = test
		if _, err := validator.ValidateResult(candidate); err != nil {
			t.Fatalf("safe value %T rejected: %v", test, err)
		}
	}
}
