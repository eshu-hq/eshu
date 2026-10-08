// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/projector/failure"
	"github.com/eshu-hq/eshu/go/internal/projector/runtime"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// zombieHealOutcome... pins the closed outcome label set of the heal
// counter. These literals are the wire contract: the implementation must
// emit exactly these values.
const (
	wantZombieHealHealed   = "healed"
	wantZombieHealInFlight = "skipped_in_flight"
	wantZombieHealOpen     = "skipped_already_open"
	wantZombieHealNoMarker = "skipped_no_write_marker"
	wantZombieHealNoActive = "skipped_no_active_generation"
)

// zombieHealProofDB returns a fresh proof schema with scope-zh published at
// gen-new: gen-new is active with a succeeded canonical projector row (the
// normal activation lifecycle state). seedSQL adds the zombie generation and
// its running projector row. The work structs these tests build deliberately
// carry no Generation.Status: the #7209 heal gate reads the durable write
// marker, never the client-supplied claim snapshot.
func zombieHealProofDB(t *testing.T, seedSQL string) *sql.DB {
	t.Helper()
	dsn := os.Getenv("ESHU_PROJECTOR_SUPERSESSION_PROOF_DSN")
	if dsn == "" {
		t.Skip("set ESHU_PROJECTOR_SUPERSESSION_PROOF_DSN to a disposable Postgres database")
	}
	database := openLivenessProofDB(t, dsn)
	provisionLivenessSchema(t, database, `
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status, active_generation_id
) VALUES ('scope-zh', 'repository', 'github', 'proof/zh', 'git',
          'proof/zh', now(), now(), 'active', 'gen-new');
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, observed_at, ingested_at,
    status, activated_at
) VALUES ('gen-new', 'scope-zh', 'push', now(), now(), 'active', now());
INSERT INTO fact_work_items (
    work_item_id, scope_id, generation_id, stage, domain, status,
    attempt_count, lease_owner, claim_until, visible_at, payload,
    created_at, updated_at
) VALUES ('projector_scope-zh_gen-new', 'scope-zh', 'gen-new', 'projector',
          'source_local', 'succeeded', 1, NULL, NULL, NULL,
          '{}'::jsonb, now(), now());
`+seedSQL)
	return database
}

// zombieHealRowState is the active generation's canonical projector row after
// a refused zombie attempt: its status, lease, attempt, heal count, and how
// many canonical rows exist (a duplicate reopen would show here).
type zombieHealRowState struct {
	status        string
	leaseOwner    sql.NullString
	attemptCount  int
	healCount     sql.NullString
	canonicalRows int
	zombieStatus  string
	zombieClass   string
}

func readZombieHealRowState(t *testing.T, database *sql.DB, zombieWorkID string) zombieHealRowState {
	t.Helper()
	var state zombieHealRowState
	var failureClass sql.NullString
	if err := database.QueryRowContext(context.Background(), `
SELECT work.status, work.lease_owner, work.attempt_count,
       work.payload ->> 'zombie_heal_count',
       (SELECT COUNT(*) FROM fact_work_items
        WHERE work_item_id = 'projector_scope-zh_gen-new'),
       zombie.status, zombie.failure_class
FROM fact_work_items AS work
JOIN fact_work_items AS zombie ON zombie.work_item_id = $1
WHERE work.work_item_id = 'projector_scope-zh_gen-new'`, zombieWorkID).Scan(
		&state.status, &state.leaseOwner, &state.attemptCount,
		&state.healCount, &state.canonicalRows,
		&state.zombieStatus, &failureClass,
	); err != nil {
		t.Fatalf("read zombie-heal state: %v", err)
	}
	state.zombieClass = failureClass.String
	return state
}

func zombieHealWork(generationID string, attempt int) projector.ScopeGenerationWork {
	return projector.ScopeGenerationWork{
		Scope:        scope.IngestionScope{ScopeID: "scope-zh"},
		Generation:   scope.ScopeGeneration{GenerationID: generationID},
		AttemptCount: attempt,
	}
}

// zombieOldSeed seeds gen-old as a superseded generation the zombie wrote:
// the write marker is set, so the zombie may have retracted gen-new's nodes
// after gen-new published. marked=false leaves the marker NULL for the
// provable-non-writer control.
func zombieOldSeed(marked bool) string {
	marker := "NULL"
	if marked {
		marker = "now() - interval '30 minutes'"
	}
	return `
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, observed_at, ingested_at,
    status, activated_at, superseded_at, projection_write_started_at
) VALUES ('gen-old', 'scope-zh', 'push', now() - interval '2 hours',
          now() - interval '2 hours', 'superseded',
          now() - interval '2 hours', now() - interval '1 hour', ` + marker + `);
INSERT INTO fact_work_items (
    work_item_id, scope_id, generation_id, stage, domain, status,
    attempt_count, lease_owner, claim_until, visible_at, payload,
    created_at, updated_at
) VALUES ('projector_scope-zh_gen-old', 'scope-zh', 'gen-old', 'projector',
          'source_local', 'running', 1, 'zombie-worker',
          now() - interval '1 minute', now(), '{}'::jsonb, now(), now());
`
}

