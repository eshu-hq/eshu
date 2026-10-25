// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extensionhost

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	sdkcollector "github.com/eshu-hq/eshu-sdk/collector"
	factschema "github.com/eshu-hq/eshu-sdk/factschema"
	"github.com/eshu-hq/eshu-sdk/factschema/fixturepack"
	"github.com/eshu-hq/eshu/go/internal/component"
	"github.com/eshu-hq/eshu/go/internal/factenvelope"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

func TestProcessResultNumbersSurviveClaimedSource(t *testing.T) {
	if os.Getenv("ESHU_EXTENSIONHOST_PROCESS_HELPER") == "number" {
		var request Request
		if err := json.NewDecoder(os.Stdin).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		item := testWorkItem()
		fact := testSDKFact(item)
		fact.Payload = map[string]any{
			"large":    json.Number("9007199254740993"),
			"negative": json.Number("-9007199254740993"),
			"max":      json.Number("9223372036854775807"),
			"decimal":  json.Number("0.12345678901234567890123456789"),
			"nested": map[string]any{
				"values": []any{json.Number("9007199254740993"), json.Number("-9223372036854775808")},
			},
		}
		result := completeResult(item, fact)
		result.Claim = request.Claim
		if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
			t.Fatalf("encode result: %v", err)
		}
		os.Exit(0)
	}

	item := testWorkItem()
	item.LeaseExpiresAt = time.Now().Add(time.Minute)
	runner := ProcessRunner{
		Command: os.Args[0],
		Args:    []string{"-test.run=^TestProcessResultNumbersSurviveClaimedSource$"},
		Env:     []string{"ESHU_EXTENSIONHOST_PROCESS_HELPER=number"},
	}
	request := Request{ProtocolVersion: sdkcollector.ProtocolVersionV1Alpha1, Claim: testSDKClaim(item), Contract: testContract()}
	result, err := runner.RunCollector(context.Background(), request)
	if err != nil {
		t.Fatalf("RunCollector() error = %v", err)
	}
	assertExactNumbers(t, result.Facts[0].Payload)

	source := mustNewSource(t, runner, nil)
	collected, ok, err := source.NextClaimed(context.Background(), item)
	if err != nil || !ok {
		t.Fatalf("NextClaimed() ok=%t error=%v, want true nil", ok, err)
	}
	envelopes := collectFacts(t, collected)
	if len(envelopes) != 1 {
		t.Fatalf("collected facts = %d, want 1", len(envelopes))
	}
	assertExactNumbers(t, envelopes[0].Clone().Payload)
}

func TestProcessResultNumberReachesPublicTypedDecoder(t *testing.T) {
	if os.Getenv("ESHU_EXTENSIONHOST_PROCESS_HELPER") == "typed-number" {
		var request Request
		if err := json.NewDecoder(os.Stdin).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		item := testWorkItem()
		fact := testSDKFact(item)
		fact.Kind = factschema.FactKindWorkItemProjectMetadata
		payload, ok := fixturepack.ValidPayload(fact.Kind)
		if !ok {
			t.Fatal("missing typed payload fixture")
		}
		payload["issue_count"] = json.Number("9007199254740993")
		fact.Payload = payload
		result := completeResult(item, fact)
		result.Claim = request.Claim
		if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
			t.Fatalf("encode result: %v", err)
		}
		os.Exit(0)
	}

	item := testWorkItem()
	item.LeaseExpiresAt = time.Now().Add(time.Minute)
	manifest := testManifest()
	manifest.Spec.EmittedFacts[0].Kind = factschema.FactKindWorkItemProjectMetadata
	manifest.Spec.EmittedFacts[0].PayloadSchemaRef = factschema.FactKindWorkItemProjectMetadata
	grants := []component.ProducerGrant{{
		ProducerID: manifest.Metadata.ID, Version: manifest.Metadata.Version,
		Kind:           factschema.FactKindWorkItemProjectMetadata,
		SchemaVersions: []string{"1.0.0"}, Scope: "repo",
		ExpiresAt: time.Now().Add(time.Hour).UTC(),
	}}
	runner := ProcessRunner{
		Command: os.Args[0],
		Args:    []string{"-test.run=^TestProcessResultNumberReachesPublicTypedDecoder$"},
		Env:     []string{"ESHU_EXTENSIONHOST_PROCESS_HELPER=typed-number"},
	}
	source, err := NewSource(Config{
		Manifest: manifest, CollectorInstanceID: item.CollectorInstanceID,
		ScopeKind: scope.KindRepository, ConfigHandle: "cfg-scorecard",
		Config: map[string]any{"fixture": "scorecard"}, Runner: runner,
		Clock: testObservedAt, Grants: grants,
	})
	if err != nil {
		t.Fatalf("NewSource: %v", err)
	}
	collected, ok, err := source.NextClaimed(context.Background(), item)
	if err != nil || !ok {
		t.Fatalf("NextClaimed ok=%t, err=%v", ok, err)
	}
	envelopes := collectFacts(t, collected)
	if len(envelopes) != 1 {
		t.Fatalf("envelopes = %d", len(envelopes))
	}
	decoded, err := factschema.DecodeWorkItemProjectMetadata(factenvelope.FactSchemaFromInternal(envelopes[0].Clone()))
	if err != nil {
		t.Fatalf("typed decode: %v", err)
	}
	if decoded.IssueCount == nil || *decoded.IssueCount != int64(9007199254740993) {
		t.Fatalf("typed issue count = %v", decoded.IssueCount)
	}
}

