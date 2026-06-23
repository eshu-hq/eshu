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

// TestValidateResultAllowsTruthProvenanceCoverage proves that prose whose
// factual content is backed by a classified packet's truth provenance satisfies
// citation_coverage even with no citation handles. This mirrors the narration
// validator, which allows ProvenanceTruth whenever the packet's truth_class is
// non-empty. Without this parity the publish-time guardrail blocks the same
// truth-provenance-backed prose the narration validator already accepted,
// returning empty prose for every supported answer (issue #3609).
func TestValidateResultAllowsTruthProvenanceCoverage(t *testing.T) {
	t.Parallel()

	verdict := ValidateResult(Result{
		AnswerSummary:   "checkout-service owns refund processing",
		Supported:       true,
		TruthProvenance: true,
	})

	if !verdict.Valid {
		t.Fatalf("ValidateResult() Valid = false, want true; findings = %#v", verdict.Findings)
	}
	if verdict.HasFinding(CriterionCitationCoverage) {
		t.Fatalf("ValidateResult() findings = %#v, want no citation-coverage finding when truth provenance covers the prose", verdict.Findings)
	}
}

// TestValidateResultRejectsUncitedProseWithoutTruthProvenance proves the
// guardrail is not weakened for genuinely uncited prose: a supported answer with
// no citation handles AND no truth provenance is still blocked on
// citation_coverage (issue #3609).
func TestValidateResultRejectsUncitedProseWithoutTruthProvenance(t *testing.T) {
	t.Parallel()

	verdict := ValidateResult(Result{
		AnswerSummary:   "checkout-service owns refund processing",
		Supported:       true,
		TruthProvenance: false,
	})

	if verdict.Valid {
		t.Fatal("ValidateResult() Valid = true, want false for uncited prose with no truth provenance")
	}
	if !verdict.HasFinding(CriterionCitationCoverage) {
		t.Fatalf("ValidateResult() findings = %#v, want citation-coverage finding", verdict.Findings)
	}
}
