// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/cigates"
)

func TestRequiredGates_SelectsPRAttributionGateForAnyChangedPath(t *testing.T) {
	t.Parallel()

	registryPath := filepath.Join("..", "..", "..", "specs", "ci-gates.v1.yaml")
	reg, err := cigates.Load(registryPath)
	if err != nil {
		t.Fatalf("Load(%q): %v", registryPath, err)
	}
	required, err := reg.RequiredGates([]string{"unlisted-surface/example.txt"})
	if err != nil {
		t.Fatalf("RequiredGates returned error: %v", err)
	}
	for _, gate := range required {
		for _, id := range gate.GateIDs {
			if id == "no-ai-attribution" {
				return
			}
		}
	}
	t.Fatal("PR-wide no-ai-attribution gate was not selected for an unrelated changed path")
}

func TestRequiredGates_SelectsReplayTierForReducerReplayProof(t *testing.T) {
	t.Parallel()

	registryPath := filepath.Join("..", "..", "..", "specs", "ci-gates.v1.yaml")
	reg, err := cigates.Load(registryPath)
	if err != nil {
		t.Fatalf("Load(%q): %v", registryPath, err)
	}
	required, err := reg.RequiredGates([]string{
		"go/internal/reducer/provenance_replay_tombstone_live_test.go",
	})
	if err != nil {
		t.Fatalf("RequiredGates returned error: %v", err)
	}
	for _, gate := range required {
		for _, id := range gate.GateIDs {
			if id == "replay-tier" {
				workflowPath := filepath.Join("..", "..", "..", ".github", "workflows", "verify-replay-tier.yml")
				workflow, readErr := os.ReadFile(workflowPath)
				if readErr != nil {
					t.Fatalf("ReadFile(%q): %v", workflowPath, readErr)
				}
				if !strings.Contains(string(workflow), "      - 'go/internal/reducer/**'") {
					t.Fatal("verify-replay-tier workflow does not watch go/internal/reducer/**")
				}
				return
			}
		}
	}
	t.Fatal("reducer replay proof did not select replay-tier")
}

func TestRequiredGates_SelectsEveryMatchingBlockingCIGate(t *testing.T) {
	t.Parallel()

	reg := &cigates.Registry{Gates: []cigates.Gate{
		{
			ID:       "local-contract",
			Blocking: true,
			Tier:     cigates.TierPrePR,
			Triggers: []string{"go/**"},
			Local:    &cigates.Local{Command: "true"},
			CI:       cigates.CI{Workflow: "test.yml", Job: "verify-contracts"},
		},
		{
			ID:           "ci-heavy-race",
			Blocking:     true,
			Tier:         cigates.TierCIHeavy,
			Triggers:     []string{"go/**"},
			CI:           cigates.CI{Workflow: "race.yml", Job: "race"},
			CIOnlyReason: "hosted runner",
		},
		{
			ID:       "same-contract-job",
			Blocking: true,
			Tier:     cigates.TierPreCommit,
			Triggers: []string{"go/**/*.go"},
			Local:    &cigates.Local{Command: "true"},
			CI:       cigates.CI{Workflow: "test.yml", Job: "verify-contracts"},
		},
		{
			ID:       "advisory",
			Blocking: false,
			Tier:     cigates.TierPrePR,
			Triggers: []string{"go/**"},
			Local:    &cigates.Local{Command: "true"},
			CI:       cigates.CI{Workflow: "advisory.yml", Job: "advisory"},
		},
		{
			ID:       "unmatched",
			Blocking: true,
			Tier:     cigates.TierPrePR,
			Triggers: []string{"web/**"},
			Local:    &cigates.Local{Command: "true"},
			CI:       cigates.CI{Workflow: "frontend.yml", Job: "frontend"},
		},
	}}

	got, err := reg.RequiredGates([]string{"go/internal/cigates/required.go"})
	if err != nil {
		t.Fatalf("RequiredGates returned error: %v", err)
	}
	want := []cigates.RequiredGate{
		{
			Workflow:   "test.yml",
			Job:        "verify-contracts",
			CheckNames: nil,
			GateIDs:    []string{"local-contract", "same-contract-job"},
		},
		{
			Workflow:   "race.yml",
			Job:        "race",
			CheckNames: nil,
			GateIDs:    []string{"ci-heavy-race"},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("RequiredGates = %#v; want %#v", got, want)
	}
}

func TestRequiredGates_RejectsSelectedBlockingGateWithoutCIJob(t *testing.T) {
	t.Parallel()

	reg := &cigates.Registry{Gates: []cigates.Gate{{
		ID:       "decorative-blocker",
		Blocking: true,
		Tier:     cigates.TierPrePR,
		Triggers: []string{"docs/**"},
		Local:    &cigates.Local{Command: "true"},
	}}}

	_, err := reg.RequiredGates([]string{"docs/index.md"})
	if err == nil {
		t.Fatal("RequiredGates should reject a selected blocking gate without a CI job")
	}
	if !strings.Contains(err.Error(), "decorative-blocker") {
		t.Fatalf("error %q should name the unreachable blocking gate", err)
	}
}

func TestRequiredGates_RejectsConflictingConcreteCheckNames(t *testing.T) {
	t.Parallel()

	reg := &cigates.Registry{Gates: []cigates.Gate{
		{
			ID:       "first-matrix-gate",
			Blocking: true,
			Triggers: []string{"go/**"},
			CI: cigates.CI{
				Workflow:   "e2e.yml",
				Job:        "test",
				CheckNames: []string{"test (nornicdb)"},
			},
		},
		{
			ID:       "second-matrix-gate",
			Blocking: true,
			Triggers: []string{"go/**"},
			CI: cigates.CI{
				Workflow:   "e2e.yml",
				Job:        "test",
				CheckNames: []string{"test (neo4j)"},
			},
		},
	}}

	_, err := reg.RequiredGates([]string{"go/main.go"})
	if err == nil {
		t.Fatal("RequiredGates should reject conflicting check_names for one workflow/job")
	}
	if !strings.Contains(err.Error(), "check_names") {
		t.Fatalf("error %q should identify conflicting check_names", err)
	}
}
