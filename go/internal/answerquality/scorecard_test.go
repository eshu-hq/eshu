package answerquality

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/answernarration"
	"github.com/eshu-hq/eshu/go/internal/query"
)

func TestDefaultSuiteCoversMajorAnswerFamilies(t *testing.T) {
	suite := DefaultSuite()
	want := []PromptFamily{
		PromptFamilyServiceStory,
		PromptFamilyCodeTopic,
		PromptFamilyIncidentContext,
		PromptFamilySupplyChainImpact,
		PromptFamilyDocumentationTruth,
		PromptFamilyFreshnessReadiness,
		PromptFamilyHostedGovernance,
	}
	for _, family := range want {
		if _, ok := suite.PromptByFamily(family); !ok {
			t.Fatalf("DefaultSuite missing family %q", family)
		}
	}
}

func TestScorePassesCompletePublishSafeEvidence(t *testing.T) {
	evidence := completeEvidence()

	verdict := Score(evidence)

	if !verdict.Pass {
		t.Fatalf("verdict.Pass = false, want true; failures: %v", verdict.FollowUpIssues)
	}
	if verdict.Score < 100 {
		t.Fatalf("verdict.Score = %d, want 100", verdict.Score)
	}
	if got := verdict.Criterion(CriterionPublishSafety).Status; got != CriterionPass {
		t.Fatalf("publish safety = %q, want pass", got)
	}
}

func TestScoreFailsMissingCitationsAndRequiredFollowUp(t *testing.T) {
	evidence := completeEvidence()
	// Genuinely uncited: no citation handles AND no truth provenance (empty
	// truth_class), so the citation_coverage guardrail still fails it under the
	// truth-provenance coverage class added in #3609.
	evidence.Prompts[0].Results[0].CitationHandles = nil
	evidence.Prompts[0].Results[0].TruthClass = ""
	evidence.Prompts[0].Results[0].Truncated = true
	evidence.Prompts[0].Results[0].NextCalls = nil

	verdict := Score(evidence)

	if verdict.Pass {
		t.Fatal("verdict.Pass = true, want false")
	}
	if got := verdict.Criterion(CriterionCitationCoverage).Status; got != CriterionFail {
		t.Fatalf("citation coverage = %q, want fail", got)
	}
	if got := verdict.Criterion(CriterionFollowUpUsefulness).Status; got != CriterionFail {
		t.Fatalf("follow-up usefulness = %q, want fail", got)
	}
	assertFollowUpLabel(t, verdict, "answer:citations")
	assertFollowUpLabel(t, verdict, "answer:dogfood")
}

func TestScoreFailsEmptyPromptResultsAcrossRequiredCriteria(t *testing.T) {
	evidence := completeEvidence()
	evidence.Prompts[0].Results = nil

	verdict := Score(evidence)

	if verdict.Pass {
		t.Fatal("verdict.Pass = true with no surface results, want false")
	}
	for _, name := range []CriterionName{
		CriterionUsefulness,
		CriterionTruthHonesty,
		CriterionCitationCoverage,
		CriterionBoundedness,
		CriterionParity,
		CriterionFollowUpUsefulness,
	} {
		if got := verdict.Criterion(name).Status; got != CriterionFail {
			t.Fatalf("%s = %q, want fail when prompt has no surface results", name, got)
		}
	}
}

func TestScoreFailsUnsafePublishableEvidence(t *testing.T) {
	evidence := completeEvidence()
	rawAddress := strings.Join([]string{"10", "31", "42", "9"}, ".")
	evidence.Prompts[1].Results[0].AnswerSummary = "calls private host " + rawAddress

	verdict := Score(evidence)

	if verdict.Pass {
		t.Fatal("verdict.Pass = true with raw address evidence, want false")
	}
	if got := verdict.Criterion(CriterionPublishSafety).Status; got != CriterionFail {
		t.Fatalf("publish safety = %q, want fail", got)
	}
	assertFollowUpLabel(t, verdict, "answer:dogfood")
}

