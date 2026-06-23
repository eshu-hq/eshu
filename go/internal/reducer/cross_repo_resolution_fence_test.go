package reducer

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/relationships"
)

// orderRecordingPersister records the relative order of audit persistence and
// generation activation against a shared sequence counter so a test can assert
// that activation (publish) happens only after the graph-acceptance intents are
// durably committed.
type orderRecordingPersister struct {
	seq                  *int
	activatedGenerations []string
	activateAt           []int
	activateErr          error
}

func (p *orderRecordingPersister) UpsertCandidates(_ context.Context, _ string, _ []relationships.Candidate) error {
	return nil
}

func (p *orderRecordingPersister) UpsertResolved(_ context.Context, _ string, _ []relationships.ResolvedRelationship) error {
	return nil
}

func (p *orderRecordingPersister) ActivateResolutionGeneration(_ context.Context, generationID, _ string) error {
	if p.activateErr != nil {
		return p.activateErr
	}
	*p.seq++
	p.activateAt = append(p.activateAt, *p.seq)
	p.activatedGenerations = append(p.activatedGenerations, generationID)
	return nil
}

// orderRecordingIntentWriter records the sequence position at which the durable
// graph-acceptance intents were committed, and can be configured to fail to
// reproduce the partial-failure window.
type orderRecordingIntentWriter struct {
	seq       *int
	upsertAt  []int
	rows      [][]SharedProjectionIntentRow
	failTimes int
	failErr   error
	calls     int
}

func (w *orderRecordingIntentWriter) UpsertIntents(_ context.Context, rows []SharedProjectionIntentRow) error {
	w.calls++
	if w.calls <= w.failTimes {
		return w.failErr
	}
	*w.seq++
	w.upsertAt = append(w.upsertAt, *w.seq)
	w.rows = append(w.rows, append([]SharedProjectionIntentRow(nil), rows...))
	return nil
}

func provisionEvidence() []relationships.EvidenceFact {
	return []relationships.EvidenceFact{
		{
			EvidenceKind:     relationships.EvidenceKindTerraformAppRepo,
			RelationshipType: relationships.RelProvisionsDependencyFor,
			SourceRepoID:     "infra-repo",
			TargetRepoID:     "app-repo",
			Confidence:       0.99,
			Rationale:        "Terraform app_repo points at the target repository",
		},
	}
}

// TestCrossRepoResolutionCommitsAcceptanceBeforeActivation proves the fence:
// the durable graph-acceptance intents must be committed before the generation
// is activated (published) to the repo-dependency surface. With the buggy
// ordering (activate then write intents) this assertion fails because the
// activation sequence position precedes the intent-write position.
func TestCrossRepoResolutionCommitsAcceptanceBeforeActivation(t *testing.T) {
	t.Parallel()

	seq := 0
	intentWriter := &orderRecordingIntentWriter{seq: &seq}
	persister := &orderRecordingPersister{seq: &seq}

	handler := CrossRepoRelationshipHandler{
		EvidenceLoader: &fakeEvidenceFactLoader{facts: provisionEvidence()},
		IntentWriter:   intentWriter,
		Persister:      persister,
	}

	if _, err := handler.Resolve(context.Background(), "scope-1", "gen-1"); err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	if len(intentWriter.upsertAt) != 1 {
		t.Fatalf("intent upserts = %d, want 1", len(intentWriter.upsertAt))
	}
	if len(persister.activateAt) != 1 {
		t.Fatalf("activations = %d, want 1", len(persister.activateAt))
	}
	if intentWriter.upsertAt[0] >= persister.activateAt[0] {
		t.Fatalf(
			"graph acceptance intents committed at seq %d but activation at seq %d; "+
				"activation must be fenced after durable acceptance",
			intentWriter.upsertAt[0], persister.activateAt[0],
		)
	}
}

