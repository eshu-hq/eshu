// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package searchbench

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateRerankProtocolEvaluationAcceptsCompleteEvidence(t *testing.T) {
	t.Parallel()

	evaluation := validRerankProtocolEvaluation(t)

	if err := ValidateRerankProtocolEvaluation(evaluation); err != nil {
		t.Fatalf("ValidateRerankProtocolEvaluation() error = %v, want nil", err)
	}
}

func TestValidateRerankProtocolEvaluationAllowsAcceptedStopReasonWithoutEvidence(t *testing.T) {
	t.Parallel()

	evaluation := RerankProtocolEvaluation{
		Version:            RerankProtocolEvaluationVersion,
		AcceptedStopReason: "Issue #421 stopped before reranking because issue #417 has no measured NornicDB hybrid baseline.",
	}

	if err := ValidateRerankProtocolEvaluation(evaluation); err != nil {
		t.Fatalf("ValidateRerankProtocolEvaluation() error = %v, want nil", err)
	}
}

func TestValidateRerankProtocolEvaluationIgnoresWhitespaceOnlyBaselineEvidenceForStopReason(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		mutate func(*RerankProtocolEvaluation)
	}{
		{
			name: "top-level baseline",
			mutate: func(evaluation *RerankProtocolEvaluation) {
				evaluation.BaselineHybridEvidence.Backend = Backend(" \t\n")
				evaluation.BaselineHybridEvidence.Mode = Mode(" \t\n")
			},
		},
		{
			name: "rerank baseline",
			mutate: func(evaluation *RerankProtocolEvaluation) {
				evaluation.RerankEvaluation.BaselineHybridEvidence.Backend = Backend(" \t\n")
				evaluation.RerankEvaluation.BaselineHybridEvidence.Mode = Mode(" \t\n")
			},
		},
		{
			name: "protocol baseline",
			mutate: func(evaluation *RerankProtocolEvaluation) {
				evaluation.ProtocolRecommendation.BaselineHybridEvidence.Backend = Backend(" \t\n")
				evaluation.ProtocolRecommendation.BaselineHybridEvidence.Mode = Mode(" \t\n")
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			evaluation := RerankProtocolEvaluation{
				Version:            RerankProtocolEvaluationVersion,
				AcceptedStopReason: "Issue #421 stopped before reranking because issue #417 has no measured NornicDB hybrid baseline.",
			}
			tc.mutate(&evaluation)

			if err := ValidateRerankProtocolEvaluation(evaluation); err != nil {
				t.Fatalf("ValidateRerankProtocolEvaluation() error = %v, want nil", err)
			}
		})
	}
}

func TestValidateRerankProtocolEvaluationRejectsStopReasonWithoutIssueLinks(t *testing.T) {
	t.Parallel()

	evaluation := RerankProtocolEvaluation{
		Version:            RerankProtocolEvaluationVersion,
		AcceptedStopReason: "live adapters were missing",
	}

	err := ValidateRerankProtocolEvaluation(evaluation)
	if err == nil {
		t.Fatal("ValidateRerankProtocolEvaluation() error = nil, want issue-link failure")
	}
	for _, want := range []string{
		"accepted_stop_reason must reference issue #421",
		"accepted_stop_reason must reference issue #417",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("ValidateRerankProtocolEvaluation() error = %q, want substring %q", err, want)
		}
	}
}

func TestValidateRerankProtocolEvaluationRejectsStopReasonWithEvidence(t *testing.T) {
	t.Parallel()

	evaluation := validRerankProtocolEvaluation(t)
	evaluation.AcceptedStopReason = "Issue #421 stopped because issue #417 has no measured baseline."

	err := ValidateRerankProtocolEvaluation(evaluation)
	if err == nil {
		t.Fatal("ValidateRerankProtocolEvaluation() error = nil, want stop-reason exclusivity failure")
	}
	if want := "accepted_stop_reason cannot be set when baseline, rerank, or protocol evidence is present"; !strings.Contains(err.Error(), want) {
		t.Fatalf("ValidateRerankProtocolEvaluation() error = %q, want substring %q", err, want)
	}
}

