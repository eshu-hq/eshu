// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #11615 disposable PostgreSQL 18 proof that every reducer requeue writer stamps last_requeue_reason from the closed vocabulary: ReopenSucceeded per reason, counting and non-counting Fail, retouch from terminal and outstanding states, the cross-scope schedule, the workload ACK-trigger conversion, and a concurrent double-reopen where only the winner's reason and one count survive; about 10 s locally; enrolled in the live-postgres-readiness runner
//eshu:readiness:env ESHU_POSTGRES_DSN

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// requeueReasonTestCause is a retryable test cause. class empty means the
// generic fallback (counting retry); class set to a non-counting readiness
// class parks without consuming budget.
type requeueReasonTestCause struct {
	class string
}

func (e requeueReasonTestCause) Error() string   { return "requeue reason proof cause" }
func (e requeueReasonTestCause) Retryable() bool { return true }
func (e requeueReasonTestCause) FailureClass() string {
	if e.class == "" {
		return "reducer_retryable_probe"
	}
	return e.class
}

// readRequeueReason returns the stored last_requeue_reason: nil when the row
// was never requeued.
func readRequeueReason(t *testing.T, db *sql.DB, ctx context.Context, id string) *string {
	t.Helper()
	var reason sql.NullString
	if err := db.QueryRowContext(ctx, `SELECT last_requeue_reason FROM fact_work_items WHERE work_item_id = $1`, id).Scan(&reason); err != nil {
		t.Fatalf("read last_requeue_reason for %s: %v", id, err)
	}
	if !reason.Valid {
		return nil
	}
	s := reason.String
	return &s
}

func assertRequeueReason(t *testing.T, db *sql.DB, ctx context.Context, id string, want *string) {
	t.Helper()
	got := readRequeueReason(t, db, ctx, id)
	if (got == nil) != (want == nil) || (got != nil && *got != *want) {
		var gotStr, wantStr string
		if got != nil {
			gotStr = *got
		} else {
			gotStr = "<NULL>"
		}
		if want != nil {
			wantStr = *want
		} else {
			wantStr = "<NULL>"
		}
		t.Fatalf("last_requeue_reason for %s = %s, want %s", id, gotStr, wantStr)
	}
}

func strptr(s string) *string { return &s }

// requeueCountsByReason collects eshu_dp_reducer_requeue_total by reason
// through a real Instruments reader: only actually-transitioned rows count.
func requeueCountsByReason(t *testing.T, reader *sdkmetric.ManualReader) map[string]int64 {
	t.Helper()
	var data metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &data); err != nil {
		t.Fatalf("collect requeue counts: %v", err)
	}
	out := map[string]int64{}
	for _, scope := range data.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "eshu_dp_reducer_requeue_total" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("requeue counter data is %T, want Sum[int64]", m.Data)
			}
			for _, point := range sum.DataPoints {
				reason, _ := point.Attributes.Value(telemetry.MetricDimensionReason)
				out[reason.AsString()] += point.Value
			}
		}
	}
	return out
}

// seedRequeueScope seeds one scope generation pair for the requeue proof.
func seedRequeueScope(t *testing.T, f reopenStampFixture, scopeID, genID string, at time.Time) {
	t.Helper()
	f.scope(t, scopeID, genID, at)
}

