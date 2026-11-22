// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package poison_test

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #11217 disposable PostgreSQL 18 invariant table for the poison-liveness sweep's replay retry allowance: stage x attempt_count x admission waits, replay_attempt_baseline <= attempt_count after the sweep in every cell, projector rows raised to attempt 1 and given baseline = attempt_count, reducer rows keep attempt_count and baseline; bootstrap-derived isolated schema; wall time is logged, not gated
//eshu:readiness:env ESHU_POISON_LIVENESS_PROOF_DSN

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/poison"
)

// allowanceCell is one seeded dead letter of the invariant table.
type allowanceCell struct {
	stage    string
	attempts int
	waits    int
}

func (c allowanceCell) id() string {
	return fmt.Sprintf("%s-a%d-w%d", c.stage, c.attempts, c.waits)
}

// TestPoisonSweepRetryAllowanceInvariantTable seeds one dead letter for each
// cell of stage {projector, reducer} x attempt_count {0, 1, 3} x
// admission_wait_attempts {0, 2}, runs one sweep, and checks the counters the
// #11217 SET list writes. The grid pins the SET arithmetic, so it includes
// cells no production writer makes: admission waits above attempt_count, and
// any admission waits on a reducer row (only the projector admission retry
// increments the counter, so the fragment's reset is a no-op on real reducer
// rows). The production-path cells (projector attempt 3, the attempt-0
// operator skip, the admission budget) are proven through the real
// ProjectorQueue in go/internal/query/admin/poison_sweep_allowance_live_test.go.
//
// In every cell replay_attempt_baseline <= attempt_count after the sweep. A
// projector row ends at attempt GREATEST(attempt, 1) with baseline equal to
// it and no admission waits. A reducer row keeps attempt_count and its
// baseline (0): the reducer budget does not read the baseline until #11184.
func TestPoisonSweepRetryAllowanceInvariantTable(t *testing.T) {
	dsn := os.Getenv("ESHU_POISON_LIVENESS_PROOF_DSN")
	if dsn == "" {
		t.Skip("set ESHU_POISON_LIVENESS_PROOF_DSN to run the poison sweep invariant table")
	}
	start := time.Now()
	var cells []allowanceCell
	for _, stage := range []string{"projector", "reducer"} {
		for _, attempts := range []int{0, 1, 3} {
			for _, waits := range []int{0, 2} {
				cells = append(cells, allowanceCell{stage: stage, attempts: attempts, waits: waits})
			}
		}
	}
	db := openPoisonLivenessProofDB(t, dsn)
	provisionPoisonLivenessSchema(t, db, allowanceSeedSQL(cells))

	result, err := poison.NewStore(postgres.SQLDB{DB: db}).RecoverDeadLetters(context.Background(),
		poison.Policy{MaxRecoverAttempts: 1, BatchLimit: poison.DefaultBatchLimit}, time.Now().UTC())
	if err != nil {
		t.Fatalf("RecoverDeadLetters: %v", err)
	}
	if result.Recovered != len(cells) {
		t.Fatalf("sweep moved %d rows, want every cell (%d)", result.Recovered, len(cells))
	}
	for _, cell := range cells {
		var status string
		var attempts, waits, baseline, recoveries int
		if err := db.QueryRowContext(context.Background(), `
SELECT status, attempt_count, admission_wait_attempts, replay_attempt_baseline,
       COALESCE((payload ->> 'poison_recovery_attempts')::int, 0)
FROM fact_work_items WHERE work_item_id = $1`, "wi-"+cell.id()).Scan(
			&status, &attempts, &waits, &baseline, &recoveries); err != nil {
			t.Fatalf("%s: read row: %v", cell.id(), err)
		}
		if status != "pending" || recoveries != 1 {
			t.Fatalf("%s: status %s, recoveries %d; want pending, 1", cell.id(), status, recoveries)
		}
		if baseline > attempts {
			t.Fatalf("%s: baseline %d > attempt_count %d", cell.id(), baseline, attempts)
		}
		if waits != 0 {
			t.Fatalf("%s: admission waits %d, want 0", cell.id(), waits)
		}
		wantAttempts, wantBaseline := cell.attempts, 0
		if cell.stage == "projector" {
			wantAttempts = max(cell.attempts, 1)
			wantBaseline = wantAttempts
		}
		if attempts != wantAttempts || baseline != wantBaseline {
			t.Fatalf("%s: attempt %d, baseline %d; want attempt %d, baseline %d",
				cell.id(), attempts, baseline, wantAttempts, wantBaseline)
		}
	}
	t.Logf("wall time %s", time.Since(start))
}

// allowanceSeedSQL seeds one scope, one failed generation and one dead letter
// per cell. Each scope has a single generation, so every row is in the
// poison class.
func allowanceSeedSQL(cells []allowanceCell) string {
	var b strings.Builder
	for _, cell := range cells {
		id := cell.id()
		domain := "source_local"
		if cell.stage == "reducer" {
			domain = "workload_materialization"
		}
		fmt.Fprintf(&b, `
INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status)
VALUES ('scope-%[1]s', 'repository', 'git', 'key-%[1]s', 'git', 'p', now(), now(), 'active');
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status)
VALUES ('gen-%[1]s', 'scope-%[1]s', 'snapshot', now(), now(), 'failed');
INSERT INTO fact_work_items (work_item_id, scope_id, generation_id, stage, domain, status, attempt_count,
    admission_wait_attempts, failure_class, created_at, updated_at)
VALUES ('wi-%[1]s', 'scope-%[1]s', 'gen-%[1]s', '%[2]s', '%[3]s', 'dead_letter', %[4]d, %[5]d,
    'invariant_table', now() - interval '1 hour', now() - interval '1 hour');
`, id, cell.stage, domain, cell.attempts, cell.waits)
	}
	return b.String()
}