func TestProcessResultNumberReachesNestedTypedDecoder(t *testing.T) {
	if os.Getenv("ESHU_EXTENSIONHOST_PROCESS_HELPER") == "typed-nested-number" {
		var request Request
		if err := json.NewDecoder(os.Stdin).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		item := testWorkItem()
		fact := testSDKFact(item)
		fact.Kind = factschema.FactKindKubernetesLivePodTemplate
		payload, ok := fixturepack.ValidPayload(fact.Kind)
		if !ok {
			t.Fatal("missing typed payload fixture")
		}
		payload["containers"] = []any{map[string]any{"ports": []any{json.Number("8e1")}}}
		fact.Payload = payload
		result := completeResult(item, fact)
		result.Claim = request.Claim
		if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
			t.Fatalf("encode result: %v", err)
		}
		os.Exit(0)
	}

	item := testWorkItem()
	item.LeaseExpiresAt = time.Now().Add(time.Minute)
	manifest := testManifest()
	manifest.Spec.EmittedFacts[0].Kind = factschema.FactKindKubernetesLivePodTemplate
	manifest.Spec.EmittedFacts[0].PayloadSchemaRef = factschema.FactKindKubernetesLivePodTemplate
	grants := []component.ProducerGrant{{
		ProducerID: manifest.Metadata.ID, Version: manifest.Metadata.Version,
		Kind:           factschema.FactKindKubernetesLivePodTemplate,
		SchemaVersions: []string{"1.0.0"}, Scope: "repo",
		ExpiresAt: time.Now().Add(time.Hour).UTC(),
	}}
	runner := ProcessRunner{
		Command: os.Args[0],
		Args:    []string{"-test.run=^TestProcessResultNumberReachesNestedTypedDecoder$"},
		Env:     []string{"ESHU_EXTENSIONHOST_PROCESS_HELPER=typed-nested-number"},
	}
	source, err := NewSource(Config{
		Manifest: manifest, CollectorInstanceID: item.CollectorInstanceID,
		ScopeKind: scope.KindRepository, ConfigHandle: "cfg-scorecard",
		Config: map[string]any{"fixture": "scorecard"}, Runner: runner,
		Clock: testObservedAt, Grants: grants,
	})
	if err != nil {
		t.Fatalf("NewSource: %v", err)
	}
	collected, ok, err := source.NextClaimed(context.Background(), item)
	if err != nil || !ok {
		t.Fatalf("NextClaimed ok=%t, err=%v", ok, err)
	}
	envelopes := collectFacts(t, collected)
	if len(envelopes) != 1 {
		t.Fatalf("envelopes = %d", len(envelopes))
	}
	decoded, err := factschema.DecodeKubernetesLivePodTemplate(factenvelope.FactSchemaFromInternal(envelopes[0].Clone()))
	if err != nil {
		t.Fatalf("typed decode: %v", err)
	}
	if len(decoded.Containers) != 1 || len(decoded.Containers[0].Ports) != 1 || decoded.Containers[0].Ports[0] != 80 {
		t.Fatalf("typed containers = %+v", decoded.Containers)
	}
}

func assertExactNumbers(t *testing.T, payload map[string]any) {
	t.Helper()
	want := map[string]string{
		"large": "9007199254740993", "negative": "-9007199254740993",
		"max": "9223372036854775807", "decimal": "0.12345678901234567890123456789",
	}
	for key, expected := range want {
		got, err := json.Marshal(payload[key])
		if err != nil || string(got) != expected {
			t.Fatalf("payload[%q] JSON = %s, error = %v; want %s", key, got, err, expected)
		}
	}
	nested, ok := payload["nested"].(map[string]any)
	if !ok {
		t.Fatalf("payload nested = %T, want map", payload["nested"])
	}
	values, ok := nested["values"].([]any)
	if !ok || len(values) != 2 {
		t.Fatalf("nested values = %#v, want two elements", nested["values"])
	}
	for index, expected := range []string{"9007199254740993", "-9223372036854775808"} {
		got, err := json.Marshal(values[index])
		if err != nil || string(got) != expected {
			t.Fatalf("nested values[%d] JSON = %s, error = %v; want %s", index, got, err, expected)
		}
	}
}
