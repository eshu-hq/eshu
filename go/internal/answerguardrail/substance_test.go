// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package answerguardrail

import "testing"

func TestIsCircularAnswer(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		question string
		answer   string
		want     bool
	}{
		{
			name:     "identity restatement is circular",
			question: "give me an overview of the payments service",
			answer:   "The payments service is a service named payments.",
			want:     true,
		},
		{
			name:     "workload tautology is circular",
			question: "what is the checkout workload",
			answer:   "checkout is a workload called checkout",
			want:     true,
		},
		{
			name:     "operational facts are substantive",
			question: "give me an overview of the payments service",
			answer:   "payments runs from repository payments-api with 3 deployments across prod and staging.",
			want:     false,
		},
		{
			name:     "a single new fact is substantive",
			question: "what is the payments service",
			answer:   "payments exposes 5 REST endpoints",
			want:     false,
		},
		{
			name:     "empty answer is not circular",
			question: "what is payments",
			answer:   "",
			want:     false,
		},
		{
			name:     "only scaffolding is identity-only",
			question: "describe billing",
			answer:   "It is a service.",
			want:     true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := IsCircularAnswer(tc.question, tc.answer); got != tc.want {
				t.Fatalf("IsCircularAnswer(%q, %q) = %v, want %v", tc.question, tc.answer, got, tc.want)
			}
		})
	}
}

func TestValidateResultRejectsCircularAnswer(t *testing.T) {
	t.Parallel()

	// A supported, cited, publish-safe answer that is still circular must fail the
	// answer-substance criterion.
	verdict := ValidateResult(Result{
		Question:        "give me an overview of the payments service",
		AnswerSummary:   "The payments service is a service named payments.",
		Supported:       true,
		TruthProvenance: true,
	})
	if verdict.Valid {
		t.Fatal("circular supported answer passed guardrails, want answer_substance finding")
	}
	if !verdict.HasFinding(CriterionAnswerSubstance) {
		t.Errorf("verdict missing answer_substance finding: %+v", verdict.Findings)
	}

	// A substantive answer with the same coverage passes.
	ok := ValidateResult(Result{
		Question:        "give me an overview of the payments service",
		AnswerSummary:   "payments runs from repository payments-api with 3 deployments across prod and staging.",
		Supported:       true,
		TruthProvenance: true,
	})
	if !ok.Valid {
		t.Errorf("substantive answer failed guardrails: %+v", ok.Findings)
	}

	// Without a Question the substance check is disabled (back-compat).
	noQuestion := ValidateResult(Result{
		AnswerSummary:   "payments is payments",
		Supported:       true,
		TruthProvenance: true,
	})
	if noQuestion.HasFinding(CriterionAnswerSubstance) {
		t.Error("substance check ran without a Question; want disabled")
	}
}
