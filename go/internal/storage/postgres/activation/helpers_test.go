// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package activation_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/projector"
	projectorruntime "github.com/eshu-hq/eshu/go/internal/projector/runtime"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/replay/parserfixture"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// activationFluxSourceYAML is one Flux GitRepository whose remote names the
// target repository; the real parser turns it into a single file fact.
const activationFluxSourceYAML = `apiVersion: source.toolkit.fluxcd.io/v1
kind: GitRepository
metadata:
  name: app-source
  namespace: flux-system
spec:
  interval: 1m
  url: https://github.com/acme/payments-deploy.git
  ref:
    branch: main
`

// openActivationObligationProofDB opens an isolated schema with the full
// production bootstrap applied. Every activation obligation proof drives real
// queue Claims, so it must never share a schema (#7479).
func openActivationObligationProofDB(t *testing.T, prefix string) (context.Context, *sql.DB) {
	t.Helper()
	if os.Getenv("ESHU_DEFERRED_PARTITION_PROOF_DISPOSABLE") != "1" {
		t.Skip("set ESHU_DEFERRED_PARTITION_PROOF_DISPOSABLE=1 for disposable PostgreSQL proof")
	}
	dsn := dsnForDeferredPartitionMemoProof(t)
	database := openIsolatedBootstrapSchema(t, dsn, prefix)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	t.Cleanup(cancel)
	return ctx, database
}

// commitFluxSourceGeneration parses the Flux YAML with the real parser and
// commits it as one source generation through the real ingestion store.
func commitFluxSourceGeneration(
	t *testing.T, ctx context.Context, store postgres.IngestionStore, scopeID, repoID, generationID string,
) {
	t.Helper()
	root := t.TempDir()
	path := filepath.Join(root, "clusters", "prod", "git-repository.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(activationFluxSourceYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	emitter, err := parserfixture.NewEmitter(parserfixture.EmitterOptions{
		ScopeID: scopeID, RepoID: repoID, TreePath: root, GenerationID: generationID,
	})
	if err != nil {
		t.Fatal(err)
	}
	collected, ok, err := emitter.Next(ctx)
	if err != nil || !ok {
		t.Fatalf("parser emitter Next: ok=%v err=%v", ok, err)
	}
	var envelopes []facts.Envelope
	for envelope := range collected.Facts {
		envelopes = append(envelopes, envelope)
	}
	if collected.FactStreamErr != nil {
		if err := collected.FactStreamErr(); err != nil {
			t.Fatalf("parser fact stream: %v", err)
		}
	}
	if len(envelopes) != 1 || envelopes[0].FactKind != "file" ||
		envelopes[0].ScopeID != scopeID || envelopes[0].GenerationID != generationID {
		t.Fatalf("parser output = %+v, want one exact source file", envelopes)
	}
	if err := store.CommitScopeGeneration(ctx, collected.Scope, collected.Generation,
		testFactChannel(envelopes)); err != nil {
		t.Fatalf("source CommitScopeGeneration: %v", err)
	}
}

func activationRepositoryFact(factID, scopeID, generationID, repoID, remote string) facts.Envelope {
	return facts.Envelope{
		FactID: factID, ScopeID: scopeID, GenerationID: generationID,
		FactKind: "repository", StableFactKey: "repository:" + repoID,
		ObservedAt: time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC),
		Payload:    map[string]any{"graph_id": repoID, "remote_url": remote},
		SourceRef:  facts.Ref{SourceSystem: "git", FactKey: factID},
	}
}

func commitActivationRepository(t *testing.T, ctx context.Context, store postgres.IngestionStore,
	envelope facts.Envelope, repoID string,
) {
	t.Helper()
	now := envelope.ObservedAt.Add(time.Minute)
	if err := store.CommitScopeGeneration(ctx,
		catalogTestScope(envelope.ScopeID, repoID),
		catalogTestGeneration(envelope.ScopeID, envelope.GenerationID, now),
		testFactChannel([]facts.Envelope{envelope})); err != nil {
		t.Fatalf("repository CommitScopeGeneration: %v", err)
	}
}