// TestProjectorRefusalHealsMarkedSupersededGeneration is the #7209
// regression. A zombie whose marked generation a newer Ack retired must stop
// at its next Heartbeat or Ack with ErrWorkSuperseded, and the refusal must
// re-open the scope's current active generation projector row (pending, one
// canonical row, heal counted) so the successor re-projects over any retract
// damage the zombie did after the successor published.
func TestProjectorRefusalHealsMarkedSupersededGeneration(t *testing.T) {
	for _, tc := range []struct {
		name   string
		refuse func(ProjectorQueue, projector.ScopeGenerationWork) error
		class  string
	}{
		{"ack", func(q ProjectorQueue, w projector.ScopeGenerationWork) error {
			return q.Ack(context.Background(), w, runtime.Result{})
		}, projectorAckGenerationSupersededClass},
		{"heartbeat", func(q ProjectorQueue, w projector.ScopeGenerationWork) error {
			return q.Heartbeat(context.Background(), w)
		}, projectorHeartbeatGenerationSupersededClass},
	} {
		t.Run(tc.name, func(t *testing.T) {
			database := zombieHealProofDB(t, zombieOldSeed(true))
			queue := NewProjectorQueue(SQLDB{DB: database}, "zombie-worker", time.Minute)
			instruments, reader := newEnqueueInstruments(t)
			queue.Instruments = instruments

			work := zombieHealWork("gen-old", 1)
			if err := tc.refuse(queue, work); !errors.Is(err, failure.ErrWorkSuperseded) {
				t.Fatalf("%s of marked superseded-generation work = %v, want ErrWorkSuperseded", tc.name, err)
			}
			state := readZombieHealRowState(t, database, "projector_scope-zh_gen-old")
			if state.zombieStatus != "superseded" || state.zombieClass != tc.class {
				t.Fatalf("zombie row = %s/%s, want superseded/%s", state.zombieStatus, state.zombieClass, tc.class)
			}
			if state.status != "pending" || state.leaseOwner.Valid || state.canonicalRows != 1 {
				t.Fatalf("active row = %+v, want one pending canonical row with no lease", state)
			}
			if !state.healCount.Valid || state.healCount.String != "1" {
				t.Fatalf("zombie_heal_count = %+v, want 1", state.healCount)
			}
			assertZombieHealOutcome(t, reader, wantZombieHealHealed, 1)
		})
	}
}

// TestProjectorRepeatedRefusalsOpenOneActiveRow proves the heal is idempotent:
// two zombie attempts refused for the same marked generation re-open one
// active row, and the second refusal neither duplicates the row nor counts a
// second heal.
func TestProjectorRepeatedRefusalsOpenOneActiveRow(t *testing.T) {
	database := zombieHealProofDB(t, zombieOldSeed(true)+`
INSERT INTO fact_work_items (
    work_item_id, scope_id, generation_id, stage, domain, status,
    attempt_count, lease_owner, claim_until, visible_at, payload,
    created_at, updated_at
) VALUES ('refinalize_scope-zh_gen-old', 'scope-zh', 'gen-old', 'projector',
          'source_local', 'running', 2, 'zombie-worker-2',
          now() - interval '1 minute', now(), '{}'::jsonb, now(), now());
`)
	instruments, reader := newEnqueueInstruments(t)

	first := NewProjectorQueue(SQLDB{DB: database}, "zombie-worker", time.Minute)
	first.Instruments = instruments
	if err := first.Heartbeat(context.Background(), zombieHealWork("gen-old", 1)); !errors.Is(err, failure.ErrWorkSuperseded) {
		t.Fatalf("first Heartbeat = %v, want ErrWorkSuperseded", err)
	}
	second := NewProjectorQueue(SQLDB{DB: database}, "zombie-worker-2", time.Minute)
	second.Instruments = instruments
	if err := second.Heartbeat(context.Background(), zombieHealWork("gen-old", 2)); !errors.Is(err, failure.ErrWorkSuperseded) {
		t.Fatalf("second Heartbeat = %v, want ErrWorkSuperseded", err)
	}

	state := readZombieHealRowState(t, database, "projector_scope-zh_gen-old")
	if state.status != "pending" || state.canonicalRows != 1 {
		t.Fatalf("active row = %+v, want one pending canonical row", state)
	}
	if !state.healCount.Valid || state.healCount.String != "1" {
		t.Fatalf("zombie_heal_count = %+v, want 1", state.healCount)
	}
	assertZombieHealOutcome(t, reader, wantZombieHealHealed, 1)
	assertZombieHealOutcome(t, reader, wantZombieHealOpen, 1)
}

