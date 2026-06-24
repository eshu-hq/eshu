// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"reflect"
	"testing"
)

func TestInvestigationWorkflowCatalogIncludesGuidedDomains(t *testing.T) {
	t.Parallel()

	seen := map[string]InvestigationWorkflow{}
	for _, workflow := range InvestigationWorkflowCatalog() {
		seen[workflow.ID] = workflow
	}
	for _, id := range []string{
		"guided_vulnerable_dependency",
		"guided_deployable_drift",
		"guided_incident_context",
	} {
		workflow, ok := seen[id]
		if !ok {
			t.Fatalf("workflow %q missing from catalog", id)
		}
		if workflow.OutputPacket.Schema == "" {
			t.Fatalf("workflow %q missing output packet schema", id)
		}
		if len(workflow.RequiredEvidence) == 0 {
			t.Fatalf("workflow %q missing required evidence", id)
		}
		if len(workflow.OptionalEvidence) == 0 {
			t.Fatalf("workflow %q missing optional evidence", id)
		}
		if len(workflow.ToolGroups) == 0 {
			t.Fatalf("workflow %q missing grouped atomic tools", id)
		}
		if len(workflow.StarterPrompts) == 0 {
			t.Fatalf("workflow %q missing starter prompts", id)
		}
	}
}

func TestInvestigationWorkflowResolveUsesMissingEvidenceForNextCalls(t *testing.T) {
	t.Parallel()

	workflow, ok := LookupInvestigationWorkflow("guided_vulnerable_dependency")
	if !ok {
		t.Fatal("guided_vulnerable_dependency missing")
	}
	resolved, err := workflow.Resolve(InvestigationWorkflowResolveInput{
		Inputs: map[string]string{
			"advisory_id": "CVE-2026-0001",
			"repo_id":     "repo-payments",
			"subject":     "CVE-2026-0001",
		},
		MissingEvidence: []string{"sbom", "workload"},
	})
	if err != nil {
		t.Fatalf("Resolve error = %v", err)
	}

	tools := map[string]ResolvedWorkflowCall{}
	for _, call := range resolved.RecommendedNextCalls {
		tools[call.Tool] = call
	}
	for _, want := range []string{"list_sbom_attestation_attachments", "list_supply_chain_impact_findings"} {
		if _, ok := tools[want]; !ok {
			t.Fatalf("missing next call %q in %#v", want, resolved.RecommendedNextCalls)
		}
	}
	sbom := tools["list_sbom_attestation_attachments"]
	if got, want := sbom.Arguments["repository_id"], "repo-payments"; got != want {
		t.Fatalf("sbom repository_id = %#v, want %#v", got, want)
	}
	finding := tools["list_supply_chain_impact_findings"]
	if got, want := finding.Arguments["advisory_id"], "CVE-2026-0001"; got != want {
		t.Fatalf("finding advisory_id = %#v, want %#v", got, want)
	}
	if got, want := finding.Arguments["limit"], 10; got != want {
		t.Fatalf("finding limit = %#v, want %#v", got, want)
	}
}

func TestVulnerableDependencyWorkflowCoversEvidenceAndFailureModes(t *testing.T) {
	t.Parallel()

	workflow, ok := LookupInvestigationWorkflow("guided_vulnerable_dependency")
	if !ok {
		t.Fatal("guided_vulnerable_dependency missing")
	}

	evidenceKeys := map[string]struct{}{}
	for _, evidence := range workflow.RequiredEvidence {
		evidenceKeys[evidence.Key] = struct{}{}
	}
	for _, evidence := range workflow.OptionalEvidence {
		evidenceKeys[evidence.Key] = struct{}{}
	}
	for _, key := range []string{"advisory", "package", "impact", "scanner", "sbom", "image", "workload", "service", "owner", "freshness"} {
		if _, ok := evidenceKeys[key]; !ok {
			t.Fatalf("workflow evidence keys missing %q in %#v", key, evidenceKeys)
		}
	}

	sections := map[string]struct{}{}
	for _, section := range workflow.OutputPacket.Sections {
		sections[section] = struct{}{}
	}
	for _, section := range []string{"scanner", "image", "service", "owner", "refusal_reasons", "missing_evidence", "recommended_next_calls"} {
		if _, ok := sections[section]; !ok {
			t.Fatalf("output sections missing %q in %#v", section, workflow.OutputPacket.Sections)
		}
	}

	failureModes := map[string]WorkflowFailureMode{}
	for _, mode := range workflow.FailureModes {
		failureModes[mode.Condition] = mode
	}
	for _, condition := range []string{"scanner_absent", "sbom_absent", "stale_generation", "permission_hidden"} {
		mode, ok := failureModes[condition]
		if !ok {
			t.Fatalf("failure mode %q missing in %#v", condition, workflow.FailureModes)
		}
		if mode.Meaning == "" || mode.RecommendedAction == "" {
			t.Fatalf("failure mode %q = %#v, want meaning and recommended action", condition, mode)
		}
	}
}

