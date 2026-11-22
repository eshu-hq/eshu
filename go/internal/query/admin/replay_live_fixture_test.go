// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admin_test

// Shared live fixture for the replay retry-allowance (#11182) and replay
// activation (#11183) proofs. Each proof bootstraps the full production schema
// into an isolated schema and drives the production ingestion commit, the
// production ProjectorQueue, and the three replay writers: the mounted
// POST /api/v0/admin/replay handler over the real admin store, and the
// recovery replay with and without a row limit. It lives in the external
// admin_test package because an internal admin test cannot import admin/store
// (import cycle).

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/projector"
	projectorruntime "github.com/eshu-hq/eshu/go/internal/projector/runtime"
	"github.com/eshu-hq/eshu/go/internal/query/admin"
	"github.com/eshu-hq/eshu/go/internal/query/admin/store"
	"github.com/eshu-hq/eshu/go/internal/query/testutil"
	"github.com/eshu-hq/eshu/go/internal/recovery"
	"github.com/eshu-hq/eshu/go/internal/scope"
	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"

	_ "github.com/jackc/pgx/v5/stdlib"
)

const replayProofScopeID = "git-repository-scope:proof/11182"

// replaySite names one of the three replay writers that carry the
// queuestore.ReplayRetryAllowanceAssignments SET fragment.
type replaySite string

const (
	replayViaAdmin           replaySite = "admin"
	replayViaRecovery        replaySite = "recovery"
	replayViaRecoveryBounded replaySite = "recovery-bounded"
)

// replaySites is every replay writer, so each replay proof runs on all three.
var replaySites = []replaySite{replayViaAdmin, replayViaRecovery, replayViaRecoveryBounded}

// proofProjectionError is a retryable projection failure that is not an
// admission timeout, such as a transient Postgres or graph write error.
type proofProjectionError struct{}

func (proofProjectionError) Error() string   { return "proof: transient projection failure" }
func (proofProjectionError) Retryable() bool { return true }

// proofAdmissionTimeoutError mirrors the ingester reducerAdmissionTimeoutError
// contract (retryable, admission_timeout, failure.AdmissionTimeoutError),
// which this package cannot import from the ingester main package.
type proofAdmissionTimeoutError struct{}

func (proofAdmissionTimeoutError) Error() string            { return "proof: reducer admission wait timed out" }
func (proofAdmissionTimeoutError) Retryable() bool          { return true }
func (proofAdmissionTimeoutError) IsAdmissionTimeout() bool { return true }
func (proofAdmissionTimeoutError) FailureClass() string     { return "admission_timeout" }

// replayProof is one isolated, fully bootstrapped proof schema.
type replayProof struct {
	t        *testing.T
	ctx      context.Context
	db       *sql.DB
	ingest   pgstatus.IngestionStore
	queue    pgstatus.ProjectorQueue
	recovery pgstatus.RecoveryStore
	admin    admin.Store
	mux      *http.ServeMux
	scope    scope.IngestionScope
	clock    time.Time
	// keys numbers admin idempotency keys; replays may run concurrently.
	keys atomic.Int64
}

