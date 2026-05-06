package explain

import (
	"fmt"
	"slices"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/correlation/engine"
	"github.com/eshu-hq/eshu/go/internal/correlation/model"
)

// Render returns a stable text rendering for one evaluated candidate.
func Render(result engine.Result) string {
	lines := []string{
		fmt.Sprintf(
			"candidate=%s kind=%s key=%s state=%s confidence=%.2f",
			result.Candidate.ID,
			result.Candidate.Kind,
			result.Candidate.CorrelationKey,
			result.Candidate.State,
			result.Candidate.Confidence,
		),
	}

	matchRuleNames := make([]string, 0, len(result.MatchCounts))
	for name := range result.MatchCounts {
		matchRuleNames = append(matchRuleNames, name)
	}
	slices.Sort(matchRuleNames)
	for _, name := range matchRuleNames {
		lines = append(lines, fmt.Sprintf("match_count rule=%s count=%d", name, result.MatchCounts[name]))
	}

	reasons := make([]string, 0, len(result.Candidate.RejectionReasons))
	for _, reason := range result.Candidate.RejectionReasons {
		reasons = append(reasons, string(reason))
	}
	slices.Sort(reasons)
	for _, reason := range reasons {
		lines = append(lines, fmt.Sprintf("rejection_reason=%s", reason))
	}

	evidence := slices.Clone(result.Candidate.Evidence)
	slices.SortFunc(evidence, compareEvidence)
	for _, atom := range evidence {
		lines = append(lines, renderEvidence(atom))
	}
	return strings.Join(lines, "\n")
}

func renderEvidence(atom model.EvidenceAtom) string {
	return fmt.Sprintf(
		"evidence id=%s source=%s type=%s key=%s value=%s confidence=%.2f",
		atom.ID,
		atom.SourceSystem,
		atom.EvidenceType,
		atom.Key,
		atom.Value,
		atom.Confidence,
	)
}

func compareEvidence(left model.EvidenceAtom, right model.EvidenceAtom) int {
	if left.ID != right.ID {
		if left.ID < right.ID {
			return -1
		}
		return 1
	}
	if left.SourceSystem != right.SourceSystem {
		if left.SourceSystem < right.SourceSystem {
			return -1
		}
		return 1
	}
	return strings.Compare(left.EvidenceType, right.EvidenceType)
}
