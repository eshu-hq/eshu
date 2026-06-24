// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	pagerduty "github.com/eshu-hq/eshu/examples/collector-extensions/pagerduty"
	sdk "github.com/eshu-hq/eshu/sdk/go/collector"
)

func TestProofDigestChangesOnPayloadDrift(t *testing.T) {
	t.Parallel()

	request := sdkRequest{
		ProtocolVersion: sdk.ProtocolVersionV1Alpha1,
		Claim: sdk.Claim{
			ComponentID:   pagerduty.ComponentID,
			InstanceID:    "pagerduty-reference",
			CollectorKind: pagerduty.CollectorKind,
			SourceSystem:  pagerduty.SourceSystem,
			Scope: sdk.Scope{
				ID:   "pagerduty:account:synthetic-reference",
				Kind: "pagerduty_account",
			},
			SourceRunID:  "component-run-pagerduty",
			GenerationID: "component-generation-pagerduty",
			WorkItemID:   "component-work-pagerduty",
			FencingToken: "42",
			Attempt:      1,
			Deadline:     time.Date(2026, time.June, 14, 15, 0, 0, 0, time.UTC),
			ConfigHandle: "component-config:pagerduty-reference",
		},
		Config: map[string]any{
			"source": map[string]any{
				"input": filepath.Join("..", "..", "testdata", "complete.json"),
			},
		},
	}
	var encoded bytes.Buffer
	if err := json.NewEncoder(&encoded).Encode(request); err != nil {
		t.Fatalf("encode request: %v", err)
	}
	requestBytes := encoded.Bytes()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := run([]string{"--sdk-stdio", "--proof-digest"}, bytes.NewReader(requestBytes), &stdout, &stderr); err != nil {
		t.Fatalf("run proof digest error = %v stderr=%s", err, stderr.String())
	}
	expectedDigest := strings.TrimSpace(stdout.String())
	if !strings.HasPrefix(expectedDigest, "sha256:") {
		t.Fatalf("proof digest = %q, want sha256 prefix", expectedDigest)
	}

	result, err := collectSDKStdio(bytes.NewReader(requestBytes))
	if err != nil {
		t.Fatalf("collect SDK stdio: %v", err)
	}
	actualDigest, err := digestSDKFacts(result.Facts)
	if err != nil {
		t.Fatalf("digest SDK facts: %v", err)
	}
	if actualDigest != expectedDigest {
		t.Fatalf("digestSDKFacts = %q, want %q", actualDigest, expectedDigest)
	}

	result.Facts[0].Payload["status"] = "drifted"
	driftedDigest, err := digestSDKFacts(result.Facts)
	if err != nil {
		t.Fatalf("digest drifted SDK facts: %v", err)
	}
	if driftedDigest == expectedDigest {
		t.Fatal("proof digest did not change after payload drift")
	}
}
