// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"strings"
	"testing"
)

func TestApplyAskSubstanceGuardrailWithholdsCircularAnswer(t *testing.T) {
	t.Parallel()

	resp := &askResponse{
		AnswerProse:     "The payments service is a service named payments.",
		TruthClass:      string(AnswerTruthDerived),
		Artifacts:       []askArtifact{{Format: "markdown", Content: "x"}},
		EvidenceHandles: []evidenceCitationHandle{{Kind: "file", RepoID: "demo"}},
	}
	applyAskSubstanceGuardrail(resp, "give me an overview of the payments service", true)

	if resp.AnswerProse != "" {
		t.Errorf("circular prose was published: %q", resp.AnswerProse)
	}
	if len(resp.Artifacts) != 0 {
		t.Errorf("artifacts survived a withheld circular answer: %+v", resp.Artifacts)
	}
	if !resp.Partial {
		t.Error("withheld circular answer must be marked partial")
	}
	// Publish-safe citation metadata is preserved for follow-up, not dropped.
	if len(resp.EvidenceHandles) == 0 {
		t.Error("evidence handles were dropped; they are publish-safe and should be preserved")
	}
	var hasVerdict, hasNextAction, hasCitationNote bool
	for _, lim := range resp.Limitations {
		if strings.Contains(lim, "answer_substance") {
			hasVerdict = true
		}
		if strings.Contains(lim, "operational facts") {
			hasNextAction = true
		}
		if strings.Contains(lim, "citation metadata") {
			hasCitationNote = true
		}
	}
	if !hasVerdict {
		t.Errorf("missing answer_substance verdict limitation: %v", resp.Limitations)
	}
	if !hasNextAction {
		t.Errorf("missing useful next-action limitation: %v", resp.Limitations)
	}
	if !hasCitationNote {
		t.Errorf("missing preserved-citation-metadata note: %v", resp.Limitations)
	}
}

func TestApplyAskSubstanceGuardrailKeepsSubstantiveAnswer(t *testing.T) {
	t.Parallel()

	prose := "payments runs from repository payments-api with 3 deployments across prod and staging."
	resp := &askResponse{AnswerProse: prose, TruthClass: string(AnswerTruthDeterministic)}
	applyAskSubstanceGuardrail(resp, "give me an overview of the payments service", true)

	if resp.AnswerProse != prose {
		t.Errorf("substantive prose was altered: %q", resp.AnswerProse)
	}
	if resp.Partial {
		t.Error("substantive answer wrongly marked partial")
	}
}

func TestApplyAskSubstanceGuardrailIgnoresUnsupported(t *testing.T) {
	t.Parallel()

	// An unsupported primary is not evaluated for substance: emptiness and
	// boundedness rules own that case.
	resp := &askResponse{AnswerProse: "payments is payments"}
	applyAskSubstanceGuardrail(resp, "what is payments", false)
	if resp.AnswerProse == "" {
		t.Error("substance guard ran on an unsupported answer; want skipped")
	}
}
