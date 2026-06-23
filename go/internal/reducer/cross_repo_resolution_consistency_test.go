package reducer

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/relationships"
)

// These tests are the dual-write Postgres↔graph consistency proof for issue
// #3559. The cross-repo resolver performs a dual write that is NOT atomic across
// backends: it persists resolved relationships and activates the relationship
// generation in Postgres (the "generation swap" / publish), and it durably
// enqueues the shared-projection intents that later drive the NornicDB graph
// edges. The denormalized graph edges carry resolved_id/generation_id pointers
// back into the active Postgres generation.
//
// The consistency invariant under a generation swap:
//
//	A relationship generation MUST NOT become active in Postgres unless every
//	durable write it implies — the resolved rows AND the graph-edge intents that
//	project them — has already committed. Activation is the single publish point;
//	it must be the LAST durable step so a partial failure converges to the prior
//	active generation instead of stranding active resolved rows whose graph edges
//	(or retractions) were never queued.
//
// These doubles record the exact order of dual-write operations and allow a
// failure to be injected at a chosen step so the proof can exercise the
// partial-failure window directly.

type consistencyOp struct {
	name         string
	generationID string
}

// orderingPersister records the sequence of persistence operations and which
// generations were activated. It is intentionally a separate, order-aware double
// from fakeResolutionPersister so the proof can assert publish-last ordering.
type orderingPersister struct {
	mu       sync.Mutex
	ops      *[]consistencyOp
	failOn   string // operation name that should return an error, "" to never fail
	failErr  error
	activate []string
}

func (p *orderingPersister) record(name, generationID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	*p.ops = append(*p.ops, consistencyOp{name: name, generationID: generationID})
	if p.failOn == name {
		return p.failErr
	}
	if name == "ActivateResolutionGeneration" {
		p.activate = append(p.activate, generationID)
	}
	return nil
}

func (p *orderingPersister) UpsertCandidates(_ context.Context, generationID string, _ []relationships.Candidate) error {
	return p.record("UpsertCandidates", generationID)
}

func (p *orderingPersister) UpsertResolved(_ context.Context, generationID string, _ []relationships.ResolvedRelationship) error {
	return p.record("UpsertResolved", generationID)
}

func (p *orderingPersister) ActivateResolutionGeneration(_ context.Context, generationID, _ string) error {
	return p.record("ActivateResolutionGeneration", generationID)
}

func (p *orderingPersister) activatedGenerations() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.activate...)
}

// orderingIntentWriter records each intent write into the shared op log and can
// inject a failure to simulate the durable graph-intent write failing mid-swap.
type orderingIntentWriter struct {
	mu      sync.Mutex
	ops     *[]consistencyOp
	failGen map[string]error // generationID -> error to return, simulating a write crash
	written map[string]int   // generationID -> rows durably written
}

func (w *orderingIntentWriter) UpsertIntents(_ context.Context, rows []SharedProjectionIntentRow) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	generationID := ""
	if len(rows) > 0 {
		generationID = rows[0].GenerationID
	}
	*w.ops = append(*w.ops, consistencyOp{name: "UpsertIntents", generationID: generationID})
	if err, ok := w.failGen[generationID]; ok {
		return err
	}
	if w.written == nil {
		w.written = map[string]int{}
	}
	w.written[generationID] += len(rows)
	return nil
}

func writeEvidence() []relationships.EvidenceFact {
	return []relationships.EvidenceFact{
		{
			EvidenceKind:     relationships.EvidenceKindTerraformAppRepo,
			RelationshipType: relationships.RelProvisionsDependencyFor,
			SourceRepoID:     "infra-repo",
			TargetRepoID:     "app-repo",
			Confidence:       0.99,
			Rationale:        "Terraform app_repo points at target",
		},
	}
}

// opIndex returns the index of the first op with the given name, or -1.
func opIndex(ops []consistencyOp, name string) int {
	for i, op := range ops {
		if op.name == name {
			return i
		}
	}
	return -1
}