func TestValidateRerankProtocolEvaluationRejectsMismatchedBaselineEvidence(t *testing.T) {
	t.Parallel()

	evaluation := validRerankProtocolEvaluation(t)
	evaluation.ProtocolRecommendation.BaselineHybridEvidence.EvidenceID = "search-benchmark:hybrid:v2"

	err := ValidateRerankProtocolEvaluation(evaluation)
	if err == nil {
		t.Fatal("ValidateRerankProtocolEvaluation() error = nil, want baseline mismatch failure")
	}
	if want := "protocol_recommendation.baseline_hybrid_evidence must match baseline_hybrid_evidence"; !strings.Contains(err.Error(), want) {
		t.Fatalf("ValidateRerankProtocolEvaluation() error = %q, want substring %q", err, want)
	}
}

func TestValidateRerankProtocolEvaluationRejectsMissingEvidenceWithoutStopReason(t *testing.T) {
	t.Parallel()

	evaluation := RerankProtocolEvaluation{Version: RerankProtocolEvaluationVersion}

	err := ValidateRerankProtocolEvaluation(evaluation)
	if err == nil {
		t.Fatal("ValidateRerankProtocolEvaluation() error = nil, want missing evidence failures")
	}
	for _, want := range []string{
		"baseline hybrid evidence is required",
		"rerank_evaluation is required unless accepted_stop_reason is set",
		"protocol_recommendation is required unless accepted_stop_reason is set",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("ValidateRerankProtocolEvaluation() error = %q, want substring %q", err, want)
		}
	}
}

func TestSameRerankBaselineEvidenceTrimsAllIdentityFields(t *testing.T) {
	t.Parallel()

	left := RerankBaselineEvidence{
		EvidenceID: " search-benchmark:hybrid:v1 ",
		Backend:    Backend(" \t" + string(BackendNornicDBHybrid) + "\n"),
		Mode:       Mode("\t" + string(ModeHybrid) + " "),
	}
	right := rerankBaselineHybridEvidence()

	if !sameRerankBaselineEvidence(left, right) {
		t.Fatalf("sameRerankBaselineEvidence(%+v, %+v) = false, want true", left, right)
	}
}

func TestIssue421StoppedEvidenceFileValidates(t *testing.T) {
	t.Parallel()

	path := filepath.Join(
		"..",
		"..",
		"..",
		"docs",
		"public",
		"reference",
		"searchbench-evidence",
		"issue-421-rerank-protocol-evaluation-v1.json",
	)
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("os.ReadFile(%q) error = %v, want nil", path, err)
	}
	var evaluation RerankProtocolEvaluation
	if err := json.Unmarshal(payload, &evaluation); err != nil {
		t.Fatalf("json.Unmarshal(%q) error = %v, want nil", path, err)
	}
	if strings.TrimSpace(evaluation.AcceptedStopReason) == "" {
		t.Fatalf("%s accepted_stop_reason is empty, want explicit stopped-evaluation reason", path)
	}
	if err := ValidateRerankProtocolEvaluation(evaluation); err != nil {
		t.Fatalf("ValidateRerankProtocolEvaluation(%q) error = %v, want nil", path, err)
	}
}

func TestRerankProtocolEvaluationJSONUsesVersionedFieldNames(t *testing.T) {
	t.Parallel()

	payload, err := json.Marshal(validRerankProtocolEvaluation(t))
	if err != nil {
		t.Fatalf("json.Marshal() error = %v, want nil", err)
	}
	body := string(payload)
	for _, want := range []string{
		`"rerank-protocol-evaluation/v1"`,
		`"baseline_hybrid_evidence"`,
		`"rerank_evaluation"`,
		`"protocol_recommendation"`,
		`"accepted_stop_reason"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("json payload = %s, want field/value %s", body, want)
		}
	}
}

func validRerankProtocolEvaluation(t *testing.T) RerankProtocolEvaluation {
	t.Helper()

	rerank, err := ScoreRerankEvaluation(validRerankEvaluationInput())
	if err != nil {
		t.Fatalf("ScoreRerankEvaluation() error = %v, want nil", err)
	}
	baseline := rerankBaselineHybridEvidence()
	protocol := validProtocolRecommendation()
	protocol.BaselineHybridEvidence = baseline
	return RerankProtocolEvaluation{
		Version:                RerankProtocolEvaluationVersion,
		BaselineHybridEvidence: baseline,
		RerankEvaluation:       rerank,
		ProtocolRecommendation: protocol,
	}
}
