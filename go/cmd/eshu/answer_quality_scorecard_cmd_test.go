// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAnswerQualityScorecardCommandPassesCompleteEvidence(t *testing.T) {
	path := writeAnswerQualityFixture(t, completeAnswerQualityEvidenceJSON())
	cmd := newAnswerQualityScorecardCommand()
	cmd.SetArgs([]string{"--from", path, "--json"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("command returned error: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), `"pass": true`) {
		t.Fatalf("JSON output missing pass verdict:\n%s", out.String())
	}
}

func TestAnswerQualityScorecardCommandFailsUnsafeEvidence(t *testing.T) {
	rawAddress := strings.Join([]string{"10", "44", "12", "7"}, ".")
	path := writeAnswerQualityFixture(t, strings.ReplaceAll(
		completeAnswerQualityEvidenceJSON(),
		"useful redacted answer for code_topic",
		"unsafe redacted answer "+rawAddress,
	))
	cmd := newAnswerQualityScorecardCommand()
	cmd.SetArgs([]string{"--from", path})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	err := cmd.Execute()
	if err == nil {
		t.Fatal("command succeeded with unsafe evidence, want error")
	}
	if !strings.Contains(err.Error(), "answer-quality scorecard FAILED") {
		t.Fatalf("error = %v, want scorecard failure", err)
	}
}

func writeAnswerQualityFixture(t *testing.T, raw string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "scorecard.json")
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}

func completeAnswerQualityEvidenceJSON() string {
	return `{
  "version": "answer-quality-scorecard/v1",
  "run_id": "redacted-local-scorecard",
  "eshu_commit": "0123456789abcdef",
  "prompts": [
    {"id":"service-story","family":"service_story","prompt":"Build the service story for service-a and cite it.","expected_truth_class":"deterministic","required_surfaces":["api","mcp"],"required_next_calls":["build_evidence_citation_packet"],"results":[{"surface":"api","useful":true,"supported":true,"answer_summary":"useful redacted answer for service_story","truth_class":"deterministic","freshness":"current","citation_handles":["repo:demo"],"next_calls":["build_evidence_citation_packet"]},{"surface":"mcp","useful":true,"supported":true,"answer_summary":"useful redacted answer for service_story","truth_class":"deterministic","freshness":"current","citation_handles":["repo:demo"],"next_calls":["build_evidence_citation_packet"]}]},
    {"id":"code-topic","family":"code_topic","prompt":"Investigate code topic auth refresh in repo-a.","expected_truth_class":"code_hint","required_surfaces":["api","mcp"],"required_next_calls":["get_code_relationship_story"],"results":[{"surface":"api","useful":true,"supported":true,"answer_summary":"useful redacted answer for code_topic","truth_class":"code_hint","freshness":"current","citation_handles":["repo:demo"],"next_calls":["get_code_relationship_story"]},{"surface":"mcp","useful":true,"supported":true,"answer_summary":"useful redacted answer for code_topic","truth_class":"code_hint","freshness":"current","citation_handles":["repo:demo"],"next_calls":["get_code_relationship_story"]}]},
    {"id":"incident-context","family":"incident_context","prompt":"Summarize incident context for redacted service.","expected_truth_class":"semantic_observation","required_surfaces":["api","mcp"],"required_next_calls":["get_incident_context"],"results":[{"surface":"api","useful":true,"supported":true,"answer_summary":"useful redacted answer for incident_context","truth_class":"semantic_observation","freshness":"current","citation_handles":["incident:redacted"],"next_calls":["get_incident_context"]},{"surface":"mcp","useful":true,"supported":true,"answer_summary":"useful redacted answer for incident_context","truth_class":"semantic_observation","freshness":"current","citation_handles":["incident:redacted"],"next_calls":["get_incident_context"]}]},
    {"id":"supply-chain","family":"supply_chain_impact","prompt":"Explain supply-chain impact for repo-a.","expected_truth_class":"deterministic","required_surfaces":["api","cli"],"required_next_calls":["vuln-scan repo"],"results":[{"surface":"api","useful":true,"supported":true,"answer_summary":"useful redacted answer for supply_chain_impact","truth_class":"deterministic","freshness":"current","citation_handles":["finding:redacted"],"next_calls":["vuln-scan repo"]},{"surface":"cli","useful":true,"supported":true,"answer_summary":"useful redacted answer for supply_chain_impact","truth_class":"deterministic","freshness":"current","citation_handles":["finding:redacted"],"next_calls":["vuln-scan repo"]}]},
    {"id":"documentation-truth","family":"documentation_truth","prompt":"Confirm a documentation finding is current.","expected_truth_class":"semantic_observation","required_surfaces":["api","mcp"],"required_next_calls":["check_documentation_evidence_packet_freshness"],"results":[{"surface":"api","useful":true,"supported":true,"answer_summary":"useful redacted answer for documentation_truth","truth_class":"semantic_observation","freshness":"current","citation_handles":["doc:redacted"],"next_calls":["check_documentation_evidence_packet_freshness"]},{"surface":"mcp","useful":true,"supported":true,"answer_summary":"useful redacted answer for documentation_truth","truth_class":"semantic_observation","freshness":"current","citation_handles":["doc:redacted"],"next_calls":["check_documentation_evidence_packet_freshness"]}]},
    {"id":"freshness-readiness","family":"freshness_readiness","prompt":"Explain readiness and freshness for repo-a.","expected_truth_class":"deterministic","required_surfaces":["api","mcp"],"required_next_calls":["get_index_status"],"results":[{"surface":"api","useful":true,"supported":true,"answer_summary":"useful redacted answer for freshness_readiness","truth_class":"deterministic","freshness":"current","citation_handles":["status:redacted"],"next_calls":["get_index_status"]},{"surface":"mcp","useful":true,"supported":true,"answer_summary":"useful redacted answer for freshness_readiness","truth_class":"deterministic","freshness":"current","citation_handles":["status:redacted"],"next_calls":["get_index_status"]}]},
    {"id":"hosted-governance","family":"hosted_onboarding_governance","prompt":"Explain hosted onboarding governance caveats.","expected_truth_class":"deterministic","required_surfaces":["hosted","cli"],"required_next_calls":["hosted-onboard"],"results":[{"surface":"hosted","useful":true,"supported":true,"answer_summary":"useful redacted answer for hosted_onboarding_governance","truth_class":"deterministic","freshness":"current","citation_handles":["artifact:redacted"],"next_calls":["hosted-onboard"]},{"surface":"cli","useful":true,"supported":true,"answer_summary":"useful redacted answer for hosted_onboarding_governance","truth_class":"deterministic","freshness":"current","citation_handles":["artifact:redacted"],"next_calls":["hosted-onboard"]}]}
  ]
}`
}
