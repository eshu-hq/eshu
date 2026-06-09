package answerquality

import (
	"strings"
	"testing"
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
	evidence.Prompts[0].Results[0].CitationHandles = nil
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
