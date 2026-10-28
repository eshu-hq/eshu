// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"testing"
	"time"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
)

// reopenedAgeFixtureRow is one reducer work item of the #9292 fixture.
type reopenedAgeFixtureRow struct {
	id, domain, status string
	created            time.Duration // offset from statusSemanticsAsOf
	reopened           *time.Duration
}

// seedReopenedBacklogAgeFixture seeds one active scope and reducer rows whose
// created_at and reopened_at disagree, the shape QA showed on 2026-10-27: a
// supply_chain_impact row created about 115 hours ago and reopened minutes
// ago by an OSV pass. It returns the fixture's source system.
func seedReopenedBacklogAgeFixture(ctx context.Context, t *testing.T, conn *sql.Conn) string {
	t.Helper()
	at := func(offset time.Duration) time.Time { return statusSemanticsAsOf.Add(offset) }
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := conn.ExecContext(ctx, query, args...); err != nil {
			t.Fatalf("seed reopened age fixture: %v\n%s", err, query)
		}
	}
	exec(`INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key,
  collector_kind, partition_key, observed_at, ingested_at, status, active_generation_id, payload)
VALUES ('s-reopen', 'repository', 'git', 's-reopen', 'git', 's-reopen', $1, $1, 'active', 'g-reopen', '{}'::jsonb)`, at(-300*time.Hour))
	exec(`INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, payload)
VALUES ('g-reopen', 's-reopen', 'snapshot', $1, $1, 'active', '{}'::jsonb)`, at(-300*time.Hour))
	minutes := func(n int) *time.Duration { d := -time.Duration(n) * time.Minute; return &d }
	rows := []reopenedAgeFixtureRow{
		// Reopened five minutes ago; first created 115 hours ago.
		{id: "w-reopened", domain: "reopened", status: "pending", created: -115 * time.Hour, reopened: minutes(5)},
		// Never reopened: the age is still measured from creation.
		{id: "w-fresh", domain: "fresh", status: "pending", created: -2 * time.Hour},
		// One never-reopened row older (10 h) than a reopened one (1 h since
		// the reopen): the oldest outstanding row is the never-reopened one.
		{id: "w-mixed-old", domain: "mixed", status: "pending", created: -10 * time.Hour},
		{id: "w-mixed-reopened", domain: "mixed", status: "claimed", created: -100 * time.Hour, reopened: minutes(60)},
		// A reopened row that failed an attempt and now retries keeps its
		// reopen as the start of the cycle.
		{id: "w-retrying", domain: "retrying", status: "retrying", created: -50 * time.Hour, reopened: minutes(20)},
		// A reopened row that already finished is not outstanding.
		{id: "w-done", domain: "done", status: "succeeded", created: -200 * time.Hour, reopened: minutes(180)},
	}
	for _, r := range rows {
		var reopened any
		if r.reopened != nil {
			reopened = at(*r.reopened)
		}
		var claimUntil any
		if r.status == "claimed" {
			claimUntil = at(10 * time.Minute)
		}
		exec(`INSERT INTO fact_work_items (work_item_id, scope_id, generation_id, stage, domain,
  status, attempt_count, claim_until, payload, created_at, updated_at, reopened_at)
VALUES ($1, 's-reopen', 'g-reopen', 'reducer', $2, $3, 0, $4, '{}'::jsonb, $5, $5, $6)`,
			r.id, r.domain, r.status, claimUntil, at(r.created), reopened)
	}
	return "git"
}

// reopenedBacklogAgeWant is the age, in seconds before statusSemanticsAsOf,
// each domain's oldest outstanding row must report: the time since the row
// last became outstanding, COALESCE(reopened_at, created_at).
var reopenedBacklogAgeWant = map[string]float64{
	"reopened": 300,
	"fresh":    7200,
	"mixed":    36000,
	"retrying": 1200,
}

// assertBacklogAges requires each fixture domain to report its expected
// oldest outstanding age, and the finished "done" domain to report none.
func assertBacklogAges(t *testing.T, label string, backlogs []statuspkg.DomainBacklog) {
	t.Helper()
	got := map[string]float64{}
	for _, backlog := range backlogs {
		got[backlog.Domain] = backlog.OldestAge.Seconds()
	}
	for domain, want := range reopenedBacklogAgeWant {
		if age, ok := got[domain]; !ok || age != want {
			t.Errorf("%s domain %q oldest age = %vs (present=%v), want %vs", label, domain, age, ok, want)
		}
	}
	if _, ok := got["done"]; ok {
		t.Errorf("%s reports domain %q, which has only a succeeded row", label, "done")
	}
}

// runReopenedBacklogAgeProof is the body of
// TestStatusOldestOutstandingAgeMeasuresFromReopen (#9292). The test lives in
// status_active_work_semantics_live_test.go, which the reducer-contention gate
// enrolls; the fixture and checks live here to keep both files under the line
// cap.
func runReopenedBacklogAgeProof(ctx context.Context, t *testing.T, conn *sql.Conn) {
	t.Helper()
	source := seedReopenedBacklogAgeFixture(ctx, t, conn)
	queryer := sqlConnQueryer{conn: conn}

	summary, err := readActiveWorkSummary(ctx, queryer, statusSemanticsAsOf)
	if err != nil {
		t.Fatalf("readActiveWorkSummary() error = %v", err)
	}
	standalone, err := listDomainBacklogs(ctx, queryer, statusSemanticsAsOf)
	if err != nil {
		t.Fatalf("standalone domain backlog: %v", err)
	}
	assertBacklogAges(t, "summary backlog", summary.DomainBacklogs)
	assertBacklogAges(t, "standalone backlog", standalone)

	const wantQueueAge = 10 * 3600 // the never-reopened "mixed" row
	if got := summary.Queue.OldestOutstandingAge.Seconds(); got != wantQueueAge {
		t.Errorf("summary queue oldest outstanding age = %vs, want %vs", got, float64(wantQueueAge))
	}
	snapshot, err := readQueueSnapshot(ctx, queryer, statusSemanticsAsOf)
	if err != nil {
		t.Fatalf("standalone queue snapshot: %v", err)
	}
	if got := snapshot.OldestOutstandingAge.Seconds(); got != wantQueueAge {
		t.Errorf("standalone queue oldest outstanding age = %vs, want %vs", got, float64(wantQueueAge))
	}

	observer := NewQueueObserverStore(queryer)
	observer.Now = func() time.Time { return statusSemanticsAsOf }
	stageAges, err := observer.QueueOldestAge(ctx)
	if err != nil {
		t.Fatalf("QueueOldestAge() error = %v", err)
	}
	if got := stageAges["reducer"]; got != wantQueueAge {
		t.Errorf("observer reducer oldest age = %vs, want %vs", got, float64(wantQueueAge))
	}
	sourceAges, err := observer.SourceQueueOldestAge(ctx)
	if err != nil {
		t.Fatalf("SourceQueueOldestAge() error = %v", err)
	}
	if got := sourceAges["reducer"][source]; got != wantQueueAge {
		t.Errorf("observer reducer/%s oldest age = %vs, want %vs", source, got, float64(wantQueueAge))
	}
}