// TestCrossRepoResolutionDoesNotPublishWhenAcceptanceFails proves the
// partial-failure guarantee: if the graph-acceptance intent write fails, the
// generation is NOT activated/published, so no stranded denormalized edges can
// be observed on the repo-dependency surface. An operator-facing signal is
// emitted naming the fenced generation.
func TestCrossRepoResolutionDoesNotPublishWhenAcceptanceFails(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn})))
	defer slog.SetDefault(previous)

	seq := 0
	intentWriter := &orderRecordingIntentWriter{
		seq:       &seq,
		failTimes: 1,
		failErr:   fmt.Errorf("acceptance commit timeout"),
	}
	persister := &orderRecordingPersister{seq: &seq}

	handler := CrossRepoRelationshipHandler{
		EvidenceLoader: &fakeEvidenceFactLoader{facts: provisionEvidence()},
		IntentWriter:   intentWriter,
		Persister:      persister,
	}

	_, err := handler.Resolve(context.Background(), "scope-1", "gen-1")
	if err == nil {
		t.Fatal("Resolve() error = nil, want acceptance failure to propagate")
	}
	if len(persister.activatedGenerations) != 0 {
		t.Fatalf(
			"generation was published despite acceptance failure: activations = %v",
			persister.activatedGenerations,
		)
	}
	if !bytes.Contains(logs.Bytes(), []byte("cross-repo activation fenced")) {
		t.Fatalf("missing operator-facing fence signal; logs = %q", logs.String())
	}
	if !bytes.Contains(logs.Bytes(), []byte("gen-1")) {
		t.Fatalf("fence signal does not name the generation; logs = %q", logs.String())
	}
}

// TestCrossRepoResolutionConvergesOnRetryAfterAcceptanceFailure proves
// idempotent convergence: the first attempt fails at the acceptance write
// (generation not published), and a retry succeeds, committing acceptance once
// and publishing the generation exactly once with no double-publish.
func TestCrossRepoResolutionConvergesOnRetryAfterAcceptanceFailure(t *testing.T) {
	t.Parallel()

	seq := 0
	intentWriter := &orderRecordingIntentWriter{
		seq:       &seq,
		failTimes: 1,
		failErr:   fmt.Errorf("transient acceptance failure"),
	}
	persister := &orderRecordingPersister{seq: &seq}

	handler := CrossRepoRelationshipHandler{
		EvidenceLoader: &fakeEvidenceFactLoader{facts: provisionEvidence()},
		IntentWriter:   intentWriter,
		Persister:      persister,
	}

	if _, err := handler.Resolve(context.Background(), "scope-1", "gen-1"); err == nil {
		t.Fatal("first Resolve() error = nil, want transient acceptance failure")
	}
	if len(persister.activatedGenerations) != 0 {
		t.Fatalf("generation published on failed attempt: %v", persister.activatedGenerations)
	}

	if _, err := handler.Resolve(context.Background(), "scope-1", "gen-1"); err != nil {
		t.Fatalf("retry Resolve() error = %v", err)
	}

	if len(intentWriter.upsertAt) != 1 {
		t.Fatalf("acceptance committed %d times, want exactly 1", len(intentWriter.upsertAt))
	}
	if len(persister.activatedGenerations) != 1 {
		t.Fatalf("generation published %d times, want exactly 1", len(persister.activatedGenerations))
	}
	if intentWriter.upsertAt[0] >= persister.activateAt[0] {
		t.Fatalf(
			"on retry, acceptance at seq %d must precede activation at seq %d",
			intentWriter.upsertAt[0], persister.activateAt[0],
		)
	}
}

// TestCrossRepoResolutionEmptyEvidenceFencesActivation proves the same fence on
// the empty-evidence path: the retract intents must be durably committed before
// the (empty) generation is activated, so a tombstone publish cannot strand
// non-retracted edges.
func TestCrossRepoResolutionEmptyEvidenceFencesActivation(t *testing.T) {
	t.Parallel()

	seq := 0
	intentWriter := &orderRecordingIntentWriter{seq: &seq}
	persister := &orderRecordingPersister{seq: &seq}

	handler := CrossRepoRelationshipHandler{
		EvidenceLoader: &fakeEvidenceFactLoader{facts: nil},
		IntentWriter:   intentWriter,
		Persister:      persister,
	}

	if _, err := handler.Resolve(context.Background(), "scope-1", "gen-1"); err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	// Empty evidence still activates the generation (tombstone). When retract
	// intents are produced they must commit before activation.
	if len(persister.activateAt) != 1 {
		t.Fatalf("activations = %d, want 1", len(persister.activateAt))
	}
	if len(intentWriter.upsertAt) > 0 && intentWriter.upsertAt[0] >= persister.activateAt[0] {
		t.Fatalf(
			"empty-evidence retract intents committed at seq %d but activation at seq %d; "+
				"activation must be fenced after durable retract acceptance",
			intentWriter.upsertAt[0], persister.activateAt[0],
		)
	}
}