// TestProjectorRefusalSkipsHealWithoutWriteMarker pins the gate's negative
// half: a refused generation that never marked provably never wrote, so the
// refusal must leave the active generation's succeeded row alone.
func TestProjectorRefusalSkipsHealWithoutWriteMarker(t *testing.T) {
	database := zombieHealProofDB(t, zombieOldSeed(false))
	queue := NewProjectorQueue(SQLDB{DB: database}, "zombie-worker", time.Minute)
	instruments, reader := newEnqueueInstruments(t)
	queue.Instruments = instruments

	if err := queue.Ack(context.Background(), zombieHealWork("gen-old", 1), runtime.Result{}); !errors.Is(err, failure.ErrWorkSuperseded) {
		t.Fatalf("Ack of unmarked superseded-generation work = %v, want ErrWorkSuperseded", err)
	}
	state := readZombieHealRowState(t, database, "projector_scope-zh_gen-old")
	if state.zombieStatus != "superseded" || state.zombieClass != projectorAckGenerationSupersededClass {
		t.Fatalf("zombie row = %s/%s, want superseded/%s", state.zombieStatus, state.zombieClass, projectorAckGenerationSupersededClass)
	}
	if state.status != "succeeded" || state.canonicalRows != 1 {
		t.Fatalf("active row = %+v, want the one succeeded row untouched", state)
	}
	if state.healCount.Valid {
		t.Fatalf("zombie_heal_count = %+v, want NULL", state.healCount)
	}
	assertZombieHealOutcome(t, reader, wantZombieHealNoMarker, 1)
}

// TestProjectorSupersededByNewerSkipsHeal pins the Heartbeat sub-case (ii)
// exclusion: work a newer pending generation replaces never started writing
// (the trigger requires a NULL marker), so stopping it must not heal.
func TestProjectorSupersededByNewerSkipsHeal(t *testing.T) {
	database := zombieHealProofDB(t, `
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, observed_at, ingested_at, status
) VALUES ('gen-old', 'scope-zh', 'push', now() - interval '2 hours',
          now() - interval '2 hours', 'pending'),
         ('gen-newer', 'scope-zh', 'push', now() - interval '1 hour',
          now() - interval '1 hour', 'pending');
INSERT INTO fact_work_items (
    work_item_id, scope_id, generation_id, stage, domain, status,
    attempt_count, lease_owner, claim_until, visible_at, payload,
    created_at, updated_at
) VALUES ('projector_scope-zh_gen-old', 'scope-zh', 'gen-old', 'projector',
          'source_local', 'running', 1, 'zombie-worker',
          now() + interval '1 minute', now(), '{}'::jsonb, now(), now());
`)
	queue := NewProjectorQueue(SQLDB{DB: database}, "zombie-worker", time.Minute)
	instruments, reader := newEnqueueInstruments(t)
	queue.Instruments = instruments

	err := queue.Heartbeat(context.Background(), zombieHealWork("gen-old", 1))
	if !errors.Is(err, failure.ErrWorkSuperseded) {
		t.Fatalf("Heartbeat of replaced work = %v, want ErrWorkSuperseded", err)
	}
	if fc := supersededFailureClass(err); fc != "projector_superseded_by_newer_generation" {
		t.Fatalf("refusal class = %q, want projector_superseded_by_newer_generation", fc)
	}
	state := readZombieHealRowState(t, database, "projector_scope-zh_gen-old")
	if state.status != "succeeded" || state.canonicalRows != 1 {
		t.Fatalf("active row = %+v, want the one succeeded row untouched", state)
	}
	assertZombieHealTotal(t, reader, 0)
}

