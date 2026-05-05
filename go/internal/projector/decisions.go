package projector

import (
	"crypto/sha256"
	"fmt"
	"time"
)

// ProjectionDecisionRow is one persisted projection decision.
type ProjectionDecisionRow struct {
	DecisionID        string
	DecisionType      string
	RepositoryID      string
	SourceRunID       string
	WorkItemID        string
	Subject           string
	ConfidenceScore   float64
	ConfidenceReason  string
	ProvenanceSummary map[string]any
	CreatedAt         time.Time
}

// ProjectionDecisionEvidenceRow is one evidence record attached to a persisted
// projection decision.
type ProjectionDecisionEvidenceRow struct {
	EvidenceID   string
	DecisionID   string
	FactID       *string
	EvidenceKind string
	Detail       map[string]any
	CreatedAt    time.Time
}

const decisionEvidenceLimit = 20

// DecisionConfidence returns a bounded confidence score and rationale for one
// projection stage.
func DecisionConfidence(stage string) (float64, string) {
	switch stage {
	case "project_workloads", "project_platforms":
		return 0.9, "Projected from persisted repository facts and materialized repository paths"
	case "project_relationships":
		return 0.75, "Projected from parsed file facts and repository import metadata"
	default:
		return 0.6, "Projected from persisted fact inputs without a specialized confidence rule"
	}
}

// DecisionInput captures the parameters needed to build one projection
// decision.
type DecisionInput struct {
	Stage        string
	WorkItemID   string
	RepositoryID string
	SourceRunID  string
	FactIDs      []string
	OutputCount  int
	CreatedAt    time.Time
}

// BuildProjectionDecision constructs a deterministic projection decision row
// from the given input.
func BuildProjectionDecision(input DecisionInput) ProjectionDecisionRow {
	score, reason := DecisionConfidence(input.Stage)

	sampleIDs := input.FactIDs
	if len(sampleIDs) > 10 {
		sampleIDs = sampleIDs[:10]
	}

	return ProjectionDecisionRow{
		DecisionID:       deterministicID(input.WorkItemID, input.Stage, input.SourceRunID),
		DecisionType:     input.Stage,
		RepositoryID:     input.RepositoryID,
		SourceRunID:      input.SourceRunID,
		WorkItemID:       input.WorkItemID,
		Subject:          input.RepositoryID,
		ConfidenceScore:  score,
		ConfidenceReason: reason,
		ProvenanceSummary: map[string]any{
			"fact_count":      len(input.FactIDs),
			"output_count":    input.OutputCount,
			"sample_fact_ids": sampleIDs,
		},
		CreatedAt: input.CreatedAt,
	}
}

// EvidenceFactInput captures one fact record's metadata needed for evidence
// row construction.
type EvidenceFactInput struct {
	FactID       string
	FactType     string
	RelativePath string
}

// BuildProjectionEvidence constructs bounded evidence rows for one projection
// decision. At most 20 evidence rows are produced.
func BuildProjectionEvidence(decisionID string, facts []EvidenceFactInput, createdAt time.Time) []ProjectionDecisionEvidenceRow {
	limit := min(len(facts), decisionEvidenceLimit)

	rows := make([]ProjectionDecisionEvidenceRow, 0, limit)
	for _, fact := range facts[:limit] {
		factID := fact.FactID
		rows = append(rows, ProjectionDecisionEvidenceRow{
			EvidenceID:   deterministicID(decisionID, fact.FactID),
			DecisionID:   decisionID,
			FactID:       &factID,
			EvidenceKind: "input",
			Detail: map[string]any{
				"fact_type":     fact.FactType,
				"relative_path": fact.RelativePath,
			},
			CreatedAt: createdAt,
		})
	}

	return rows
}

// deterministicID produces a stable hex-encoded identifier from the given
// components, matching the Python uuid5(NAMESPACE_URL, ...) approach with a
// SHA-256 digest truncated to 32 hex characters for readability.
func deterministicID(parts ...string) string {
	h := sha256.New()
	for i, p := range parts {
		if i > 0 {
			h.Write([]byte(":"))
		}
		h.Write([]byte(p))
	}
	return fmt.Sprintf("%x", h.Sum(nil)[:16])
}
