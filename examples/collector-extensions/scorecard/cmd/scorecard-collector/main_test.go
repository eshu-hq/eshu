// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	scorecard "github.com/eshu-hq/eshu/examples/collector-extensions/scorecard"
	sdk "github.com/eshu-hq/eshu/sdk/go/collector"
)

func TestRunSDKStdioUsesHostClaimAndConfig(t *testing.T) {
	t.Parallel()

	claim := sdk.Claim{
		ComponentID:   scorecard.ComponentID,
		InstanceID:    "scorecard-local",
		CollectorKind: scorecard.CollectorKind,
		SourceSystem:  scorecard.SourceSystem,
		Scope: sdk.Scope{
			ID:   "github.com/example/widgets",
			Kind: "repository",
		},
		SourceRunID:  "component-run-scorecard",
		GenerationID: "component-generation-scorecard",
		WorkItemID:   "component-work-scorecard",
		FencingToken: "42",
		Attempt:      1,
		Deadline:     time.Date(2026, time.June, 9, 15, 0, 0, 0, time.UTC),
		ConfigHandle: "component-config:scorecard-local",
	}
	request := sdkRequest{
		ProtocolVersion: sdk.ProtocolVersionV1Alpha1,
		Claim:           claim,
		Contract: sdk.Contract{
			ProtocolVersion: sdk.ProtocolVersionV1Alpha1,
			Facts: []sdk.FactDeclaration{
				{
					Kind:             scorecard.FactKindSnapshot,
					SchemaVersions:   []string{"1.0.0"},
					SourceConfidence: []sdk.SourceConfidence{sdk.SourceConfidenceReported},
				},
				{
					Kind:             scorecard.FactKindCheck,
					SchemaVersions:   []string{"1.0.0"},
					SourceConfidence: []sdk.SourceConfidence{sdk.SourceConfidenceReported},
				},
				{
					Kind:             scorecard.FactKindWarning,
					SchemaVersions:   []string{"1.0.0"},
					SourceConfidence: []sdk.SourceConfidence{sdk.SourceConfidenceReported},
				},
			},
		},
		Config: map[string]any{
			"source": map[string]any{
				"input":     filepath.Join("..", "..", "testdata", "complete.json"),
				"sourceURI": "https://api.securityscorecards.dev/projects/github.com/example/widgets",
			},
		},
	}
	var stdin bytes.Buffer
	if err := json.NewEncoder(&stdin).Encode(request); err != nil {
		t.Fatalf("encode request: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := run([]string{"--sdk-stdio"}, &stdin, &stdout, &stderr); err != nil {
		t.Fatalf("run() error = %v stderr=%s", err, stderr.String())
	}
	var result sdk.Result
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("decode result: %v\nstdout=%s", err, stdout.String())
	}
	if !reflect.DeepEqual(result.Claim, claim) {
		t.Fatalf("result claim = %#v, want %#v", result.Claim, claim)
	}
	if got, want := result.Generation.ID, claim.GenerationID; got != want {
		t.Fatalf("generation ID = %q, want %q", got, want)
	}
	if len(result.Facts) == 0 {
		t.Fatal("result emitted no facts")
	}
	// The OCI reference-image proof (#1980) runs this exact stdin contract inside
	// a digest-pinned container and asserts all three declared fact families come
	// back, so guard them here at the program boundary too.
	families := map[string]int{}
	for _, fact := range result.Facts {
		families[fact.Kind]++
	}
	for _, kind := range []string{scorecard.FactKindSnapshot, scorecard.FactKindCheck, scorecard.FactKindWarning} {
		if families[kind] == 0 {
			t.Fatalf("result missing fact family %q; emitted families = %v", kind, families)
		}
	}
}
