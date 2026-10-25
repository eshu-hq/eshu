// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extensionhost

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	sdkcollector "github.com/eshu-hq/eshu-sdk/collector"
)

func TestProcessRunnerAzureTagMarkersReachValidator(t *testing.T) {
	if mode := os.Getenv("ESHU_EXTENSIONHOST_PROCESS_HELPER"); strings.HasPrefix(mode, "azure-tag-") {
		var request Request
		if err := json.NewDecoder(os.Stdin).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		item := testWorkItem()
		fact := testSDKFact(item)
		fact.Kind = "azure_tag_observation"
		fact.SourceConfidence = sdkcollector.SourceConfidenceReported
		value := "redacted:hmac-sha256:c78a3aed0e9e0e9662dc19a40eddcae8440730c99950d67b15453e8affde39a8"
		if mode == "azure-tag-raw" {
			value = "synthetic-raw-tag-canary"
		}
		fact.Payload = map[string]any{"tag_value_fingerprints": map[string]string{"api_key": value}}
		result := completeResult(item, fact)
		result.Claim = request.Claim
		if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
			t.Fatalf("encode result: %v", err)
		}
		os.Exit(0)
	}

	item := testWorkItem()
	request := Request{ProtocolVersion: sdkcollector.ProtocolVersionV1Alpha1, Claim: testSDKClaim(item)}
	contract := sdkcollector.Contract{
		ProtocolVersion: sdkcollector.ProtocolVersionV1Alpha1,
		Facts: []sdkcollector.FactDeclaration{{
			Kind: "azure_tag_observation", SchemaVersions: []string{"1.0.0"},
			SourceConfidence: []sdkcollector.SourceConfidence{sdkcollector.SourceConfidenceReported},
		}},
	}
	validator := sdkcollector.NewValidator(contract)
	for _, test := range []struct {
		name      string
		wantError bool
	}{
		{"azure-tag-safe", false},
		{"azure-tag-raw", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			runner := ProcessRunner{
				Command: os.Args[0],
				Args:    []string{"-test.run=^TestProcessRunnerAzureTagMarkersReachValidator$"},
				Env:     []string{"ESHU_EXTENSIONHOST_PROCESS_HELPER=" + test.name},
			}
			result, err := runner.RunCollector(context.Background(), request)
			if err != nil {
				t.Fatalf("RunCollector: %v", err)
			}
			_, err = validator.ValidateResult(result)
			if (err != nil) != test.wantError {
				t.Fatalf("ValidateResult error = %v, wantError = %t", err, test.wantError)
			}
		})
	}
}

// TestProcessRunnerKubernetesPostureFactsReachValidator exercises the real
// subprocess JSON boundary with the source facts used by the k8s-live parity
// cassette. Result validation must admit only the scoped posture fields.
func TestProcessRunnerKubernetesPostureFactsReachValidator(t *testing.T) {
	if mode := os.Getenv("ESHU_EXTENSIONHOST_PROCESS_HELPER"); strings.HasPrefix(mode, "k8s-posture-") {
		var request Request
		if err := json.NewDecoder(os.Stdin).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		kind := strings.TrimPrefix(mode, "k8s-posture-")
		unsafe := strings.HasSuffix(kind, "-unsafe")
		kind = strings.TrimSuffix(kind, "-unsafe")
		payload := kubernetesPostureParityPayload(t, kind)
		if unsafe {
			payload["api_key"] = "synthetic-raw-secret-canary"
		}
		item := testWorkItem()
		fact := testSDKFact(item)
		fact.Kind = kind
		fact.SourceConfidence = sdkcollector.SourceConfidenceReported
		fact.Payload = payload
		result := completeResult(item, fact)
		result.Claim = request.Claim
		if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
			t.Fatalf("encode result: %v", err)
		}
		os.Exit(0)
	}

	item := testWorkItem()
	request := Request{ProtocolVersion: sdkcollector.ProtocolVersionV1Alpha1, Claim: testSDKClaim(item)}
	for _, kind := range []string{
		"k8s_service_account",
		"k8s_service_account_token_posture",
		"k8s_workload_identity_use",
	} {
		t.Run(kind, func(t *testing.T) {
			contract := sdkcollector.Contract{
				ProtocolVersion: sdkcollector.ProtocolVersionV1Alpha1,
				Facts: []sdkcollector.FactDeclaration{{
					Kind: kind, SchemaVersions: []string{"1.0.0"},
					SourceConfidence: []sdkcollector.SourceConfidence{sdkcollector.SourceConfidenceReported},
				}},
			}
			validator := sdkcollector.NewValidator(contract)
			for _, tc := range []struct {
				name, mode string
				wantError  bool
			}{
				{"safe", kind, false},
				{"unsafe", kind + "-unsafe", true},
			} {
				t.Run(tc.name, func(t *testing.T) {
					runner := ProcessRunner{
						Command: os.Args[0],
						Args:    []string{"-test.run=^TestProcessRunnerKubernetesPostureFactsReachValidator$"},
						Env:     []string{"ESHU_EXTENSIONHOST_PROCESS_HELPER=k8s-posture-" + tc.mode},
					}
					result, err := runner.RunCollector(context.Background(), request)
					if err != nil {
						t.Fatalf("RunCollector: %v", err)
					}
					_, err = validator.ValidateResult(result)
					if (err != nil) != tc.wantError {
						t.Fatalf("ValidateResult error = %v, wantError = %t", err, tc.wantError)
					}
				})
			}
		})
	}
}

func kubernetesPostureParityPayload(t *testing.T, kind string) map[string]any {
	t.Helper()
	path := filepath.Join("..", "kuberneteslive", "testdata", "8859-parity-kubernetes-live.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var cassette struct {
		Scopes []struct {
			Facts []struct {
				Kind    string         `json:"fact_kind"`
				Payload map[string]any `json:"payload"`
			} `json:"facts"`
		} `json:"scopes"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&cassette); err != nil {
		t.Fatal(err)
	}
	for _, scope := range cassette.Scopes {
		for _, fact := range scope.Facts {
			if fact.Kind != kind {
				continue
			}
			keys := []string{"projected_service_account_token"}
			if kind != "k8s_workload_identity_use" {
				keys = []string{"secret_ref_count", "image_pull_secret_ref_count", "automount_token"}
			}
			for _, key := range keys {
				if _, ok := fact.Payload[key]; !ok {
					t.Fatalf("k8s parity %s fact missing %s", kind, key)
				}
			}
			return fact.Payload
		}
	}
	t.Fatalf("k8s parity cassette has no %s fact", kind)
	return nil
}