func TestScoreFailsUnsafeRunMetadata(t *testing.T) {
	evidence := completeEvidence()
	evidence.RunID = strings.Join([]string{"http", "://", "private-run"}, "")

	verdict := Score(evidence)

	if verdict.Pass {
		t.Fatal("verdict.Pass = true with unsafe run metadata, want false")
	}
	if got := verdict.Criterion(CriterionPublishSafety).Status; got != CriterionFail {
		t.Fatalf("publish safety = %q, want fail", got)
	}
}

func TestScorePassesValidNarrationComparison(t *testing.T) {
	evidence := completeEvidence()
	evidence.Prompts[0].Results[0].Narration = validNarrationComparison()

	verdict := Score(evidence)

	if !verdict.Pass {
		t.Fatalf("verdict.Pass = false with valid narration comparison, want true: %#v", verdict.FollowUpIssues)
	}
	if got := verdict.Criterion(CriterionNarrationFallback).Status; got != CriterionPass {
		t.Fatalf("narration fallback = %q, want pass", got)
	}
}

func TestScoreFailsNarrationThatDropsFallbackCitation(t *testing.T) {
	evidence := completeEvidence()
	result := &evidence.Prompts[0].Results[0]
	result.Narration = validNarrationComparison()
	result.Narration.Fallback.CitationHandles = []string{"repo:demo", "file:src/service.go"}
	result.CitationHandles = []string{"repo:demo"}

	verdict := Score(evidence)

	if verdict.Pass {
		t.Fatal("verdict.Pass = true with weaker narrated citations, want false")
	}
	if got := verdict.Criterion(CriterionNarrationFallback).Status; got != CriterionFail {
		t.Fatalf("narration fallback = %q, want fail", got)
	}
	assertFollowUpLabel(t, verdict, "answer:narration")
}

func TestScoreFailsNarrationThatChangesFallbackTruthClass(t *testing.T) {
	evidence := completeEvidence()
	result := &evidence.Prompts[0].Results[0]
	result.Narration = validNarrationComparison()
	result.TruthClass = "derived"

	verdict := Score(evidence)

	if verdict.Pass {
		t.Fatal("verdict.Pass = true with changed fallback truth class, want false")
	}
	if got := verdict.Criterion(CriterionNarrationFallback).Status; got != CriterionFail {
		t.Fatalf("narration fallback = %q, want fail", got)
	}
}

func TestScoreFailsNarrationThatDropsFallbackNextCall(t *testing.T) {
	evidence := completeEvidence()
	result := &evidence.Prompts[0].Results[0]
	result.Narration = validNarrationComparison()
	result.NextCalls = nil

	verdict := Score(evidence)

	if verdict.Pass {
		t.Fatal("verdict.Pass = true with dropped fallback next call, want false")
	}
	if got := verdict.Criterion(CriterionNarrationFallback).Status; got != CriterionFail {
		t.Fatalf("narration fallback = %q, want fail", got)
	}
}

func TestScoreFailsNarrationThatHidesFallbackLimitation(t *testing.T) {
	evidence := completeEvidence()
	result := &evidence.Prompts[0].Results[0]
	result.Narration = validNarrationComparison()
	result.Narration.Fallback.Partial = true
	result.Narration.Fallback.Limitations = []string{"bounded result set is incomplete"}
	result.Partial = false
	result.Limitations = nil

	verdict := Score(evidence)

	if verdict.Pass {
		t.Fatal("verdict.Pass = true with hidden fallback limitation, want false")
	}
	if got := verdict.Criterion(CriterionNarrationFallback).Status; got != CriterionFail {
		t.Fatalf("narration fallback = %q, want fail", got)
	}
}

func TestScoreFailsAcceptedNarrationWhenValidatorFails(t *testing.T) {
	evidence := completeEvidence()
	result := &evidence.Prompts[0].Results[0]
	result.Narration = validNarrationComparison()
	result.Narration.ValidatorInput.Response.Sentences[0].Provenance = nil

	verdict := Score(evidence)

	if verdict.Pass {
		t.Fatal("verdict.Pass = true with invalid accepted narration, want false")
	}
	if got := verdict.Criterion(CriterionNarrationFallback).Status; got != CriterionFail {
		t.Fatalf("narration fallback = %q, want fail", got)
	}
}