// claimActivationProjectorWork claims through the same queue (and therefore
// the same lease owner) the caller later Acks with; Ack fences on the owner.
func claimActivationProjectorWork(t *testing.T, ctx context.Context, queue postgres.ProjectorQueue,
	wantScope, wantGeneration string,
) projector.ScopeGenerationWork {
	t.Helper()
	work, ok, err := queue.Claim(ctx)
	if err != nil || !ok {
		t.Fatalf("Claim: ok=%v err=%v", ok, err)
	}
	if work.Scope.ScopeID != wantScope || work.Generation.GenerationID != wantGeneration {
		t.Fatalf("Claim = %s/%s, want %s/%s", work.Scope.ScopeID,
			work.Generation.GenerationID, wantScope, wantGeneration)
	}
	return work
}

func assertExactActivationObligation(t *testing.T, ctx context.Context, database *sql.DB,
	work projector.ScopeGenerationWork, absentMessage string,
) {
	t.Helper()
	var count int
	var workID, state string
	err := database.QueryRowContext(ctx, `SELECT count(*),
COALESCE(min(work_item_id), ''), COALESCE(min(state), '')
FROM activation_obligations
WHERE scope_id = $1 AND generation_id = $2`,
		work.Scope.ScopeID, work.Generation.GenerationID).Scan(&count, &workID, &state)
	if err != nil || count != 1 {
		t.Fatalf("%s: count=%d err=%v", absentMessage, count, err)
	}
	wantID := postgres.ProjectorWorkItemID(work.Scope.ScopeID, work.Generation.GenerationID)
	if workID != wantID || state != "pending" {
		t.Fatalf("obligation work/state = %q/%q, want %q/pending", workID, state, wantID)
	}
}

func assertNoActivationObligation(t *testing.T, ctx context.Context, database *sql.DB,
	work projector.ScopeGenerationWork,
) {
	t.Helper()
	var count int
	if err := database.QueryRowContext(ctx, `SELECT count(*)
FROM activation_obligations
WHERE scope_id = $1 AND generation_id = $2`, work.Scope.ScopeID,
		work.Generation.GenerationID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("rejected work obligation count=%d err=%v, want zero", count, err)
	}
}