func TestVulnerableDependencyWorkflowResolvesScannerImageServiceAndOwnerGaps(t *testing.T) {
	t.Parallel()

	workflow, ok := LookupInvestigationWorkflow("guided_vulnerable_dependency")
	if !ok {
		t.Fatal("guided_vulnerable_dependency missing")
	}
	resolved, err := workflow.Resolve(InvestigationWorkflowResolveInput{
		Inputs: map[string]string{
			"finding_id": "finding-1",
			"image_ref":  "registry.example.com/checkout:latest",
			"repo_id":    "repo-checkout",
			"service_id": "checkout",
			"subject":    "CVE-2026-0001",
		},
		MissingEvidence: []string{"scanner", "impact", "image", "service", "owner"},
	})
	if err != nil {
		t.Fatalf("Resolve error = %v", err)
	}

	calls := map[string]ResolvedWorkflowCall{}
	for _, call := range resolved.RecommendedNextCalls {
		calls[call.ID] = call
	}
	wantTools := map[string]string{
		"scanner_contract":   "get_vulnerability_scanner_read_contract",
		"impact_explanation": "explain_supply_chain_impact",
		"image_identity":     "list_container_image_identities",
		"service_story":      "get_service_story",
		"owner_correlation":  "list_service_catalog_correlations",
	}
	for id, tool := range wantTools {
		call, ok := calls[id]
		if !ok {
			t.Fatalf("missing call %q in %#v", id, resolved.RecommendedNextCalls)
		}
		if call.Tool != tool {
			t.Fatalf("call %q tool = %q, want %q", id, call.Tool, tool)
		}
		if call.ExpectedEvidence == "" {
			t.Fatalf("call %q expected evidence is empty", id)
		}
	}
	if got, want := calls["image_identity"].Arguments["image_ref"], "registry.example.com/checkout:latest"; got != want {
		t.Fatalf("image_ref = %#v, want %#v", got, want)
	}
	if got, want := calls["impact_explanation"].Arguments["finding_id"], "finding-1"; got != want {
		t.Fatalf("finding_id = %#v, want %#v", got, want)
	}
	if got, want := calls["owner_correlation"].Arguments["repository_id"], "repo-checkout"; got != want {
		t.Fatalf("owner repository_id = %#v, want %#v", got, want)
	}
}

func TestVulnerableDependencyWorkflowBlocksUnanchoredEvidenceCalls(t *testing.T) {
	t.Parallel()

	workflow, ok := LookupInvestigationWorkflow("guided_vulnerable_dependency")
	if !ok {
		t.Fatal("guided_vulnerable_dependency missing")
	}
	resolved, err := workflow.Resolve(InvestigationWorkflowResolveInput{
		Inputs: map[string]string{
			"subject": "CVE-2026-0001",
		},
		MissingEvidence: []string{"image", "owner"},
	})
	if err != nil {
		t.Fatalf("Resolve error = %v", err)
	}
	if len(resolved.RecommendedNextCalls) != 0 {
		t.Fatalf("recommended calls = %#v, want none without image/service/repository/owner anchors", resolved.RecommendedNextCalls)
	}
	blocked := map[string]BlockedWorkflowCall{}
	for _, call := range resolved.BlockedNextCalls {
		blocked[call.ID] = call
	}
	for _, id := range []string{"image_identity", "owner_correlation"} {
		call, ok := blocked[id]
		if !ok {
			t.Fatalf("blocked call %q missing in %#v", id, resolved.BlockedNextCalls)
		}
		if len(call.RequiredInputsAny) == 0 {
			t.Fatalf("blocked call %q required inputs empty", id)
		}
	}
}

