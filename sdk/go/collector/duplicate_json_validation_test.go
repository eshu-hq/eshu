// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

type duplicatePayloadJSON string

func (value duplicatePayloadJSON) MarshalJSON() ([]byte, error) { return []byte(value), nil }

func TestValidateResultRejectsDuplicateNativeJSONKeys(t *testing.T) {
	const canary = "synthetic-raw-secret-duplicate"
	const marker = "redacted:hmac-sha256:c78a3aed0e9e0e9662dc19a40eddcae8440730c99950d67b15453e8affde39a8"
	result := readFixture(t, "complete")
	result.Facts[0].Kind = "azure_tag_observation"
	result.Facts[0].SchemaVersion = "1.0.0"
	result.Facts[0].Payload = map[string]any{
		"tag_value_fingerprints": json.RawMessage(`{"api_key":"` + canary + `","api_key":"` + marker + `"}`),
	}
	contract := Contract{ProtocolVersion: ProtocolVersionV1Alpha1, Facts: []FactDeclaration{{Kind: "azure_tag_observation", SchemaVersions: []string{"1.0.0"}, SourceConfidence: []SourceConfidence{SourceConfidenceReported}}}}
	raw, err := json.Marshal(result)
	if err != nil || !bytes.Contains(raw, []byte(canary)) {
		t.Fatalf("invalid seeded representation: %v", err)
	}
	_, err = NewValidator(contract).ValidateResult(result)
	if err == nil {
		emitted, marshalErr := json.Marshal(result)
		t.Fatalf("duplicate native keys accepted: retained raw canary=%t marshal_error=%v", bytes.Contains(emitted, []byte(canary)), marshalErr)
	}
	if strings.Contains(err.Error(), canary) || strings.Contains(err.Error(), "api_key") {
		t.Fatalf("validation error exposed payload content: %v", err)
	}
	emitted, err := json.Marshal(result)
	if err != nil || !bytes.Contains(emitted, []byte(canary)) {
		t.Fatalf("validation mutated caller result: retained raw canary=%t marshal_error=%v", bytes.Contains(emitted, []byte(canary)), err)
	}
}

func TestValidateResultCanonicalJSONMemberUniqueness(t *testing.T) {
	const marker = "redacted:hmac-sha256:c78a3aed0e9e0e9662dc19a40eddcae8440730c99950d67b15453e8affde39a8"
	tests := []struct {
		name    string
		field   string
		payload string
		kind    string
		wantErr bool
	}{
		{"azure reversed", "tag_value_fingerprints", `{"api_key":"` + marker + `","api_key":"synthetic-raw-secret-duplicate"}`, "azure_tag_observation", true},
		{"escaped equivalent", "body", `{"plain":1,"pl\u0061in":2}`, "sample.fact", true},
		{"prior null", "body", `{"plain":null,"plain":1}`, "sample.fact", true},
		{"identical benign", "body", `{"plain":"same","plain":"same"}`, "sample.fact", true},
		{"nested object", "body", `{"outer":{"plain":1,"plain":2}}`, "sample.fact", true},
		{"nested array", "body", `{"entries":[{"plain":1,"plain":2}]}`, "sample.fact", true},
		{"sibling reuse", "body", `{"a":{"plain":1},"b":{"plain":2}}`, "sample.fact", false},
		{"numeric lexemes", "body", `{"count":9007199254740993.1,"exponent":1e+123}`, "sample.fact", false},
		{"azure unique", "tag_value_fingerprints", `{"api_key":"` + marker + `"}`, "azure_tag_observation", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := readFixture(t, "complete")
			result.Facts[0].Kind = tc.kind
			result.Facts[0].SchemaVersion = "1.0.0"
			result.Facts[0].Payload = map[string]any{tc.field: json.RawMessage(tc.payload)}
			contract := Contract{ProtocolVersion: ProtocolVersionV1Alpha1, Facts: []FactDeclaration{{Kind: tc.kind, SchemaVersions: []string{"1.0.0"}, SourceConfidence: []SourceConfidence{SourceConfidenceReported}}}}
			before := append([]byte(nil), result.Facts[0].Payload[tc.field].(json.RawMessage)...)
			_, err := NewValidator(contract).ValidateResult(result)
			if (err != nil) != tc.wantErr {
				t.Fatalf("ValidateResult() error = %v, want error %t", err, tc.wantErr)
			}
			if err != nil && (strings.Contains(err.Error(), "synthetic-raw-secret-duplicate") || strings.Contains(err.Error(), "plain")) {
				t.Fatalf("validation error exposed payload content: %v", err)
			}
			if !bytes.Equal(result.Facts[0].Payload[tc.field].(json.RawMessage), before) {
				t.Fatal("validation mutated caller RawMessage")
			}
		})
	}
}

func TestValidateResultRejectsCustomMarshalerDuplicateJSONKeys(t *testing.T) {
	result := readFixture(t, "complete")
	result.Facts[0].Payload = map[string]any{
		"body": duplicatePayloadJSON(`{"plain":1,"plain":2}`),
	}
	if _, err := NewValidator(testContract()).ValidateResult(result); err == nil {
		t.Fatal("custom marshaler duplicate JSON members accepted")
	}
}

func TestCanonicalPayloadPreservesNumberTokens(t *testing.T) {
	decoded, err := decodeCanonicalPayload([]byte(`{"number":9007199254740993.1,"exponent":1e+123,"empty":[],"null":null}`))
	if err != nil {
		t.Fatal(err)
	}
	if decoded["number"] != json.Number("9007199254740993.1") || decoded["exponent"] != json.Number("1e+123") {
		t.Fatalf("number tokens changed: %v", decoded)
	}
	array, ok := decoded["empty"].([]any)
	if !ok || array == nil || len(array) != 0 || decoded["null"] != nil {
		t.Fatalf("array/null shape changed: %v", decoded)
	}
	for _, raw := range []string{`{"a":1} {"b":2}`, `{"a":1`, `{"a":1]`} {
		if _, err := decodeCanonicalPayload([]byte(raw)); err == nil {
			t.Fatalf("invalid canonical document accepted: %q", raw)
		}
	}
}
