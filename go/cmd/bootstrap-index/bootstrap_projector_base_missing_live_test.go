// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8779 review F3 disposable PostgreSQL proof that the bootstrap-index projector retires a delta whose union base is missing through the delta-baseline fence like the projector service does: the delta generation is superseded with the projector_delta_base_missing class and the scope gets a reindex watermark, instead of a generic Fail retry; isolated bootstrapped schema per test; wall time is logged, not gated
//eshu:readiness:env ESHU_AWS_DRIFT_ARN_PROOF_DISPOSABLE ESHU_AWS_DRIFT_ARN_PROOF_DSN ESHU_DEFERRED_PARTITION_PROOF_DISPOSABLE ESHU_DEFERRED_PARTITION_PROOF_DSN ESHU_PROJECTOR_CLAIM_DEADLOCK_PROOF_DSN ESHU_PROJECTOR_SUPERSESSION_PROOF_DSN ESHU_REDUCER_FAIRNESS_PROOF_DSN

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	gitcontent "github.com/eshu-hq/eshu/go/internal/collector/git/content"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/recovery"
	"github.com/eshu-hq/eshu/go/internal/repositoryidentity"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

// openBaseMissingProofDB bootstraps the production schema into a disposable,
// per-test schema on ESHU_PROJECTOR_SUPERSESSION_PROOF_DSN.
func openBaseMissingProofDB(t *testing.T) *sql.DB {
	t.Helper()
	baseDSN := strings.TrimSpace(os.Getenv("ESHU_PROJECTOR_SUPERSESSION_PROOF_DSN"))
	if baseDSN == "" {
		t.Skip("set ESHU_PROJECTOR_SUPERSESSION_PROOF_DSN to a disposable Postgres database")
	}
	ctx := context.Background()
	admin, err := sql.Open("pgx", baseDSN)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	postgresproof.InstallTrigramExtension(ctx, t, admin)
	schemaName := fmt.Sprintf("proof_8779_bootstrap_%d", time.Now().UnixNano())
	if _, err := admin.ExecContext(ctx, "CREATE SCHEMA "+schemaName); err != nil {
		t.Fatalf("create proof schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = admin.ExecContext(context.Background(), "DROP SCHEMA "+schemaName+" CASCADE")
		_ = admin.Close()
	})
	parsed, err := url.Parse(baseDSN)
	if err != nil {
		t.Fatalf("parse dsn: %v", err)
	}
	query := parsed.Query()
	query.Set("search_path", schemaName+",public")
	parsed.RawQuery = query.Encode()
	database, err := sql.Open("pgx", parsed.String())
	if err != nil {
		t.Fatalf("open proof schema: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := postgres.ApplyBootstrap(ctx, postgres.SQLDB{DB: database}); err != nil {
		t.Fatalf("apply bootstrap: %v", err)
	}
	return database
}

// TestDrainProjectorBaseMissingSupersedesAndRequestsReindexLive commits a full
// generation A, projects it through drainProjector, drains A's facts (the
// retention-prune shape), commits delta B diffed from A, and drains again.
// B must end superseded with the base-missing class and a reindex watermark,
// exactly as the projector service retires it.
func TestDrainProjectorBaseMissingSupersedesAndRequestsReindexLive(t *testing.T) {
	database := openBaseMissingProofDB(t)
	ctx := context.Background()
	db := postgres.SQLDB{DB: database}
	repo, err := repositoryidentity.MetadataFor("bootstrap-base-missing-8779", "/repos/bootstrap-base-missing-8779",
		"https://github.com/eshu-proof/bootstrap-base-missing-8779.git")
	if err != nil {
		t.Fatalf("repository identity: %v", err)
	}
	scopeValue := scope.IngestionScope{
		ScopeID: recovery.GitRepositoryScopePrefix + repo.ID, SourceSystem: "git", ScopeKind: scope.KindRepository,
		CollectorKind: scope.CollectorGit, PartitionKey: repo.ID,
		Metadata: map[string]string{"repo_id": repo.ID, "local_path": repo.LocalPath},
	}
	store := postgres.NewIngestionStore(db)
	observed := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	commit := func(generationID, commitSHA, baseline string, delta bool) {
		t.Helper()
		observed = observed.Add(time.Minute)
		generation := scope.ScopeGeneration{
			GenerationID: generationID, ScopeID: scopeValue.ScopeID, ObservedAt: observed, IngestedAt: observed,
			Status: scope.GenerationStatusPending, TriggerKind: scope.TriggerKindSnapshot,
			FreshnessHint: "hint-" + generationID, SourceCommitSHA: commitSHA, IsDelta: delta,
			DeltaBaselineCommitSHA: baseline,
		}
		var changed []string
		if delta {
			changed = []string{"keep.go"}
		}
		envelopes := []facts.Envelope{
			gitcontent.RepositoryFactEnvelope(repo.LocalPath, repo, "run-"+generationID, scopeValue.ScopeID, generationID,
				observed, 1, nil, false, "main", nil, delta, changed, nil, false, false), // delta, reconcile, emptyScope
			gitcontent.FileFactEnvelope(repo.LocalPath, repo.ID, scopeValue.ScopeID, generationID, observed,
				map[string]any{"path": repo.LocalPath + "/keep.go", "language": "go"}, false),
		}
		stream := make(chan facts.Envelope, len(envelopes))
		for _, envelope := range envelopes {
			stream <- envelope
		}
		close(stream)
		if err := store.CommitScopeGeneration(ctx, scopeValue, generation, stream); err != nil {
			t.Fatalf("commit %s: %v", generationID, err)
		}
	}
	drain := func() {
		t.Helper()
		queue := postgres.NewProjectorQueue(db, "proof-8779-bootstrap", time.Minute)
		start := time.Now()
		err := drainProjector(ctx, queue, postgres.NewFactStore(db), &fakeProjectionRunner{}, queue,
			queue, queue, queue, 0, 1, nil, nil, nil)
		t.Logf("bootstrap projector drained in %s (error: %v)", time.Since(start), err)
	}

	const commitA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaab779"
	commit("gen-8779-bootstrap-a", commitA, "", false)
	drain()
	if _, err := database.ExecContext(ctx, `DELETE FROM fact_records WHERE scope_id = $1 AND generation_id = $2`,
		scopeValue.ScopeID, "gen-8779-bootstrap-a"); err != nil {
		t.Fatalf("drain base facts: %v", err)
	}
	commit("gen-8779-bootstrap-b", "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb779", commitA, true)
	drain()

	var workStatus, failureClass, generationStatus string
	if err := database.QueryRowContext(ctx, `
SELECT w.status, COALESCE(w.failure_class, ''), g.status
FROM fact_work_items w JOIN scope_generations g ON g.generation_id = w.generation_id
WHERE w.stage = 'projector' AND w.generation_id = 'gen-8779-bootstrap-b'`).Scan(
		&workStatus, &failureClass, &generationStatus); err != nil {
		t.Fatalf("read delta B work row: %v", err)
	}
	if failureClass != projector.DeltaBaselineBaseMissingClass || generationStatus != "superseded" || workStatus != "superseded" {
		t.Fatalf("delta B work status = %q, failure class = %q, generation status = %q; want superseded, %q, superseded (fence refusal, not Fail)",
			workStatus, failureClass, generationStatus, projector.DeltaBaselineBaseMissingClass)
	}
	var watermarks int
	if err := database.QueryRowContext(ctx, `SELECT count(*) FROM repository_reindex_requests WHERE scope_id = $1`,
		scopeValue.ScopeID).Scan(&watermarks); err != nil {
		t.Fatalf("read reindex watermark: %v", err)
	}
	if watermarks != 1 {
		t.Fatalf("reindex watermarks for the refused scope = %d, want 1 (the refusal must route to the forced-full path)", watermarks)
	}
}
