// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"strings"
	"testing"
)

func TestCatalogIncludesSecondWavePlaybooks(t *testing.T) {
	t.Parallel()

	want := map[string]string{
		"incident_context_evidence_path":      "1.0.0",
		"supply_chain_impact_explanation":     "1.0.0",
		"secrets_iam_trust_chain_posture":     "1.0.0",
		"incremental_freshness_readiness":     "1.0.0",
		"hosted_onboarding_governance_status": "1.0.0",
		"change_surface_source_investigation": "1.0.0",
	}
	seen := make(map[string]string, len(PlaybookCatalog()))
	for _, pb := range PlaybookCatalog() {
		seen[pb.ID] = pb.Version
	}
	for id, version := range want {
		if got, ok := seen[id]; !ok || got != version {
			t.Fatalf("playbook %q version = %q, present=%t; want %q", id, got, ok, version)
		}
	}
}

func TestSecondWavePlaybooksDeclareAnswerExperienceContracts(t *testing.T) {
	t.Parallel()

	for _, id := range []string{
		"incident_context_evidence_path",
		"supply_chain_impact_explanation",
		"secrets_iam_trust_chain_posture",
		"incremental_freshness_readiness",
		"hosted_onboarding_governance_status",
		"change_surface_source_investigation",
	} {
		id := id
		t.Run(id, func(t *testing.T) {
			t.Parallel()
			pb, ok := LookupPlaybook(id)
			if !ok {
				t.Fatalf("playbook %q missing", id)
			}
			assertPlaybookHasRequiredInput(t, pb)
			assertPlaybookHasBoundedStep(t, pb)
			assertPlaybookCoversFailureModes(t, pb, []string{
				"unsupported",
				"missing evidence",
				"stale",
				"building",
				"truncated",
				"ambiguous",
			})
		})
	}
}

func assertPlaybookHasRequiredInput(t *testing.T, pb QueryPlaybook) {
	t.Helper()

	for _, input := range pb.RequiredInputs {
		if input.Required {
			return
		}
	}
	t.Fatalf("playbook %q has no required input", pb.ID)
}

func assertPlaybookHasBoundedStep(t *testing.T, pb QueryPlaybook) {
	t.Helper()

	for _, step := range pb.Steps {
		for _, param := range step.Params {
			if param.Name == "limit" && param.hasConstInt && param.ConstInt > 0 {
				return
			}
		}
	}
	t.Fatalf("playbook %q has no positive default limit", pb.ID)
}

func assertPlaybookCoversFailureModes(t *testing.T, pb QueryPlaybook, want []string) {
	t.Helper()

	joined := ""
	for _, mode := range pb.FailureModes {
		joined += " " + mode.Condition + " " + mode.Meaning + " " + mode.Fallback
	}
	for _, term := range want {
		if !containsTerm(joined, term) {
			t.Fatalf("playbook %q failure modes do not cover %q: %s", pb.ID, term, joined)
		}
	}
}

func containsTerm(haystack, needle string) bool {
	return strings.Contains(strings.ToLower(haystack), strings.ToLower(needle))
}