func TestVulnerableDependencyWorkflowRequiresAdvisoryAnchorForImpact(t *testing.T) {
	t.Parallel()

	workflow, ok := LookupInvestigationWorkflow("guided_vulnerable_dependency")
	if !ok {
		t.Fatal("guided_vulnerable_dependency missing")
	}
	resolved, err := workflow.Resolve(InvestigationWorkflowResolveInput{
		Inputs: map[string]string{
			"package_id": "pkg-1",
			"repo_id":    "repo-checkout",
			"subject":    "CVE-2026-0001",
		},
		MissingEvidence: []string{"impact"},
	})
	if err != nil {
		t.Fatalf("Resolve error = %v", err)
	}
	if len(resolved.RecommendedNextCalls) != 0 {
		t.Fatalf("recommended calls = %#v, want none without advisory, CVE, or finding anchor", resolved.RecommendedNextCalls)
	}
	if len(resolved.BlockedNextCalls) != 1 {
		t.Fatalf("blocked calls = %#v, want impact explanation blocked", resolved.BlockedNextCalls)
	}
	call := resolved.BlockedNextCalls[0]
	if call.ID != "impact_explanation" {
		t.Fatalf("blocked call ID = %q, want impact_explanation", call.ID)
	}
	want := []string{"advisory_id", "cve_id", "finding_id"}
	if !reflect.DeepEqual(call.RequiredInputsAny, want) {
		t.Fatalf("required inputs = %#v, want %#v", call.RequiredInputsAny, want)
	}
}

