package answerguardrail

import (
	"strings"
	"testing"
)

func TestValidateResultRejectsUnsafePublishableStrings(t *testing.T) {
	t.Parallel()

	rawAddress := strings.Join([]string{"10", "44", "5", "9"}, ".")
	verdict := ValidateResult(Result{
		AnswerSummary:   "calls private host " + rawAddress,
		Supported:       true,
		CitationHandles: []string{"entity:service"},
	})

	if verdict.Valid {
		t.Fatal("ValidateResult() Valid = true, want false")
	}
	if !verdict.HasFinding(CriterionPublishSafety) {
		t.Fatalf("ValidateResult() findings = %#v, want publish-safety finding", verdict.Findings)
	}
}

func TestValidateResultRejectsSupportedAnswerWithoutCitations(t *testing.T) {
	t.Parallel()

	verdict := ValidateResult(Result{
		AnswerSummary: "checkout-service owns refund processing",
		Supported:     true,
	})

	if verdict.Valid {
		t.Fatal("ValidateResult() Valid = true, want false")
	}
	if !verdict.HasFinding(CriterionCitationCoverage) {
		t.Fatalf("ValidateResult() findings = %#v, want citation-coverage finding", verdict.Findings)
	}
}

func TestValidateResultAllowsUnsupportedFallbackWithoutCitations(t *testing.T) {
	t.Parallel()

	verdict := ValidateResult(Result{
		AnswerSummary: "no supported evidence assembled",
		Supported:     false,
		Limitations:   []string{"capability unsupported"},
	})

	if !verdict.Valid {
		t.Fatalf("ValidateResult() Valid = false, want true; findings = %#v", verdict.Findings)
	}
}