func activationWorkStatus(t *testing.T, ctx context.Context, database *sql.DB,
	work projector.ScopeGenerationWork,
) string {
	t.Helper()
	var status string
	if err := database.QueryRowContext(ctx, `SELECT status FROM fact_work_items
WHERE work_item_id = $1 AND scope_id = $2 AND generation_id = $3`,
		postgres.ProjectorWorkItemID(work.Scope.ScopeID, work.Generation.GenerationID),
		work.Scope.ScopeID, work.Generation.GenerationID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	return status
}

func activationGenerationStatus(t *testing.T, ctx context.Context, database *sql.DB,
	generationID string,
) string {
	t.Helper()
	var status string
	if err := database.QueryRowContext(ctx, `SELECT status FROM scope_generations
WHERE generation_id = $1`, generationID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	return status
}

func assertActivationActivePointer(t *testing.T, ctx context.Context, database *sql.DB,
	scopeID, want string,
) {
	t.Helper()
	var active sql.NullString
	if err := database.QueryRowContext(ctx, `SELECT active_generation_id FROM ingestion_scopes
WHERE scope_id = $1`, scopeID).Scan(&active); err != nil {
		t.Fatal(err)
	}
	if active.String != want || active.Valid != (want != "") {
		t.Fatalf("active pointer for %s = %+v, want %q", scopeID, active, want)
	}
}

// setupActivationConsumer commits a Flux source and two generations of its
// target repository. The old target generation is Acked (active); the new
// target generation and the source are claimed but not yet Acked. Every
// Claim and Ack uses the one "7584-consumer-projector" lease owner.
func setupActivationConsumer(t *testing.T, prefix string) (
	context.Context, *sql.DB, postgres.IngestionStore,
	projector.ScopeGenerationWork, projector.ScopeGenerationWork,
) {
	t.Helper()
	ctx, database := openActivationObligationProofDB(t, prefix)
	store := postgres.NewIngestionStore(postgres.SQLDB{DB: database})
	store.SkipRelationshipBackfill = true
	commitFluxSourceGeneration(t, ctx, store, "git:consumer-source", "repo-consumer-source", "gen-consumer-source")
	queue := postgres.NewProjectorQueue(postgres.SQLDB{DB: database}, "7584-consumer-projector", time.Minute)
	sourceWork := claimActivationProjectorWork(t, ctx, queue, "git:consumer-source", "gen-consumer-source")

	oldTarget := activationRepositoryFact("fact-consumer-target-old", "git:consumer-target",
		"gen-consumer-target-old", "repo-consumer-target", "https://github.com/acme/payments-deploy.git")
	commitActivationRepository(t, ctx, store, oldTarget, "repo-consumer-target")
	oldWork := claimActivationProjectorWork(t, ctx, queue, oldTarget.ScopeID, oldTarget.GenerationID)
	if err := queue.Ack(ctx, oldWork, projectorruntime.Result{}); err != nil {
		t.Fatal(err)
	}
	target := activationRepositoryFact("fact-consumer-target", "git:consumer-target",
		"gen-consumer-target", "repo-consumer-target", "https://github.com/acme/payments-deploy.git")
	target.ObservedAt = target.ObservedAt.Add(time.Hour)
	commitActivationRepository(t, ctx, store, target, "repo-consumer-target")
	targetWork := claimActivationProjectorWork(t, ctx, queue, target.ScopeID, target.GenerationID)
	return ctx, database, store, sourceWork, targetWork
}

// claimTargetReducerIntent claims reducer work until the exact target
// deployment_mapping row is returned, recording every other claim.
func claimTargetReducerIntent(t *testing.T, ctx context.Context, queue postgres.ReducerQueue,
	scopeID, generationID string,
) reducer.Intent {
	t.Helper()
	claimed := make([]string, 0, 8)
	for attempt := 0; attempt < 8; attempt++ {
		intent, ok, err := queue.Claim(ctx)
		if err != nil {
			t.Fatalf("native reducer Claim: %v; prior=%v", err, claimed)
		}
		if !ok {
			break
		}
		claimed = append(claimed, intent.IntentID)
		if intent.ScopeID == scopeID && intent.GenerationID == generationID &&
			intent.Domain == reducer.DomainDeploymentMapping {
			return intent
		}
	}
	t.Fatalf("target reducer work not claimed; prior=%v", claimed)
	return reducer.Intent{}
}

func assertActivationTargetRetry(t *testing.T, ctx context.Context, database *sql.DB,
	scopeID, generationID, wantStatus, wantClass string,
) {
	t.Helper()
	var count int
	err := database.QueryRowContext(ctx, `SELECT count(*)
FROM fact_work_items
WHERE scope_id = $1 AND generation_id = $2
  AND stage = 'reducer' AND domain = 'deployment_mapping'
  AND status = $3 AND failure_class = $4`,
		scopeID, generationID, wantStatus, wantClass).Scan(&count)
	if err != nil || count != 1 {
		t.Fatalf("target retry count=%d err=%v, want one", count, err)
	}
}

func assertActivationBackwardPhase(t *testing.T, ctx context.Context,
	database *sql.DB, work projector.ScopeGenerationWork, want bool,
) {
	t.Helper()
	var exists bool
	err := database.QueryRowContext(ctx, `SELECT EXISTS (
SELECT 1 FROM graph_projection_phase_state
WHERE scope_id = $1 AND acceptance_unit_id = $1
  AND source_run_id = $2 AND generation_id = $2
  AND keyspace = 'cross_repo_evidence'
  AND phase = 'backward_evidence_committed')`,
		work.Scope.ScopeID, work.Generation.GenerationID).Scan(&exists)
	if err != nil || exists != want {
		t.Fatalf("native phase exists=%v want=%v err=%v", exists, want, err)
	}
}

// activationQueueRow is a read-only row snapshot for field-preservation oracles.
func activationQueueRow(ctx context.Context, database *sql.DB, id string) (string, error) {
	var row string
	err := database.QueryRowContext(ctx,
		"SELECT row_to_json(work)::text FROM fact_work_items AS work WHERE work_item_id=$1", id).Scan(&row)
	return row, err
}
