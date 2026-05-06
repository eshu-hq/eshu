package projector

import (
	"testing"
	"time"
)

func TestProjectionDecisionRowFields(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	d := ProjectionDecisionRow{
		DecisionID:       "d-1",
		DecisionType:     "project_workloads",
		RepositoryID:     "repository:r_payments",
		SourceRunID:      "run-001",
		WorkItemID:       "wi-001",
		Subject:          "repository:r_payments",
		ConfidenceScore:  0.9,
		ConfidenceReason: "test reason",
		ProvenanceSummary: map[string]any{
			"fact_count":   3,
			"output_count": 5,
		},
		CreatedAt: now,
	}

	if d.DecisionID != "d-1" {
		t.Errorf("DecisionID = %q, want d-1", d.DecisionID)
	}
	if d.ConfidenceScore != 0.9 {
		t.Errorf("ConfidenceScore = %f, want 0.9", d.ConfidenceScore)
	}
	if d.CreatedAt != now {
		t.Errorf("CreatedAt mismatch")
	}
}

func TestProjectionDecisionEvidenceRowFields(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	factID := "fact-123"
	e := ProjectionDecisionEvidenceRow{
		EvidenceID:   "ev-1",
		DecisionID:   "d-1",
		FactID:       &factID,
		EvidenceKind: "input",
		Detail:       map[string]any{"fact_type": "file_fact"},
		CreatedAt:    now,
	}

	if e.EvidenceID != "ev-1" {
		t.Errorf("EvidenceID = %q, want ev-1", e.EvidenceID)
	}
	if *e.FactID != "fact-123" {
		t.Errorf("FactID = %q, want fact-123", *e.FactID)
	}
}

func TestProjectionDecisionEvidenceNilFactID(t *testing.T) {
	t.Parallel()

	e := ProjectionDecisionEvidenceRow{
		EvidenceID:   "ev-2",
		DecisionID:   "d-1",
		FactID:       nil,
		EvidenceKind: "input",
		Detail:       map[string]any{},
		CreatedAt:    time.Now().UTC(),
	}

	if e.FactID != nil {
		t.Error("FactID should be nil")
	}
}

func TestDecisionConfidenceWorkloads(t *testing.T) {
	t.Parallel()

	score, reason := DecisionConfidence("project_workloads")
	if score != 0.9 {
		t.Errorf("score = %f, want 0.9", score)
	}
	if reason == "" {
		t.Error("reason should not be empty")
	}
}

func TestDecisionConfidencePlatforms(t *testing.T) {
	t.Parallel()

	score, _ := DecisionConfidence("project_platforms")
	if score != 0.9 {
		t.Errorf("score = %f, want 0.9", score)
	}
}

func TestDecisionConfidenceRelationships(t *testing.T) {
	t.Parallel()

	score, _ := DecisionConfidence("project_relationships")
	if score != 0.75 {
		t.Errorf("score = %f, want 0.75", score)
	}
}

func TestDecisionConfidenceDefault(t *testing.T) {
	t.Parallel()

	score, _ := DecisionConfidence("project_entities")
	if score != 0.6 {
		t.Errorf("score = %f, want 0.6", score)
	}
}

func TestBuildProjectionDecision(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	factIDs := []string{"f-1", "f-2", "f-3"}
	d := BuildProjectionDecision(DecisionInput{
		Stage:        "project_workloads",
		WorkItemID:   "wi-100",
		RepositoryID: "repository:r_payments",
		SourceRunID:  "run-42",
		FactIDs:      factIDs,
		OutputCount:  7,
		CreatedAt:    now,
	})

	if d.DecisionType != "project_workloads" {
		t.Errorf("DecisionType = %q, want project_workloads", d.DecisionType)
	}
	if d.RepositoryID != "repository:r_payments" {
		t.Errorf("RepositoryID = %q", d.RepositoryID)
	}
	if d.ConfidenceScore != 0.9 {
		t.Errorf("ConfidenceScore = %f, want 0.9", d.ConfidenceScore)
	}
	if d.DecisionID == "" {
		t.Error("DecisionID should be non-empty deterministic UUID")
	}
	if d.Subject != "repository:r_payments" {
		t.Errorf("Subject = %q", d.Subject)
	}

	summary, ok := d.ProvenanceSummary["fact_count"]
	if !ok || summary != 3 {
		t.Errorf("provenance fact_count = %v", summary)
	}
	outputCount, ok := d.ProvenanceSummary["output_count"]
	if !ok || outputCount != 7 {
		t.Errorf("provenance output_count = %v", outputCount)
	}
}