// TestDualWriteActivatesGenerationLastOnWritePath proves the publish-last
// invariant for the non-empty-evidence path: the graph-edge intents are durably
// written BEFORE the relationship generation is activated. If activation ran
// first, a failed intent write would strand an active generation with no graph
// edges (Postgres↔graph divergence).
func TestDualWriteActivatesGenerationLastOnWritePath(t *testing.T) {
	t.Parallel()

	var ops []consistencyOp
	persister := &orderingPersister{ops: &ops}
	intentWriter := &orderingIntentWriter{ops: &ops}

	handler := CrossRepoRelationshipHandler{
		EvidenceLoader: &fakeEvidenceFactLoader{facts: writeEvidence()},
		Persister:      persister,
		IntentWriter:   intentWriter,
	}

	if _, err := handler.Resolve(context.Background(), "scope-1", "gen-1"); err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	intentIdx := opIndex(ops, "UpsertIntents")
	activateIdx := opIndex(ops, "ActivateResolutionGeneration")
	if intentIdx < 0 {
		t.Fatalf("expected an UpsertIntents op, got %#v", ops)
	}
	if activateIdx < 0 {
		t.Fatalf("expected an ActivateResolutionGeneration op, got %#v", ops)
	}
	if activateIdx < intentIdx {
		t.Fatalf("generation activated (idx %d) before graph intents durably written (idx %d): "+
			"a failed intent write would strand an active generation with no graph edges. ops=%#v",
			activateIdx, intentIdx, ops)
	}
}

// TestDualWriteIntentFailureDoesNotActivateGenerationOnWritePath proves that
// when the durable graph-intent write fails, the generation is NOT activated, so
// Postgres converges to the prior active generation and never strands active
// resolved rows without their graph edges.
func TestDualWriteIntentFailureDoesNotActivateGenerationOnWritePath(t *testing.T) {
	t.Parallel()

	var ops []consistencyOp
	persister := &orderingPersister{ops: &ops}
	intentWriter := &orderingIntentWriter{
		ops:     &ops,
		failGen: map[string]error{"gen-1": fmt.Errorf("nornicdb intent write timeout")},
	}

	handler := CrossRepoRelationshipHandler{
		EvidenceLoader: &fakeEvidenceFactLoader{facts: writeEvidence()},
		Persister:      persister,
		IntentWriter:   intentWriter,
	}

	_, err := handler.Resolve(context.Background(), "scope-1", "gen-1")
	if err == nil {
		t.Fatal("Resolve() error = nil, want intent-write failure to propagate")
	}
	if got := persister.activatedGenerations(); len(got) != 0 {
		t.Fatalf("generation activated despite failed graph-intent write: %v "+
			"(this is the divergence the publish-last ordering must prevent)", got)
	}
}

// TestDualWriteActivatesGenerationLastOnEmptyEvidencePath proves the same
// publish-last invariant for the empty-evidence retract path. When evidence
// disappears, the resolver must durably enqueue the retract intents (which
// remove the now-stale graph edges) BEFORE activating the new empty generation.
// Activating first would publish an empty generation while the stale graph edges
// are still present and their retraction was never queued.
func TestDualWriteActivatesGenerationLastOnEmptyEvidencePath(t *testing.T) {
	t.Parallel()

	var ops []consistencyOp
	persister := &orderingPersister{ops: &ops}
	intentWriter := &orderingIntentWriter{ops: &ops}

	// A repository-scoped scope id yields retract rows even with no evidence,
	// so the retract-intent write and the activation both occur.
	handler := CrossRepoRelationshipHandler{
		EvidenceLoader: &fakeEvidenceFactLoader{facts: nil},
		Persister:      persister,
		IntentWriter:   intentWriter,
	}

	if _, err := handler.Resolve(context.Background(), "git-repository-scope:repository:r_infra", "gen-empty"); err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	intentIdx := opIndex(ops, "UpsertIntents")
	activateIdx := opIndex(ops, "ActivateResolutionGeneration")
	if intentIdx < 0 {
		t.Fatalf("expected an UpsertIntents (retract) op on the empty path, got %#v", ops)
	}
	if activateIdx < 0 {
		t.Fatalf("expected an ActivateResolutionGeneration op on the empty path, got %#v", ops)
	}
	if activateIdx < intentIdx {
		t.Fatalf("empty generation activated (idx %d) before retract intents durably written (idx %d): "+
			"stale graph edges would be stranded. ops=%#v", activateIdx, intentIdx, ops)
	}
}

// TestDualWriteRetractFailureDoesNotActivateEmptyGeneration proves the
// empty-evidence partial-failure window converges: when the retract-intent write
// fails, the empty generation is NOT activated, so the prior generation (and its
// graph edges) remain the active truth until a retry succeeds.
func TestDualWriteRetractFailureDoesNotActivateEmptyGeneration(t *testing.T) {
	t.Parallel()

	var ops []consistencyOp
	persister := &orderingPersister{ops: &ops}
	intentWriter := &orderingIntentWriter{
		ops:     &ops,
		failGen: map[string]error{"gen-empty": fmt.Errorf("nornicdb retract timeout")},
	}

	handler := CrossRepoRelationshipHandler{
		EvidenceLoader: &fakeEvidenceFactLoader{facts: nil},
		Persister:      persister,
		IntentWriter:   intentWriter,
	}

	_, err := handler.Resolve(context.Background(), "git-repository-scope:repository:r_infra", "gen-empty")
	if err == nil {
		t.Fatal("Resolve() error = nil, want retract-intent failure to propagate")
	}
	if got := persister.activatedGenerations(); len(got) != 0 {
		t.Fatalf("empty generation activated despite failed retract write: %v", got)
	}
}

