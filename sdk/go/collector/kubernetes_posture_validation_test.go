// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"bytes"
	"encoding/json"
	"math"
	"testing"
)

type postureJSON string
type postureCountAlias int
type postureStateAlias string

func (v postureJSON) MarshalJSON() ([]byte, error) { return []byte(v), nil }

// TestKubernetesPostureProducerFieldsAdmitNativeAndWire uses the field values
// emitted by the core 8859 parity fixture: two workload rows, a service
// account, and its token-posture fact. The full producer fixture stays in core.
func TestKubernetesPostureProducerFieldsAdmitNativeAndWire(t *testing.T) {
	rows := []struct {
		name, kind string
		payload    map[string]any
	}{
		{"service account", "k8s_service_account", map[string]any{"secret_ref_count": 1, "image_pull_secret_ref_count": 0, "automount_token": "true", "provider": "kubernetes"}},
		{"token posture", "k8s_service_account_token_posture", map[string]any{"secret_ref_count": 1, "image_pull_secret_ref_count": 0, "automount_token": "true", "provider": "kubernetes"}},
		{"deployment identity", "k8s_workload_identity_use", map[string]any{"projected_service_account_token": false, "workload_kind": "deployments"}},
		{"pod identity", "k8s_workload_identity_use", map[string]any{"projected_service_account_token": false, "workload_kind": "pods"}},
	}
	contract := Contract{ProtocolVersion: ProtocolVersionV1Alpha1}
	for _, kind := range []string{"k8s_service_account", "k8s_service_account_token_posture", "k8s_workload_identity_use"} {
		contract.Facts = append(contract.Facts, FactDeclaration{Kind: kind, SchemaVersions: []string{"1.0.0"}, SourceConfidence: []SourceConfidence{SourceConfidenceReported}})
	}
	validator := NewValidator(contract)
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			result := readFixture(t, "complete")
			result.Facts[0].Kind = row.kind
			result.Facts[0].SchemaVersion = "1.0.0"
			result.Facts[0].Payload = row.payload
			for _, candidate := range []Result{result, wireKubernetesPostureResult(t, result)} {
				if _, err := validator.ValidateResult(candidate); err != nil {
					t.Fatalf("producer posture fields rejected: %v", err)
				}
			}
		})
	}
}

func wireKubernetesPostureResult(t *testing.T, result Result) Result {
	t.Helper()
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var decoded Result
	if err := decoder.Decode(&decoded); err != nil {
		t.Fatal(err)
	}
	return decoded
}