func TestBuildProjectionDecisionDeterministicID(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	input := DecisionInput{
		Stage:        "project_workloads",
		WorkItemID:   "wi-100",
		RepositoryID: "repository:r_payments",
		SourceRunID:  "run-42",
		FactIDs:      []string{"f-1"},
		OutputCount:  1,
		CreatedAt:    now,
	}

	d1 := BuildProjectionDecision(input)
	d2 := BuildProjectionDecision(input)

	if d1.DecisionID != d2.DecisionID {
		t.Errorf("non-deterministic: %q != %q", d1.DecisionID, d2.DecisionID)
	}
}

func TestBuildProjectionEvidence(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	facts := []EvidenceFactInput{
		{FactID: "f-1", FactType: "file_fact", RelativePath: "src/main.py"},
		{FactID: "f-2", FactType: "entity_fact", RelativePath: "src/models.py"},
	}

	rows := BuildProjectionEvidence("d-1", facts, now)

	if len(rows) != 2 {
		t.Fatalf("len = %d, want 2", len(rows))
	}
	if rows[0].DecisionID != "d-1" {
		t.Errorf("DecisionID = %q", rows[0].DecisionID)
	}
	if *rows[0].FactID != "f-1" {
		t.Errorf("FactID = %q", *rows[0].FactID)
	}
	if rows[0].EvidenceKind != "input" {
		t.Errorf("EvidenceKind = %q", rows[0].EvidenceKind)
	}
	if rows[0].EvidenceID == "" {
		t.Error("EvidenceID should be non-empty")
	}
}

func TestBuildProjectionEvidenceLimitsTwenty(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	facts := make([]EvidenceFactInput, 30)
	for i := range facts {
		facts[i] = EvidenceFactInput{
			FactID:       "f-" + string(rune('A'+i)),
			FactType:     "file_fact",
			RelativePath: "src/file.py",
		}
	}

	rows := BuildProjectionEvidence("d-1", facts, now)

	if len(rows) != 20 {
		t.Errorf("len = %d, want 20 (evidence limit)", len(rows))
	}
}

func TestBuildProjectionEvidenceDeterministicID(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	facts := []EvidenceFactInput{
		{FactID: "f-1", FactType: "file_fact", RelativePath: "src/main.py"},
	}

	rows1 := BuildProjectionEvidence("d-1", facts, now)
	rows2 := BuildProjectionEvidence("d-1", facts, now)

	if rows1[0].EvidenceID != rows2[0].EvidenceID {
		t.Errorf("non-deterministic: %q != %q", rows1[0].EvidenceID, rows2[0].EvidenceID)
	}
}

func TestBuildProjectionDecisionProvenanceSampleCapsAtTen(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	factIDs := make([]string, 25)
	for i := range factIDs {
		factIDs[i] = "f-" + string(rune('A'+i))
	}

	d := BuildProjectionDecision(DecisionInput{
		Stage:        "project_entities",
		WorkItemID:   "wi-200",
		RepositoryID: "repository:r_api",
		SourceRunID:  "run-99",
		FactIDs:      factIDs,
		OutputCount:  50,
		CreatedAt:    now,
	})

	sampleIDs, ok := d.ProvenanceSummary["sample_fact_ids"].([]string)
	if !ok {
		t.Fatal("sample_fact_ids not a []string")
	}
	if len(sampleIDs) != 10 {
		t.Errorf("sample_fact_ids len = %d, want 10", len(sampleIDs))
	}
}