// TestDualWriteConvergesOnRetryAfterIntentFailure proves convergence: a Resolve
// that failed mid-swap (intent write failed, generation not activated) reaches a
// consistent state when retried — the same deterministic generation re-runs, the
// intents become durable, and the generation activates exactly once.
func TestDualWriteConvergesOnRetryAfterIntentFailure(t *testing.T) {
	t.Parallel()

	var ops []consistencyOp
	persister := &orderingPersister{ops: &ops}
	intentWriter := &orderingIntentWriter{
		ops:     &ops,
		failGen: map[string]error{"gen-1": fmt.Errorf("transient nornicdb timeout")},
	}

	handler := CrossRepoRelationshipHandler{
		EvidenceLoader: &fakeEvidenceFactLoader{facts: writeEvidence()},
		Persister:      persister,
		IntentWriter:   intentWriter,
	}

	// First attempt: intent write fails, generation stays non-active.
	if _, err := handler.Resolve(context.Background(), "scope-1", "gen-1"); err == nil {
		t.Fatal("first Resolve() error = nil, want injected failure")
	}
	if got := persister.activatedGenerations(); len(got) != 0 {
		t.Fatalf("generation activated on failed attempt: %v", got)
	}

	// Recovery: the transient backend failure clears.
	intentWriter.mu.Lock()
	delete(intentWriter.failGen, "gen-1")
	intentWriter.mu.Unlock()

	// Retry the same generation: must converge to active with durable intents.
	if _, err := handler.Resolve(context.Background(), "scope-1", "gen-1"); err != nil {
		t.Fatalf("retry Resolve() error = %v", err)
	}
	got := persister.activatedGenerations()
	if len(got) != 1 || got[0] != "gen-1" {
		t.Fatalf("after convergence, activatedGenerations = %v, want exactly [gen-1]", got)
	}
	intentWriter.mu.Lock()
	written := intentWriter.written["gen-1"]
	intentWriter.mu.Unlock()
	if written == 0 {
		t.Fatal("expected graph intents durably written after convergence, got 0")
	}
}

// TestDualWriteConcurrentGenerationsPreserveOrderingInvariant proves the
// publish-last invariant holds under concurrency: many resolvers for distinct
// generations run at once (race detector enabled in CI), and every generation
// activates only after its own graph intents were durably written. No
// interleaving strands a generation with an out-of-order publish.
func TestDualWriteConcurrentGenerationsPreserveOrderingInvariant(t *testing.T) {
	t.Parallel()

	const generations = 24

	// Per-generation op logs avoid cross-talk; each handler records into its own
	// ordered slice so the invariant is asserted independently per generation.
	type perGen struct {
		ops          []consistencyOp
		persister    *orderingPersister
		intentWriter *orderingIntentWriter
	}

	gens := make([]*perGen, generations)
	for i := range gens {
		g := &perGen{}
		g.persister = &orderingPersister{ops: &g.ops}
		g.intentWriter = &orderingIntentWriter{ops: &g.ops}
		gens[i] = g
	}

	var wg sync.WaitGroup
	errs := make([]error, generations)
	for i := range gens {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			g := gens[i]
			handler := CrossRepoRelationshipHandler{
				EvidenceLoader: &fakeEvidenceFactLoader{facts: writeEvidence()},
				Persister:      g.persister,
				IntentWriter:   g.intentWriter,
			}
			_, errs[i] = handler.Resolve(
				context.Background(),
				fmt.Sprintf("scope-%d", i),
				fmt.Sprintf("gen-%d", i),
			)
		}(i)
	}
	wg.Wait()

	for i, g := range gens {
		if errs[i] != nil {
			t.Fatalf("gen %d Resolve() error = %v", i, errs[i])
		}
		intentIdx := opIndex(g.ops, "UpsertIntents")
		activateIdx := opIndex(g.ops, "ActivateResolutionGeneration")
		if intentIdx < 0 || activateIdx < 0 {
			t.Fatalf("gen %d missing dual-write ops: %#v", i, g.ops)
		}
		if activateIdx < intentIdx {
			t.Fatalf("gen %d activated (idx %d) before intents durable (idx %d) under concurrency: %#v",
				i, activateIdx, intentIdx, g.ops)
		}
		if got := g.persister.activatedGenerations(); len(got) != 1 {
			t.Fatalf("gen %d activated %d times, want exactly 1", i, len(got))
		}
	}
}
