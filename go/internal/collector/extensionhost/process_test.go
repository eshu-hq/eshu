// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extensionhost

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	sdkcollector "github.com/eshu-hq/eshu/sdk/go/collector"
)

func TestProcessRunnerLaunchesJSONRequestAndDecodesResult(t *testing.T) {
	if os.Getenv("ESHU_EXTENSIONHOST_PROCESS_HELPER") == "complete" {
		processRunnerHelper(t)
		os.Exit(0)
	}
	t.Parallel()

	item := testWorkItem()
	request := Request{
		ProtocolVersion: sdkcollector.ProtocolVersionV1Alpha1,
		Claim:           testSDKClaim(item),
		Contract:        testContract(),
		Config: map[string]any{
			"fixture": "scorecard",
		},
	}
	runner := ProcessRunner{
		Command: os.Args[0],
		Args:    []string{"-test.run=TestProcessRunnerLaunchesJSONRequestAndDecodesResult"},
		Env:     []string{"ESHU_EXTENSIONHOST_PROCESS_HELPER=complete"},
	}

	result, err := runner.RunCollector(context.Background(), request)
	if err != nil {
		t.Fatalf("RunCollector() error = %v, want nil", err)
	}
	if got, want := result.ProtocolVersion, sdkcollector.ProtocolVersionV1Alpha1; got != want {
		t.Fatalf("ProtocolVersion = %q, want %q", got, want)
	}
	if got, want := result.Claim.WorkItemID, request.Claim.WorkItemID; got != want {
		t.Fatalf("Claim.WorkItemID = %q, want %q", got, want)
	}
}

func TestProcessRunnerRejectsOversizedStdout(t *testing.T) {
	if os.Getenv("ESHU_EXTENSIONHOST_PROCESS_HELPER") == "oversized" {
		_, _ = os.Stdout.WriteString(strings.Repeat("x", 128))
		os.Exit(0)
	}
	t.Parallel()

	runner := ProcessRunner{
		Command:          os.Args[0],
		Args:             []string{"-test.run=TestProcessRunnerRejectsOversizedStdout"},
		Env:              []string{"ESHU_EXTENSIONHOST_PROCESS_HELPER=oversized"},
		StdoutLimitBytes: 32,
	}

	_, err := runner.RunCollector(context.Background(), Request{})
	if err == nil {
		t.Fatal("RunCollector() error = nil, want stdout limit error")
	}
	if !strings.Contains(err.Error(), "stdout limit") {
		t.Fatalf("RunCollector() error = %v, want stdout limit", err)
	}
}

func TestProcessRunnerRejectsTrailingJSON(t *testing.T) {
	if os.Getenv("ESHU_EXTENSIONHOST_PROCESS_HELPER") == "trailing" {
		_, _ = os.Stdout.WriteString(`{"protocol_version":"collector-sdk/v1alpha1"}{}`)
		os.Exit(0)
	}
	t.Parallel()

	runner := ProcessRunner{
		Command: os.Args[0],
		Args:    []string{"-test.run=TestProcessRunnerRejectsTrailingJSON"},
		Env:     []string{"ESHU_EXTENSIONHOST_PROCESS_HELPER=trailing"},
	}

	_, err := runner.RunCollector(context.Background(), Request{})
	if err == nil {
		t.Fatal("RunCollector() error = nil, want trailing JSON error")
	}
	if !strings.Contains(err.Error(), "trailing JSON") {
		t.Fatalf("RunCollector() error = %v, want trailing JSON", err)
	}
}

func processRunnerHelper(t *testing.T) {
	t.Helper()

	var request Request
	if err := json.NewDecoder(os.Stdin).Decode(&request); err != nil {
		t.Fatalf("Decode(stdin) error = %v, want nil", err)
	}
	result := sdkcollector.Result{
		ProtocolVersion: request.ProtocolVersion,
		State:           sdkcollector.ResultComplete,
		Claim:           request.Claim,
		Generation: sdkcollector.Generation{
			ID:         request.Claim.GenerationID,
			ObservedAt: testObservedAt(),
		},
		Statuses: []sdkcollector.Status{{
			Class: sdkcollector.StatusComplete,
		}},
	}
	if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
		t.Fatalf("Encode(stdout) error = %v, want nil", err)
	}
}

func testContract() sdkcollector.Contract {
	return sdkcollector.Contract{
		ProtocolVersion: sdkcollector.ProtocolVersionV1Alpha1,
		Facts: []sdkcollector.FactDeclaration{{
			Kind:             "dev.eshu.examples.scorecard.check",
			SchemaVersions:   []string{"1.0.0"},
			SourceConfidence: []sdkcollector.SourceConfidence{sdkcollector.SourceConfidenceReported},
		}},
	}
}