func TestScoreFailsUnsafeNarrationValidatorMetadata(t *testing.T) {
	evidence := completeEvidence()
	rawAddress := strings.Join([]string{"10", "55", "12", "8"}, ".")
	result := &evidence.Prompts[0].Results[0]
	result.Narration = validNarrationComparison()
	result.Narration.ValidatorInput.Packet.RecommendedNextCalls = []map[string]any{
		{"target": rawAddress},
	}

	verdict := Score(evidence)

	if verdict.Pass {
		t.Fatal("verdict.Pass = true with unsafe narration validator metadata, want false")
	}
	if got := verdict.Criterion(CriterionPublishSafety).Status; got != CriterionFail {
		t.Fatalf("publish safety = %q, want fail", got)
	}
}

func TestParseEvidenceRejectsUnknownVersion(t *testing.T) {
	raw := []byte(`{"version":"answer-quality-scorecard/v2","prompts":[]}`)

	if _, err := ParseEvidence(raw); err == nil {
		t.Fatal("ParseEvidence succeeded with an unknown version, want error")
	}
}

func assertFollowUpLabel(t *testing.T, verdict Verdict, label string) {
	t.Helper()
	for _, issue := range verdict.FollowUpIssues {
		for _, got := range issue.Labels {
			if got == label {
				return
			}
		}
	}
	t.Fatalf("follow-up issues missing label %q: %#v", label, verdict.FollowUpIssues)
}

func completeEvidence() Evidence {
	suite := DefaultSuite()
	evidence := Evidence{
		Version:    EvidenceVersion,
		RunID:      "redacted-local-scorecard",
		EshuCommit: "0123456789abcdef",
		Prompts:    make([]PromptResult, 0, len(suite.Prompts)),
	}
	for _, prompt := range suite.Prompts {
		evidence.Prompts = append(evidence.Prompts, completePrompt(prompt))
	}
	return evidence
}

func completePrompt(prompt PromptSpec) PromptResult {
	result := PromptResult{
		ID:                    prompt.ID,
		Family:                prompt.Family,
		Prompt:                prompt.Prompt,
		ExpectedTruthClass:    prompt.ExpectedTruthClass,
		RequiredSurfaces:      append([]Surface(nil), prompt.RequiredSurfaces...),
		RequiredNextCalls:     append([]string(nil), prompt.RequiredNextCalls...),
		AcceptableLimitations: []string{"bounded result set is acceptable when continuation is present"},
	}
	for _, surface := range prompt.RequiredSurfaces {
		result.Results = append(result.Results, SurfaceResult{
			Surface:         surface,
			Useful:          true,
			Supported:       true,
			AnswerSummary:   "useful redacted answer for " + string(prompt.Family),
			TruthClass:      prompt.ExpectedTruthClass,
			Freshness:       "current",
			CitationHandles: []string{"repo:demo", "file:src/service.go"},
			NextCalls:       append([]string(nil), prompt.RequiredNextCalls...),
		})
	}
	return result
}

func validNarrationComparison() *NarrationComparison {
	return &NarrationComparison{
		Status:      NarrationStatusAccepted,
		FallbackRef: "service-story:api:deterministic",
		Fallback: NarrationBaseline{
			Supported:       true,
			TruthClass:      "deterministic",
			Freshness:       "current",
			CitationHandles: []string{"repo:demo"},
			NextCalls:       []string{"build_evidence_citation_packet"},
		},
		ValidatorInput: &answernarration.Input{
			Packet: query.AnswerPacket{
				TruthClass:           query.AnswerTruthDeterministic,
				Summary:              "useful redacted answer for service_story",
				Supported:            true,
				CitationRef:          "repo:demo",
				RecommendedNextCalls: []map[string]any{{"tool": "build_evidence_citation_packet"}},
			},
			Response: answernarration.Narration{
				TruthClass: query.AnswerTruthDeterministic,
				Supported:  true,
				Sentences: []answernarration.Sentence{
					{
						Text: "useful redacted answer for service_story",
						Kind: answernarration.SentenceFactual,
						Provenance: []answernarration.ProvenanceRef{
							{Kind: answernarration.ProvenanceCitation, ID: "repo:demo"},
						},
					},
				},
			},
			CitationIDs: []string{"repo:demo"},
		},
	}
}