// TestProjectorRefusalSkipsHealWithoutActiveGeneration pins the no-target
// half: a marked refusal with no other active generation (a NULL scope
// pointer after the successor failed) must leave every row alone.
func TestProjectorRefusalSkipsHealWithoutActiveGeneration(t *testing.T) {
	database := zombieHealProofDB(t, zombieOldSeed(true))
	if _, err := database.ExecContext(context.Background(), `
UPDATE ingestion_scopes SET active_generation_id = NULL WHERE scope_id = 'scope-zh';
UPDATE scope_generations SET status = 'failed', activated_at = NULL WHERE generation_id = 'gen-new';
UPDATE fact_work_items SET status = 'dead_letter' WHERE work_item_id = 'projector_scope-zh_gen-new'`); err != nil {
		t.Fatalf("stage failed successor: %v", err)
	}
	queue := NewProjectorQueue(SQLDB{DB: database}, "zombie-worker", time.Minute)
	instruments, reader := newEnqueueInstruments(t)
	queue.Instruments = instruments

	if err := queue.Ack(context.Background(), zombieHealWork("gen-old", 1), runtime.Result{}); !errors.Is(err, failure.ErrWorkSuperseded) {
		t.Fatalf("Ack of marked superseded-generation work = %v, want ErrWorkSuperseded", err)
	}
	state := readZombieHealRowState(t, database, "projector_scope-zh_gen-old")
	if state.status != "dead_letter" || state.canonicalRows != 1 {
		t.Fatalf("active row = %+v, want the dead_letter row untouched", state)
	}
	if state.healCount.Valid {
		t.Fatalf("zombie_heal_count = %+v, want NULL", state.healCount)
	}
	assertZombieHealOutcome(t, reader, wantZombieHealNoActive, 1)
}

// TestProjectorRefusalSkipsHealWhenActiveRowInFlight pins the write-time
// guard: when the active generation already has live projector work, the heal
// must neither clobber that claim nor count a heal.
func TestProjectorRefusalSkipsHealWhenActiveRowInFlight(t *testing.T) {
	database := zombieHealProofDB(t, zombieOldSeed(true))
	if _, err := database.ExecContext(context.Background(), `
UPDATE fact_work_items
SET status = 'running', attempt_count = 3, lease_owner = 'live-worker',
    claim_until = now() + interval '1 minute', updated_at = now()
WHERE work_item_id = 'projector_scope-zh_gen-new'`); err != nil {
		t.Fatalf("stage live active row: %v", err)
	}
	queue := NewProjectorQueue(SQLDB{DB: database}, "zombie-worker", time.Minute)
	instruments, reader := newEnqueueInstruments(t)
	queue.Instruments = instruments

	if err := queue.Ack(context.Background(), zombieHealWork("gen-old", 1), runtime.Result{}); !errors.Is(err, failure.ErrWorkSuperseded) {
		t.Fatalf("Ack of marked superseded-generation work = %v, want ErrWorkSuperseded", err)
	}
	state := readZombieHealRowState(t, database, "projector_scope-zh_gen-old")
	if state.status != "running" || !state.leaseOwner.Valid || state.leaseOwner.String != "live-worker" || state.attemptCount != 3 {
		t.Fatalf("active row = %+v, want the live running claim untouched", state)
	}
	if state.healCount.Valid {
		t.Fatalf("zombie_heal_count = %+v, want NULL", state.healCount)
	}
	assertZombieHealOutcome(t, reader, wantZombieHealInFlight, 1)
}

// zombieHealOutcomeCounts collects the heal counter by outcome label.
func zombieHealOutcomeCounts(t *testing.T, reader interface {
	Collect(context.Context, *metricdata.ResourceMetrics) error
},
) map[string]int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect metrics: %v", err)
	}
	counts := map[string]int64{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "eshu_dp_projector_zombie_heal_total" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("heal counter data = %T, want Sum[int64]", m.Data)
			}
			for _, dp := range sum.DataPoints {
				outcome := ""
				for _, attr := range dp.Attributes.ToSlice() {
					if string(attr.Key) == telemetry.MetricDimensionOutcome {
						outcome = attr.Value.AsString()
					}
				}
				counts[outcome] += dp.Value
			}
		}
	}
	return counts
}

// assertZombieHealOutcome fails unless the heal counter recorded want for
// the outcome label.
func assertZombieHealOutcome(t *testing.T, reader interface {
	Collect(context.Context, *metricdata.ResourceMetrics) error
}, outcome string, want int64,
) {
	t.Helper()
	if got := zombieHealOutcomeCounts(t, reader)[outcome]; got != want {
		t.Fatalf("heal counter outcome %q = %d, want %d", outcome, got, want)
	}
}

// assertZombieHealTotal fails unless the heal counter recorded want across
// all outcomes.
func assertZombieHealTotal(t *testing.T, reader interface {
	Collect(context.Context, *metricdata.ResourceMetrics) error
}, want int64,
) {
	t.Helper()
	var total int64
	for _, n := range zombieHealOutcomeCounts(t, reader) {
		total += n
	}
	if total != want {
		t.Fatalf("heal counter total = %d, want %d", total, want)
	}
}