func openReplayProof(t *testing.T) *replayProof {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_DSN to a disposable Postgres to run the #11182/#11183 replay proofs")
	}
	database := postgresproof.OpenIsolatedSchema(t, dsn, "replay_allowance", func(ctx context.Context, database *sql.DB) error {
		return pgstatus.ApplyBootstrap(ctx, pgstatus.SQLDB{DB: database})
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	t.Cleanup(cancel)
	adminStore := store.NewStore(database)
	return &replayProof{
		t: t, ctx: ctx, db: database,
		ingest: pgstatus.NewIngestionStore(pgstatus.SQLDB{DB: database}),
		// Production projector workers share one lease owner, so the proof
		// does too: the attempt count is the only claim fence.
		queue:    pgstatus.NewProjectorQueue(pgstatus.SQLDB{DB: database}, "ingester", time.Minute),
		recovery: pgstatus.NewRecoveryStore(pgstatus.SQLDB{DB: database}),
		admin:    adminStore,
		mux: testutil.MountAdminHandler(&admin.Handler{
			Store: adminStore, Audit: &testutil.FakeGovernanceAuditAppender{},
		}),
		scope: scope.IngestionScope{
			ScopeID: replayProofScopeID, SourceSystem: "git", ScopeKind: scope.KindRepository,
			CollectorKind: scope.CollectorGit, PartitionKey: "proof/11182",
		},
		clock: time.Now().UTC().Add(-24 * time.Hour).Truncate(time.Second),
	}
}

// commit stores one full snapshot generation with a few file facts through
// the production IngestionStore, which also enqueues its projector row.
func (p *replayProof) commit(generationID string) {
	p.t.Helper()
	p.clock = p.clock.Add(time.Minute)
	generation := scope.ScopeGeneration{
		GenerationID: generationID, ScopeID: p.scope.ScopeID, ObservedAt: p.clock, IngestedAt: p.clock,
		Status: scope.GenerationStatusPending, TriggerKind: scope.TriggerKindSnapshot,
		FreshnessHint: "hint-" + generationID, SourceCommitSHA: "sha-" + generationID,
	}
	const factCount = 3
	stream := make(chan facts.Envelope, factCount)
	for i := range factCount {
		stream <- facts.Envelope{
			FactID:        fmt.Sprintf("%s-fact-%d", generationID, i),
			ScopeID:       p.scope.ScopeID,
			GenerationID:  generationID,
			FactKind:      "file",
			StableFactKey: fmt.Sprintf("file:proof/11182:f%d.go", i),
			ObservedAt:    p.clock,
			Payload:       map[string]any{"relative_path": fmt.Sprintf("f%d.go", i)},
		}
	}
	close(stream)
	if err := p.ingest.CommitScopeGeneration(p.ctx, p.scope, generation, stream); err != nil {
		p.t.Fatalf("commit %s: %v", generationID, err)
	}
}

// claim claims the next projector row and requires it to be generationID's.
func (p *replayProof) claim(generationID string) projector.ScopeGenerationWork {
	p.t.Helper()
	work, ok, err := p.queue.Claim(p.ctx)
	if err != nil || !ok || work.Generation.GenerationID != generationID {
		p.t.Fatalf("claim = %q ok=%v err=%v, want %s", work.Generation.GenerationID, ok, err, generationID)
	}
	return work
}

// ack Acks work through the production projector Ack.
func (p *replayProof) ack(work projector.ScopeGenerationWork) {
	p.t.Helper()
	if err := p.queue.Ack(p.ctx, work, projectorruntime.Result{}); err != nil {
		p.t.Fatalf("ack %s: %v", work.Generation.GenerationID, err)
	}
}

// budgetRow is one generation's canonical projector work row and its
// retry-budget state.
type budgetRow struct {
	id             string
	status         string
	attempts       int
	admissionWaits int
	baseline       int
	reopened       bool
}

func (p *replayProof) work(generationID string) budgetRow {
	p.t.Helper()
	var row budgetRow
	if err := p.db.QueryRowContext(p.ctx, `
SELECT work_item_id, status, attempt_count, admission_wait_attempts, replay_attempt_baseline,
       reopened_at IS NOT NULL
FROM fact_work_items
WHERE stage = 'projector' AND generation_id = $1 AND work_item_id = 'projector_' || scope_id || '_' || generation_id`,
		generationID).Scan(&row.id, &row.status, &row.attempts, &row.admissionWaits, &row.baseline, &row.reopened); err != nil {
		p.t.Fatalf("read work row %s: %v", generationID, err)
	}
	return row
}

// makeVisible lets the next claim take generationID's pending or retrying
// rows now instead of after their backoff.
func (p *replayProof) makeVisible(generationID string) {
	p.t.Helper()
	if _, err := p.db.ExecContext(p.ctx, `UPDATE fact_work_items
SET visible_at = now() - interval '1 second', next_attempt_at = NULL
WHERE stage = 'projector' AND generation_id = $1 AND status IN ('pending', 'retrying')`, generationID); err != nil {
		p.t.Fatalf("make %s visible: %v", generationID, err)
	}
}

// failOnce claims generationID's row, optionally records the write-start
// marker the way a projection that began canonical writes does, and fails it
// with cause through the production Fail path. It returns the claimed work.
func (p *replayProof) failOnce(generationID string, cause error, marker bool) projector.ScopeGenerationWork {
	p.t.Helper()
	p.makeVisible(generationID)
	work := p.claim(generationID)
	if marker {
		if err := p.queue.MarkProjectionWriteStarted(p.ctx, work); err != nil {
			p.t.Fatalf("mark write started %s: %v", generationID, err)
		}
	}
	if err := p.queue.Fail(p.ctx, work, cause); err != nil {
		p.t.Fatalf("fail %s: %v", generationID, err)
	}
	return work
}

// failUntilDeadLetter fails generationID's row with cause until it
// dead-letters and returns how many failures that took. It stops at limit so
// an unbounded budget fails the test instead of looping.
func (p *replayProof) failUntilDeadLetter(generationID string, cause error, limit int, marker bool) int {
	p.t.Helper()
	for n := 1; n <= limit; n++ {
		p.failOnce(generationID, cause, marker)
		switch row := p.work(generationID); row.status {
		case "dead_letter":
			return n
		case "retrying":
		default:
			p.t.Fatalf("failure %d: work = %+v, want retrying or dead_letter", n, row)
		}
	}
	p.t.Fatalf("%s did not dead-letter within %d failures of %T", generationID, limit, cause)
	return 0
}

// deadLetterAtThree dead-letters generationID's row the way the #10972 QA
// canary row was: three projection failures, attempt 3, no admission waits.
func (p *replayProof) deadLetterAtThree(generationID string, marker bool) budgetRow {
	p.t.Helper()
	if n := p.failUntilDeadLetter(generationID, proofProjectionError{}, 10, marker); n != 3 {
		p.t.Fatalf("new row dead-lettered after %d projection failures, want 3", n)
	}
	row := p.work(generationID)
	if row.attempts != 3 || row.admissionWaits != 0 || row.baseline != 0 {
		p.t.Fatalf("dead letter = %+v, want attempt 3, no admission waits, baseline 0", row)
	}
	return row
}

// replay replays one work item through site and requires exactly one row to
// move.
func (p *replayProof) replay(site replaySite, workItemID string) {
	p.t.Helper()
	if got := p.replayCount(site, workItemID); got != 1 {
		p.t.Fatalf("%s replay of %s moved %d rows, want 1", site, workItemID, got)
	}
}

// replayCount replays through site and returns how many rows moved. The admin
// site posts POST /api/v0/admin/replay with a fresh idempotency key, no force
// and no auth context (the dev-mode open route), so it runs the unsafe-class
// exclusion, the idempotency ledger and the admin store. The recovery sites
// select the proof scope's projector rows.
func (p *replayProof) replayCount(site replaySite, workItemID string) int {
	p.t.Helper()
	switch site {
	case replayViaAdmin:
		got := p.adminReplay(workItemID, fmt.Sprintf("11182-%d", p.keys.Add(1)))
		count, _ := got["replayed_count"].(float64)
		return int(count)
	case replayViaRecovery, replayViaRecoveryBounded:
		filter := recovery.ReplayFilter{Stage: recovery.StageProjector, ScopeIDs: []string{p.scope.ScopeID}}
		if site == replayViaRecoveryBounded {
			filter.Limit = 10
		}
		result, err := p.recovery.ReplayFailedWorkItems(p.ctx, filter, time.Now().UTC())
		if err != nil {
			p.t.Fatalf("%s replay: %v", site, err)
		}
		return result.Replayed
	}
	p.t.Fatalf("unknown replay site %q", site)
	return 0
}

// adminReplay posts one admin replay request with a fixed idempotency key and
// returns the decoded body.
func (p *replayProof) adminReplay(workItemID, key string) map[string]any {
	p.t.Helper()
	w := testutil.PostJSON(p.mux, "/api/v0/admin/replay", map[string]any{
		"work_item_ids":   []string{workItemID},
		"reason":          "#11182 proof: replay a dead-lettered projector row",
		"idempotency_key": key,
		"operator_note":   "#11182 proof",
	})
	if w.Code != http.StatusOK {
		p.t.Fatalf("admin replay %s status = %d, want 200; body: %s", workItemID, w.Code, w.Body.String())
	}
	return testutil.DecodeResponseBody(p.t, w)
}

// requireReplayed checks the replay gave the row a new row's allowance and
// changed nothing else on it: pending with reopened_at stamped, attempt_count
// kept (never lowered), baseline equal to it, admission waits reset to 0.
func (p *replayProof) requireReplayed(generationID string, attempts int) {
	p.t.Helper()
	row := p.work(generationID)
	if row.status != "pending" || row.attempts != attempts || row.baseline != attempts || row.admissionWaits != 0 || !row.reopened {
		p.t.Fatalf("after replay: work = %+v, want pending, attempt %d kept, baseline %d, no admission waits, reopened_at set",
			row, attempts, attempts)
	}
}