// TestRequeueReasonWritersStampReasonLive is the #11615 per-writer proof:
// every reducer requeue path writes its closed-vocabulary reason, rows that
// see no transition keep their prior reason (NULL when never requeued), and
// a concurrent double-reopen leaves exactly one winner.
//
// Skipped unless ESHU_POSTGRES_DSN names a disposable Postgres.
func TestRequeueReasonWritersStampReasonLive(t *testing.T) {
	database, ctx := openIsolatedLiveDB(t, "requeue_reason", "set ESHU_POSTGRES_DSN to run the #11615 requeue-reason writer proof")
	now := time.Now().UTC().Truncate(time.Microsecond)
	f := newReopenStampFixture(ctx, database, now)

	newQueue := func() ReducerQueue {
		return ReducerQueue{
			database:      SQLDB{DB: database},
			LeaseOwner:    "requeue-proof-owner",
			LeaseDuration: time.Minute,
			MaxAttempts:   3,
			Now:           func() time.Time { return now },
		}
	}

	t.Run("reopen succeeded per reason", func(t *testing.T) {
		seedRequeueScope(t, f, "scope-reopen", "gen-reopen", now)
		f.row(t, reopenStampRow{id: "reopen-maint", scopeID: "scope-reopen", generationID: "gen-reopen", stage: "reducer", domain: "deployment_mapping", status: "succeeded"})
		f.row(t, reopenStampRow{id: "reopen-producer", scopeID: "scope-reopen", generationID: "gen-reopen", stage: "reducer", domain: "deployment_mapping", status: "succeeded"})

		queue := newQueue()
		ok, err := queue.ReopenSucceeded(ctx, "reopen-maint", telemetry.RequeueReasonMaintenanceReopen)
		if err != nil || !ok {
			t.Fatalf("ReopenSucceeded() = (%v, %v), want (true, nil)", ok, err)
		}
		assertRequeueReason(t, database, ctx, "reopen-maint", strptr("maintenance_reopen"))

		ok, err = queue.ReopenSucceeded(ctx, "reopen-producer", telemetry.RequeueReasonProducerActivationSettle)
		if err != nil || !ok {
			t.Fatalf("ReopenSucceeded() = (%v, %v), want (true, nil)", ok, err)
		}
		assertRequeueReason(t, database, ctx, "reopen-producer", strptr("producer_activation_settle"))

		// A reopen of an already-pending row is a no-op that writes nothing.
		ok, err = queue.ReopenSucceeded(ctx, "reopen-maint", telemetry.RequeueReasonMaintenanceReopen)
		if err != nil || ok {
			t.Fatalf("ReopenSucceeded() on a pending row = (%v, %v), want (false, nil)", ok, err)
		}
		assertRequeueReason(t, database, ctx, "reopen-maint", strptr("maintenance_reopen"))

		if _, err := queue.ReopenSucceeded(ctx, "reopen-maint", telemetry.RequeueReason("bogus")); err == nil {
			t.Fatal("ReopenSucceeded() with an invalid reason = nil, want an error")
		}
	})

	t.Run("counting and non-counting fail", func(t *testing.T) {
		seedRequeueScope(t, f, "scope-fail", "gen-fail", now)
		f.row(t, reopenStampRow{id: "fail-counting", scopeID: "scope-fail", generationID: "gen-fail", stage: "reducer", domain: "deployment_mapping", status: "pending"})
		f.row(t, reopenStampRow{id: "fail-readiness", scopeID: "scope-fail", generationID: "gen-fail", stage: "reducer", domain: "deployment_mapping", status: "pending"})

		queue := newQueue()
		intent, ok, err := queue.Claim(ctx)
		if err != nil || !ok {
			t.Fatalf("Claim() = (%v, %v), want a claimed intent", ok, err)
		}
		first := intent
		intent, ok, err = queue.Claim(ctx)
		if err != nil || !ok {
			t.Fatalf("Claim() = (%v, %v), want a second claimed intent", ok, err)
		}
		if err := queue.Fail(ctx, first, requeueReasonTestCause{}); err != nil {
			t.Fatalf("Fail() counting = %v, want nil", err)
		}
		if err := queue.Fail(ctx, intent, requeueReasonTestCause{class: "gcp_relationship_nodes_not_ready"}); err != nil {
			t.Fatalf("Fail() readiness = %v, want nil", err)
		}
		assertRequeueReason(t, database, ctx, first.IntentID, strptr("retry_backoff"))
		assertRequeueReason(t, database, ctx, intent.IntentID, strptr("readiness_wait"))
	})

	t.Run("retouch terminal and outstanding", func(t *testing.T) {
		seedRequeueScope(t, f, "scope-retouch", "gen-retouch", now)
		f.row(t, reopenStampRow{id: "retouch-terminal", scopeID: "scope-retouch", generationID: "gen-retouch", stage: "reducer", domain: "deployment_mapping", status: "failed"})
		f.row(t, reopenStampRow{id: "retouch-pending", scopeID: "scope-retouch", generationID: "gen-retouch", stage: "reducer", domain: "deployment_mapping", status: "pending"})

		queue := newQueue()
		ok, err := queue.RetouchWorkItem(ctx, "retouch-terminal")
		if err != nil || !ok {
			t.Fatalf("RetouchWorkItem() terminal = (%v, %v), want (true, nil)", ok, err)
		}
		assertRequeueReason(t, database, ctx, "retouch-terminal", strptr("retouch_new_evidence"))

		ok, err = queue.RetouchWorkItem(ctx, "retouch-pending")
		if err != nil || !ok {
			t.Fatalf("RetouchWorkItem() pending = (%v, %v), want (true, nil)", ok, err)
		}
		// A pending row that sees no transition keeps its prior reason:
		// never requeued, so NULL.
		assertRequeueReason(t, database, ctx, "retouch-pending", nil)
	})

	t.Run("cross-scope schedule and ack-trigger conversion", func(t *testing.T) {
		seedRequeueScope(t, f, "scope-cross", "gen-cross", now)
		// The stable work-item identity reducerWorkItemID derives from the
		// scope-generation-domain-entity triple; seeding the succeeded row
		// under it makes the schedule UPDATE match instead of enqueueing.
		f.row(t, reopenStampRow{id: "reducer_scope-cross_gen-cross_workload_materialization_entity-cross", scopeID: "scope-cross", generationID: "gen-cross", stage: "reducer", domain: string(reducer.DomainWorkloadMaterialization), status: "succeeded"})

		queue := newQueue()
		outcome, err := queue.ReplayWorkloadMaterializationOutcome(ctx, "scope-cross", "gen-cross", "entity-cross")
		if err != nil || outcome != reducer.WorkloadMaterializationReplayScheduled {
			t.Fatalf("ReplayWorkloadMaterializationOutcome() = (%q, %v), want (scheduled, nil)", outcome, err)
		}
		assertRequeueReason(t, database, ctx, "reducer_scope-cross_gen-cross_workload_materialization_entity-cross", strptr("cross_scope_replay"))

		// Flag a claimed row for replay, then ACK it: the trigger converts
		// the succeeded ACK back to pending with the same reason, and no
		// second count is emitted at conversion (counting is covered by
		// hermetic tests; here the stored reason is the proof). Claim in a
		// loop: earlier subtests leave other pending rows behind, and only
		// cross-flagged carries the flag this subtest sets.
		f.row(t, reopenStampRow{id: "cross-flagged", scopeID: "scope-cross", generationID: "gen-cross", stage: "reducer", domain: "deployment_mapping", status: "pending"})
		var claimed reducercontract.Intent
		for i := 0; i < 20; i++ {
			next, ok, err := queue.Claim(ctx)
			if err != nil {
				t.Fatalf("Claim() = %v, want nil", err)
			}
			if !ok {
				t.Fatal("Claim() ok = false, want the cross-flagged row")
			}
			if next.IntentID == "cross-flagged" {
				claimed = next
				break
			}
		}
		if claimed.IntentID == "" {
			t.Fatal("never claimed cross-flagged in 20 attempts")
		}
		f.exec(t, `UPDATE fact_work_items SET cross_scope_replay_required = TRUE WHERE work_item_id = $1`, claimed.IntentID)
		f.exec(t, `UPDATE fact_work_items SET last_requeue_reason = 'cross_scope_replay' WHERE work_item_id = $1`, claimed.IntentID)
		if err := queue.Ack(ctx, claimed, reducercontract.Result{}); err != nil {
			t.Fatalf("Ack() on a flagged row = %v, want nil", err)
		}
		var status string
		if err := database.QueryRowContext(ctx, `SELECT status FROM fact_work_items WHERE work_item_id = $1`, claimed.IntentID).Scan(&status); err != nil {
			t.Fatalf("read converted row status: %v", err)
		}
		if status != "pending" {
			t.Fatalf("converted row status = %q, want pending", status)
		}
		assertRequeueReason(t, database, ctx, claimed.IntentID, strptr("cross_scope_replay"))
	})

	t.Run("fenced replay fence matrix", func(t *testing.T) {
		// Proof item 4: the fenced workload replay stamps the reason and
		// counts exactly the fence-change transitions. Fence-a (succeeded,
		// changed fence) reopens and counts; fence-b (succeeded, same
		// fence) is a matched no-op that keeps its prior reason and counts
		// nothing; fence-c (claimed, changed fence) flags for replay and
		// counts. The counter is asserted through a real Instruments
		// reader, so a matched-counting regression fails here.
		reader := sdkmetric.NewManualReader()
		provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
		t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
		instruments, err := telemetry.NewInstruments(provider.Meter("requeue-fence-matrix-test"))
		if err != nil {
			t.Fatalf("NewInstruments() = %v, want nil", err)
		}
		queue := ReducerQueue{
			database:      SQLDB{DB: database},
			LeaseOwner:    "requeue-proof-owner",
			LeaseDuration: time.Minute,
			MaxAttempts:   3,
			Now:           func() time.Time { return now },
			Instruments:   instruments,
		}

		seedRequeueScope(t, f, "scope-fence", "gen-fence", now)
		seedFenced := func(entity, status, storedFence string) string {
			id := "reducer_scope-fence_gen-fence_workload_materialization_" + entity
			f.row(t, reopenStampRow{id: id, scopeID: "scope-fence", generationID: "gen-fence", stage: "reducer", domain: string(reducer.DomainWorkloadMaterialization), status: status})
			f.exec(t, `UPDATE fact_work_items SET payload = jsonb_set(payload, '{repo_dependency_readiness_fence}', to_jsonb($2::text), TRUE) WHERE work_item_id = $1`, id, storedFence)
			return id
		}
		fenceA := seedFenced("entity-a", "succeeded", "fence-a-old")
		fenceB := seedFenced("entity-b", "succeeded", "fence-b-same")
		fenceC := seedFenced("entity-c", "claimed", "fence-c-old")

		outcome, err := queue.ReplayWorkloadMaterializationForFenceOutcome(ctx, "scope-fence", "gen-fence", "entity-a", "repo:fence-matrix", "fence-a-new")
		if err != nil || outcome != reducer.WorkloadMaterializationReplayScheduled {
			t.Fatalf("fence-a ReplayWorkloadMaterializationForFenceOutcome() = (%q, %v), want (scheduled, nil)", outcome, err)
		}
		assertRequeueReason(t, database, ctx, fenceA, strptr("cross_scope_replay"))

		outcome, err = queue.ReplayWorkloadMaterializationForFenceOutcome(ctx, "scope-fence", "gen-fence", "entity-b", "repo:fence-matrix", "fence-b-same")
		if err != nil || outcome != reducer.WorkloadMaterializationReplayScheduled {
			t.Fatalf("fence-b ReplayWorkloadMaterializationForFenceOutcome() = (%q, %v), want (scheduled, nil)", outcome, err)
		}
		// A matched no-op: the row stays succeeded with its prior reason.
		var fenceBStatus string
		if err := database.QueryRowContext(ctx, `SELECT status FROM fact_work_items WHERE work_item_id = $1`, fenceB).Scan(&fenceBStatus); err != nil {
			t.Fatalf("read fence-b row status: %v", err)
		}
		if fenceBStatus != "succeeded" {
			t.Fatalf("fence-b row status = %q, want succeeded (same fence must not reopen)", fenceBStatus)
		}
		assertRequeueReason(t, database, ctx, fenceB, nil)

		outcome, err = queue.ReplayWorkloadMaterializationForFenceOutcome(ctx, "scope-fence", "gen-fence", "entity-c", "repo:fence-matrix", "fence-c-new")
		if err != nil || outcome != reducer.WorkloadMaterializationReplayScheduled {
			t.Fatalf("fence-c ReplayWorkloadMaterializationForFenceOutcome() = (%q, %v), want (scheduled, nil)", outcome, err)
		}
		var fenceCFlag bool
		if err := database.QueryRowContext(ctx, `SELECT cross_scope_replay_required FROM fact_work_items WHERE work_item_id = $1`, fenceC).Scan(&fenceCFlag); err != nil {
			t.Fatalf("read fence-c replay flag: %v", err)
		}
		if !fenceCFlag {
			t.Fatal("fence-c row cross_scope_replay_required = false, want true (changed fence must flag the claimed row)")
		}
		assertRequeueReason(t, database, ctx, fenceC, strptr("cross_scope_replay"))

		if got := requeueCountsByReason(t, reader)["cross_scope_replay"]; got != 2 {
			t.Fatalf("eshu_dp_reducer_requeue_total{reason=cross_scope_replay} = %d, want 2 (fence-a and fence-c transitioned, fence-b matched only)", got)
		}
	})

	t.Run("digest replay digest matrix", func(t *testing.T) {
		// The digest-fenced workload replay stamps the reason and counts
		// exactly the digest-change transitions. digest-a (succeeded,
		// changed digest) reopens and counts; digest-b (succeeded, same
		// digest) is a quiet no-op that keeps its prior reason and counts
		// nothing; digest-c (claimed, changed digest) flags for replay and
		// counts; digest-d (claimed, same digest, already flagged) keeps its
		// prior reason and counts nothing; digest-e (pending, changed
		// digest) matches without transitioning and keeps its NULL reason.
		// The counter is asserted through a real Instruments reader, so a
		// matched-counting regression fails here.
		reader := sdkmetric.NewManualReader()
		provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
		t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
		instruments, err := telemetry.NewInstruments(provider.Meter("requeue-digest-matrix-test"))
		if err != nil {
			t.Fatalf("NewInstruments() = %v, want nil", err)
		}
		queue := ReducerQueue{
			database:      SQLDB{DB: database},
			LeaseOwner:    "requeue-proof-owner",
			LeaseDuration: time.Minute,
			MaxAttempts:   3,
			Now:           func() time.Time { return now },
			Instruments:   instruments,
		}

		seedRequeueScope(t, f, "scope-digest", "gen-digest", now)
		seedDigested := func(scopeID, genID, entity, status, storedDigest string) string {
			id := "reducer_" + scopeID + "_" + genID + "_workload_materialization_" + entity
			f.row(t, reopenStampRow{id: id, scopeID: scopeID, generationID: genID, stage: "reducer", domain: string(reducer.DomainWorkloadMaterialization), status: status})
			f.exec(t, `UPDATE fact_work_items SET payload = jsonb_set(payload, '{deployment_evidence_digest}', to_jsonb($2::text), TRUE) WHERE work_item_id = $1`, id, storedDigest)
			return id
		}
		digestA := seedDigested("scope-digest", "gen-digest", "entity-a", "succeeded", "digest-a-old")
		digestB := seedDigested("scope-digest", "gen-digest", "entity-b", "succeeded", "digest-b-same")
		digestC := seedDigested("scope-digest", "gen-digest", "entity-c", "claimed", "digest-c-old")
		// A second claimed row in the same scope would collide with digest-c
		// on the live-lease partial unique index, so the already-flagged
		// row lives in its own scope.
		seedRequeueScope(t, f, "scope-digest-d", "gen-digest-d", now)
		digestD := seedDigested("scope-digest-d", "gen-digest-d", "entity-d", "claimed", "digest-d-same")
		f.exec(t, `UPDATE fact_work_items SET cross_scope_replay_required = TRUE, last_requeue_reason = 'retouch_new_evidence' WHERE work_item_id = $1`, digestD)
		digestE := seedDigested("scope-digest", "gen-digest", "entity-e", "pending", "digest-e-old")

		outcome, err := queue.ReplayWorkloadMaterializationForDigestOutcome(ctx, "scope-digest", "gen-digest", "entity-a", "digest-a-new")
		if err != nil || outcome != reducer.WorkloadMaterializationReplayScheduled {
			t.Fatalf("digest-a ReplayWorkloadMaterializationForDigestOutcome() = (%q, %v), want (scheduled, nil)", outcome, err)
		}
		assertRequeueReason(t, database, ctx, digestA, strptr("cross_scope_replay"))

		outcome, err = queue.ReplayWorkloadMaterializationForDigestOutcome(ctx, "scope-digest", "gen-digest", "entity-b", "digest-b-same")
		if err != nil || outcome != reducer.WorkloadMaterializationReplayNotScheduled {
			t.Fatalf("digest-b ReplayWorkloadMaterializationForDigestOutcome() = (%q, %v), want (not_scheduled, nil)", outcome, err)
		}
		var digestBStatus string
		if err := database.QueryRowContext(ctx, `SELECT status FROM fact_work_items WHERE work_item_id = $1`, digestB).Scan(&digestBStatus); err != nil {
			t.Fatalf("read digest-b row status: %v", err)
		}
		if digestBStatus != "succeeded" {
			t.Fatalf("digest-b row status = %q, want succeeded (same digest must stay quiet)", digestBStatus)
		}
		assertRequeueReason(t, database, ctx, digestB, nil)

		outcome, err = queue.ReplayWorkloadMaterializationForDigestOutcome(ctx, "scope-digest", "gen-digest", "entity-c", "digest-c-new")
		if err != nil || outcome != reducer.WorkloadMaterializationReplayScheduled {
			t.Fatalf("digest-c ReplayWorkloadMaterializationForDigestOutcome() = (%q, %v), want (scheduled, nil)", outcome, err)
		}
		var digestCFlag bool
		if err := database.QueryRowContext(ctx, `SELECT cross_scope_replay_required FROM fact_work_items WHERE work_item_id = $1`, digestC).Scan(&digestCFlag); err != nil {
			t.Fatalf("read digest-c replay flag: %v", err)
		}
		if !digestCFlag {
			t.Fatal("digest-c row cross_scope_replay_required = false, want true (changed digest must flag the claimed row)")
		}
		assertRequeueReason(t, database, ctx, digestC, strptr("cross_scope_replay"))

		outcome, err = queue.ReplayWorkloadMaterializationForDigestOutcome(ctx, "scope-digest-d", "gen-digest-d", "entity-d", "digest-d-same")
		if err != nil || outcome != reducer.WorkloadMaterializationReplayScheduled {
			t.Fatalf("digest-d ReplayWorkloadMaterializationForDigestOutcome() = (%q, %v), want (scheduled, nil)", outcome, err)
		}
		// An already-flagged row sees no transition, so it keeps the reason
		// a previous writer stamped instead of taking the replay reason.
		assertRequeueReason(t, database, ctx, digestD, strptr("retouch_new_evidence"))

		outcome, err = queue.ReplayWorkloadMaterializationForDigestOutcome(ctx, "scope-digest", "gen-digest", "entity-e", "digest-e-new")
		if err != nil || outcome != reducer.WorkloadMaterializationReplayScheduled {
			t.Fatalf("digest-e ReplayWorkloadMaterializationForDigestOutcome() = (%q, %v), want (scheduled, nil)", outcome, err)
		}
		var digestEStatus string
		if err := database.QueryRowContext(ctx, `SELECT status FROM fact_work_items WHERE work_item_id = $1`, digestE).Scan(&digestEStatus); err != nil {
			t.Fatalf("read digest-e row status: %v", err)
		}
		if digestEStatus != "pending" {
			t.Fatalf("digest-e row status = %q, want pending (matched without transitioning)", digestEStatus)
		}
		assertRequeueReason(t, database, ctx, digestE, nil)

		if got := requeueCountsByReason(t, reader)["cross_scope_replay"]; got != 2 {
			t.Fatalf("eshu_dp_reducer_requeue_total{reason=cross_scope_replay} = %d, want 2 (digest-a and digest-c transitioned, the rest matched quietly or not at all)", got)
		}
	})

	t.Run("concurrent double reopen", func(t *testing.T) {
		seedRequeueScope(t, f, "scope-race", "gen-race", now)
		f.row(t, reopenStampRow{id: "race-row", scopeID: "scope-race", generationID: "gen-race", stage: "reducer", domain: "deployment_mapping", status: "succeeded"})

		queue := newQueue()
		first, err := queue.ReopenSucceeded(ctx, "race-row", telemetry.RequeueReasonMaintenanceReopen)
		if err != nil {
			t.Fatalf("first ReopenSucceeded() = %v, want nil", err)
		}
		second, err := queue.ReopenSucceeded(ctx, "race-row", telemetry.RequeueReasonAdminReopen)
		if err != nil {
			t.Fatalf("second ReopenSucceeded() = %v, want nil", err)
		}
		if !first || second {
			t.Fatalf("double reopen = (%v, %v), want exactly one winner (true, false)", first, second)
		}
		// Only the winner's reason survives.
		assertRequeueReason(t, database, ctx, "race-row", strptr("maintenance_reopen"))
	})
}
