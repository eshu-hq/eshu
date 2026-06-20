package answerquality

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/answerguardrail"
)

func TestScorePublishSafetyUsesSharedGuardrail(t *testing.T) {
	t.Parallel()

	evidence := completeEvidence()
	rawAddress := strings.Join([]string{"10", "77", "3", "9"}, ".")
	evidence.Prompts[0].Results[0].AnswerSummary = "private host " + rawAddress

	verdict := Score(evidence)
	guardrail := answerguardrail.ValidateResult(answerguardrail.Result{
		AnswerSummary:   evidence.Prompts[0].Results[0].AnswerSummary,
		Supported:       evidence.Prompts[0].Results[0].Supported,
		CitationHandles: evidence.Prompts[0].Results[0].CitationHandles,
	})

	if got := verdict.Criterion(CriterionPublishSafety).Status; got != CriterionFail {
		t.Fatalf("Score() publish safety = %q, want fail", got)
	}
	if !guardrail.HasFinding(answerguardrail.CriterionPublishSafety) {
		t.Fatalf("shared guardrail findings = %#v, want publish-safety finding", guardrail.Findings)
	}
}
