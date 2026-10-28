// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestReadinessHelpersRouteToProof(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	registry, err := Load(filepath.Join(root, "specs/ci-gates.v1.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var gate *Gate
	for i := range registry.Gates {
		if registry.Gates[i].ID == "live-postgres-readiness" {
			gate = &registry.Gates[i]
			break
		}
	}
	if gate == nil {
		t.Fatal("readiness gate missing")
	}
	fragment, err := os.ReadFile(filepath.Join(root, "specs/ci-pr-workflows.d/live-postgres-readiness.yml"))
	if err != nil {
		t.Fatal(err)
	}
	definitions, err := ParsePRPlanFragment(fragment)
	if err != nil || len(definitions) != 1 {
		t.Fatalf("readiness inventory definitions=%d err=%v", len(definitions), err)
	}
	workflow, err := os.ReadFile(filepath.Join(root, ".github/workflows/live-postgres-readiness.yml"))
	if err != nil {
		t.Fatal(err)
	}
	definition, err := ValidateCallablePRWorkflow(definitions[0], workflow)
	if err != nil {
		t.Fatal(err)
	}
	smoke, err := PlanPRWorkflows([]PRWorkflowDefinition{definition},
		[]string{"scripts/lib/live_postgres_readiness_v2.py"}, false, "main", "smoke",
		map[string][]string{definition.File: {readinessAggregateJob}})
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"readiness-plan", readinessShardJob, readinessAggregateJob} {
		if !slices.Contains(smoke[definition.File], required) {
			t.Fatalf("smoke aggregate omits dependency %s: %v", required, smoke[definition.File])
		}
	}
	var parsed readinessWorkflow
	if err := yaml.Unmarshal(workflow, &parsed); err != nil {
		t.Fatal(err)
	}
	for _, helper := range []string{
		"scripts/lib/live_postgres_readiness_batch_mutant.py",
		"scripts/lib/live_postgres_readiness_v2.py",
		"scripts/lib/live_postgres_readiness_v2_planner.py",
		"scripts/lib/live_postgres_readiness_v2_cases.py",
		"scripts/lib/live_postgres_readiness_reports.py",
		"scripts/lib/live_postgres_readiness_refresh_v2.py",
		"scripts/lib/live_postgres_readiness_refresh_v2_provenance.py",
		"scripts/test-live-postgres-readiness-refresh-v2.py",
		"specs/live-postgres-readiness-weights.v2.json",
	} {
		t.Run(helper, func(t *testing.T) {
			planned, err := PlanPRWorkflows([]PRWorkflowDefinition{definition}, []string{helper}, false, "main", "full", nil)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(planned[definition.File], definition.Jobs) {
				t.Fatalf("helper-only PR planned jobs=%v; want %v", planned[definition.File], definition.Jobs)
			}
			registered := func(patterns []string) bool {
				for _, pattern := range patterns {
					if MatchGlob(pattern, helper) {
						return true
					}
				}
				return false
			}
			if !registered(gate.Triggers) || !registered(gate.SelfTestTriggers) {
				t.Fatalf("helper must select readiness gate and its hermetic self-test: triggers=%v self-test=%v", gate.Triggers, gate.SelfTestTriggers)
			}
			gateIDs, err := SelectPRGateIDs(registry, []PRWorkflowDefinition{definition}, []string{helper}, false, "full", planned)
			if err != nil || !slices.Contains(gateIDs, gate.ID) {
				t.Fatalf("helper-only PR selected gates=%v err=%v; want %s", gateIDs, err, gate.ID)
			}
			if !slices.Contains(parsed.On["push"].Paths, helper) {
				t.Fatal("main push paths omit readiness helper")
			}
		})
	}
}