func TestInvestigationWorkflowResolveIsDeterministicAndReportsUnmatchedEvidence(t *testing.T) {
	t.Parallel()

	workflow, ok := LookupInvestigationWorkflow("guided_incident_context")
	if !ok {
		t.Fatal("guided_incident_context missing")
	}
	input := InvestigationWorkflowResolveInput{
		Inputs: map[string]string{
			"incident_id": "INC-1",
			"service_id":  "checkout",
		},
		MissingEvidence: []string{"observability", "unknown-family"},
	}
	first, err := workflow.Resolve(input)
	if err != nil {
		t.Fatalf("first Resolve error = %v", err)
	}
	second, err := workflow.Resolve(input)
	if err != nil {
		t.Fatalf("second Resolve error = %v", err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("Resolve is not deterministic:\n%#v\n%#v", first, second)
	}
	if got, want := first.UnmatchedMissingEvidence, []string{"unknown-family"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("unmatched evidence = %#v, want %#v", got, want)
	}
	if len(first.RecommendedNextCalls) == 0 || first.RecommendedNextCalls[0].Tool != "list_observability_coverage_correlations" {
		t.Fatalf("first next call = %#v, want observability correlation call", first.RecommendedNextCalls)
	}
	if got, want := first.RecommendedNextCalls[0].Arguments["target_service_ref"], "checkout"; got != want {
		t.Fatalf("observability target_service_ref = %#v, want %#v", got, want)
	}
	if _, ok := first.RecommendedNextCalls[0].Arguments["service_id"]; ok {
		t.Fatalf("observability arguments contain unsupported service_id: %#v", first.RecommendedNextCalls[0].Arguments)
	}
}

func TestInvestigationWorkflowResolveIncidentChangesUsesCICDCorrelation(t *testing.T) {
	t.Parallel()

	workflow, ok := LookupInvestigationWorkflow("guided_incident_context")
	if !ok {
		t.Fatal("guided_incident_context missing")
	}
	resolved, err := workflow.Resolve(InvestigationWorkflowResolveInput{
		Inputs: map[string]string{
			"incident_id": "INC-1",
			"repo_id":     "repo-checkout",
			"environment": "prod",
			"service_id":  "checkout",
		},
		MissingEvidence: []string{"changes"},
	})
	if err != nil {
		t.Fatalf("Resolve error = %v", err)
	}
	if len(resolved.RecommendedNextCalls) != 1 {
		t.Fatalf("next calls = %#v, want one changes call", resolved.RecommendedNextCalls)
	}
	call := resolved.RecommendedNextCalls[0]
	if got, want := call.Tool, "list_ci_cd_run_correlations"; got != want {
		t.Fatalf("tool = %q, want %q", got, want)
	}
	if got, want := call.Arguments["repository_id"], "repo-checkout"; got != want {
		t.Fatalf("repository_id = %#v, want %#v", got, want)
	}
	if got, want := call.Arguments["environment"], "prod"; got != want {
		t.Fatalf("environment = %#v, want %#v", got, want)
	}
}

func TestInvestigationWorkflowDogfoodRoutesFewerCallsThanAtomicOnly(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		workflowID      string
		inputs          map[string]string
		missingEvidence []string
	}{
		{
			name:       "vulnerable dependency prompt",
			workflowID: "guided_vulnerable_dependency",
			inputs: map[string]string{
				"subject": "CVE-2026-0001",
				"repo_id": "repo-checkout",
			},
			missingEvidence: []string{"sbom", "workload"},
		},
		{
			name:       "deployable drift prompt",
			workflowID: "guided_deployable_drift",
			inputs: map[string]string{
				"deployable_unit_id": "workload:checkout",
				"generation_id":      "gen-1",
				"repo_id":            "repo-checkout",
				"scope_id":           "scope-1",
			},
			missingEvidence: []string{"admission", "runtime"},
		},
		{
			name:       "incident context prompt",
			workflowID: "guided_incident_context",
			inputs: map[string]string{
				"environment": "prod",
				"incident_id": "INC-1",
				"repo_id":     "repo-checkout",
				"service_id":  "checkout",
			},
			missingEvidence: []string{"incident", "observability", "changes"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			workflow, ok := LookupInvestigationWorkflow(tt.workflowID)
			if !ok {
				t.Fatalf("workflow %q missing", tt.workflowID)
			}
			resolved, err := workflow.Resolve(InvestigationWorkflowResolveInput{
				Inputs:          tt.inputs,
				MissingEvidence: tt.missingEvidence,
			})
			if err != nil {
				t.Fatalf("Resolve error = %v", err)
			}

			atomicOnlyChoices := 0
			for _, group := range workflow.ToolGroups {
				atomicOnlyChoices += len(group.Tools)
			}
			if len(resolved.RecommendedNextCalls) >= atomicOnlyChoices {
				t.Fatalf("resolved calls = %d, want fewer than grouped atomic choices %d", len(resolved.RecommendedNextCalls), atomicOnlyChoices)
			}
			gotEvidence := map[string]struct{}{}
			for _, call := range resolved.RecommendedNextCalls {
				gotEvidence[call.MissingEvidence] = struct{}{}
				if call.ExpectedEvidence == "" {
					t.Fatalf("call %#v missing expected evidence", call)
				}
			}
			for _, key := range tt.missingEvidence {
				if _, ok := gotEvidence[key]; !ok {
					t.Fatalf("resolved calls missing evidence key %q in %#v", key, resolved.RecommendedNextCalls)
				}
			}
		})
	}
}

func TestInvestigationWorkflowResolveRejectsMissingRequiredInput(t *testing.T) {
	t.Parallel()

	workflow, ok := LookupInvestigationWorkflow("guided_deployable_drift")
	if !ok {
		t.Fatal("guided_deployable_drift missing")
	}
	if _, err := workflow.Resolve(InvestigationWorkflowResolveInput{}); err == nil {
		t.Fatal("Resolve error = nil, want missing required input error")
	}
}
