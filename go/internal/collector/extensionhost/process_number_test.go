// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extensionhost

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	sdkcollector "github.com/eshu-hq/eshu/sdk/go/collector"
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