func TestKubernetesPostureFieldPolicy(t *testing.T) {
	serviceAccount := "k8s_service_account"
	posture := "k8s_service_account_token_posture"
	workload := "k8s_workload_identity_use"
	var nilObject map[string]any
	var nilArray []string
	var nilCount *int
	var nilState *string
	var nilBoolean *bool
	tests := []struct {
		name, kind, schema string
		payload            map[string]any
		wantSafe           bool
	}{
		{"count integer", serviceAccount, "1.0.0", map[string]any{"secret_ref_count": int64(-1)}, true},
		{"count unsigned", posture, "1.0.0", map[string]any{"image_pull_secret_ref_count": uint64(math.MaxUint64)}, true},
		{"count float", serviceAccount, "1.0.0", map[string]any{"secret_ref_count": float32(1.5)}, true},
		{"count negative fraction", serviceAccount, "1.0.0", map[string]any{"secret_ref_count": float64(-2.5)}, true},
		{"count decimal", serviceAccount, "1.0.0", map[string]any{"secret_ref_count": json.Number("1.0")}, true},
		{"count decimal text", posture, "1.0.0", map[string]any{"secret_ref_count": json.Number("9007199254740993.1")}, true},
		{"count exponent text", serviceAccount, "1.0.0", map[string]any{"image_pull_secret_ref_count": json.Number("1e3")}, true},
		{"count nil", serviceAccount, "1.0.0", map[string]any{"secret_ref_count": nil}, true},
		{"count typed nil map", posture, "1.0.0", map[string]any{"secret_ref_count": nilObject}, true},
		{"count typed nil pointer", serviceAccount, "1.0.0", map[string]any{"secret_ref_count": nilCount}, true},
		{"count raw null", serviceAccount, "1.0.0", map[string]any{"secret_ref_count": json.RawMessage("null")}, true},
		{"count raw decimal", serviceAccount, "1.0.0", map[string]any{"secret_ref_count": json.RawMessage("9007199254740993.1")}, true},
		{"count custom decimal", serviceAccount, "1.0.0", map[string]any{"secret_ref_count": postureJSON("9007199254740993.1")}, true},
		{"count alias", serviceAccount, "1.0.0", map[string]any{"secret_ref_count": postureCountAlias(7)}, true},
		{"count string", serviceAccount, "1.0.0", map[string]any{"secret_ref_count": "1"}, false},
		{"count object", posture, "1.0.0", map[string]any{"secret_ref_count": map[string]any{}}, false},
		{"count malformed", serviceAccount, "1.0.0", map[string]any{"secret_ref_count": json.Number("bad")}, false},
		{"count NaN", posture, "1.0.0", map[string]any{"secret_ref_count": math.NaN()}, false},
		{"automount states", serviceAccount, "1.0.0", map[string]any{"automount_token": "unknown"}, true},
		{"automount false", posture, "1.0.0", map[string]any{"automount_token": "false"}, true},
		{"automount nil array", posture, "1.0.0", map[string]any{"automount_token": nilArray}, true},
		{"automount nil pointer", posture, "1.0.0", map[string]any{"automount_token": nilState}, true},
		{"automount custom", serviceAccount, "1.0.0", map[string]any{"automount_token": postureJSON(`"true"`)}, true},
		{"automount alias", serviceAccount, "1.0.0", map[string]any{"automount_token": postureStateAlias("true")}, true},
		{"automount arbitrary", posture, "1.0.0", map[string]any{"automount_token": "enabled"}, false},
		{"automount object", posture, "1.0.0", map[string]any{"automount_token": map[string]any{}}, false},
		{"projected boolean", workload, "1.0.0", map[string]any{"projected_service_account_token": true}, true},
		{"projected custom boolean", workload, "1.0.0", map[string]any{"projected_service_account_token": postureJSON("false")}, true},
		{"projected nil", workload, "1.0.0", map[string]any{"projected_service_account_token": nil}, false},
		{"projected typed nil", workload, "1.0.0", map[string]any{"projected_service_account_token": nilBoolean}, false},
		{"projected raw null", workload, "1.0.0", map[string]any{"projected_service_account_token": json.RawMessage("null")}, false},
		{"projected string", workload, "1.0.0", map[string]any{"projected_service_account_token": "true"}, false},
		{"wrong kind", workload, "1.0.0", map[string]any{"secret_ref_count": 1}, false},
		{"wrong schema", serviceAccount, "2.0.0", map[string]any{"secret_ref_count": 1}, false},
		{"wrong case", serviceAccount, "1.0.0", map[string]any{"Secret_ref_count": 1}, false},
		{"nested field", serviceAccount, "1.0.0", map[string]any{"nested": map[string]any{"automount_token": "true"}}, false},
		{"array descendant", posture, "1.0.0", map[string]any{"rows": []any{map[string]any{"secret_ref_count": 1}}}, false},
		{"custom hidden secret", serviceAccount, "1.0.0", map[string]any{"safe": postureJSON(`{"password":"raw"}`)}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fact := Fact{Kind: tt.kind, SchemaVersion: tt.schema, Payload: tt.payload}
			if err := validateFactPayload(fact); (err == nil) != tt.wantSafe {
				t.Fatalf("native admission error = %v, want safe %t", err, tt.wantSafe)
			}
			raw, err := json.Marshal(tt.payload)
			if err != nil {
				if tt.wantSafe {
					t.Fatalf("safe value did not serialize: %v", err)
				}
				return
			}
			decoder := json.NewDecoder(bytes.NewReader(raw))
			decoder.UseNumber()
			var wire map[string]any
			if err := decoder.Decode(&wire); err != nil {
				t.Fatal(err)
			}
			fact.Payload = wire
			if err := validateFactPayload(fact); (err == nil) != tt.wantSafe {
				t.Fatalf("wire admission error = %v, want safe %t", err, tt.wantSafe)
			}
		})
	}
	for _, key := range []string{"secret_ref_count", "image_pull_secret_ref_count", "automount_token", "projected_service_account_token"} {
		if !IsSensitiveKeyName(key) || ValidateShareSafeKeys(map[string]any{key: 1}) == nil {
			t.Fatalf("generic sensitive-key check weakened for %q", key)
		}
	}
	for _, kind := range []string{serviceAccount, posture, workload} {
		if err := validateFactPayload(Fact{Kind: kind, SchemaVersion: "1.0.0", Payload: map[string]any{}}); err != nil {
			t.Fatalf("optional fields required for %s: %v", kind, err)
		}
	}
}
