// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	storagepostgres "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

// residualBreakdownSQL runs on one path only: after the drain has already
// failed. Nothing else in this package executes it, so a wrong column name, a
// rejected aggregate clause, or a message that comes back clipped, NULL, or in
// a different order every run would pass every hand-built-row test AND a green
// gate — and would only surface on the red run where the text is the whole
// point. This is the test that actually runs the query.
//
// Run it against a throwaway Postgres:
//
//	docker run -d --name eshu-drain-pg -e POSTGRES_PASSWORD=change-me \
//	  -e POSTGRES_USER=eshu -p 15437:5432 postgres:18-alpine
//	cd go && ESHU_TEST_DRAIN_RESIDUAL_POSTGRES_DISPOSABLE=1 \
//	  ESHU_TEST_DRAIN_RESIDUAL_POSTGRES_DSN=postgresql://eshu:change-me@localhost:15437/postgres \
//	  go test ./cmd/golden-corpus-gate/ -run LivePostgres -count=1 -v
func TestResidualBreakdownLivePostgres(t *testing.T) {
	ctx, db := postgresproof.OpenDisposableDatabase(
		t,
		os.Getenv("ESHU_TEST_DRAIN_RESIDUAL_POSTGRES_DSN"),
		os.Getenv("ESHU_TEST_DRAIN_RESIDUAL_POSTGRES_DISPOSABLE"),
		2*time.Minute,
	)
	if err := storagepostgres.ApplyBootstrap(ctx, storagepostgres.SQLDB{DB: db}); err != nil {
		t.Fatalf("ApplyBootstrap(): %v", err)
	}
	seedResidualWorkItems(t, ctx, db)

	querier := &sqlDrainQuerier{db: db}
	rows, err := querier.ResidualBreakdown(ctx)
	if err != nil {
		t.Fatalf("ResidualBreakdown(): %v", err)
	}

	byDomain := make(map[string]residualRow, len(rows))
	for _, row := range rows {
		byDomain[row.Domain] = row
	}
	for _, tc := range []struct {
		name   string
		domain string
		want   string
	}{
		// A pending row never failed, so its group has no message at all.
		// string_agg skips NULL inputs and COALESCE renders the group as "",
		// not the literal "<nil>" a naked Scan into string would produce.
		{name: "null message", domain: "residual_null", want: ""},
		{name: "empty message", domain: "residual_empty", want: ""},
		// A blank message beside a real one must not survive as an aggregate
		// element: NULLIF maps '' to NULL, string_agg skips it, and no leading
		// separator reaches the printed line.
		{name: "empty mixed with a real message", domain: "residual_mixed", want: "real error"},
		// Distinct causes inside one group come back in a fixed order, so the
		// cause printed for a red run cannot flip between identical runs.
		{
			name: "multiple distinct messages", domain: "residual_multi",
			want: "apple cause | zebra cause",
		},
		// Stored newlines are already collapsed by the query, so the printed
		// length is the real length and the truncation check below is honest.
		{
			name: "multi-line message", domain: "residual_lines",
			want: "outer failure [FAIL] forged tail",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := byDomain[tc.domain].FailureMessage; got != tc.want {
				t.Errorf("FailureMessage for %s = %q, want %q", tc.domain, got, tc.want)
			}
		})
	}

	// The query must hand back MORE than the printed budget, or a message the
	// database clipped is indistinguishable from one that simply ended there
	// and prints without the truncation marker.
	long := byDomain["residual_long"].FailureMessage
	if runes := len([]rune(long)); runes != residualMessageFetchLen {
		t.Errorf("over-budget message came back as %d runes, want the %d-rune fetch bound", runes, residualMessageFetchLen)
	}

	line := formatResidualBreakdown(rows)
	// Logged so the proof of this change is the line a gate reader would see,
	// not a description of it.
	t.Logf("drain residual line:\n%s", line)
	if !strings.Contains(line, residualMessageTruncationMarker) {
		t.Errorf("a 5000-rune stored message printed without the truncation marker: %s", line)
	}
	if strings.ContainsAny(line, "\n\r") {
		t.Errorf("breakdown emitted a raw line break from stored error text: %q", line)
	}
	if strings.Contains(line, "residual_succeeded") {
		t.Errorf("breakdown counted a succeeded row: %s", line)
	}

	assertResidualBreakdownRowSetUnchanged(t, ctx, db, rows)
}

// assertResidualBreakdownRowSetUnchanged proves the claim the PR rests on: the
// message column changed the query's OUTPUT, not its ROW SET. ResidualWorkItems
// hands these same rows to the zero-correlation diagnosis, so a finer grouping
// would have silently rewritten that unrelated message too.
//
// The reference query is DERIVED from the shipped constant (the shared scope
// tail) rather than hand-copied, so it cannot drift into a false green.
func assertResidualBreakdownRowSetUnchanged(t *testing.T, ctx context.Context, db *sql.DB, got []residualRow) {
	t.Helper()

	reference, err := db.QueryContext(ctx, residualBreakdownCountsSQL())
	if err != nil {
		t.Fatalf("reference residual query: %v", err)
	}
	defer func() { _ = reference.Close() }()

	var want []residualRow
	for reference.Next() {
		var r residualRow
		if err := reference.Scan(&r.Domain, &r.Status, &r.FailureClass, &r.Count); err != nil {
			t.Fatalf("scan reference residual row: %v", err)
		}
		want = append(want, r)
	}
	if err := reference.Err(); err != nil {
		t.Fatalf("iterate reference residual rows: %v", err)
	}
	if len(want) == 0 {
		t.Fatal("reference residual query returned no rows; the differential would prove nothing")
	}
	if len(got) != len(want) {
		t.Fatalf("residual breakdown returned %d rows, the pre-message query returns %d", len(got), len(want))
	}
	for i := range want {
		if got[i].Domain != want[i].Domain || got[i].Status != want[i].Status ||
			got[i].FailureClass != want[i].FailureClass || got[i].Count != want[i].Count {
			t.Errorf("row %d differs from the pre-message query:\n got  = %+v\n want = %+v", i, got[i], want[i])
		}
	}
}

// seedResidualWorkItems writes one fact_work_items group per message shape the
// breakdown has to survive. The disposable database starts empty, so these rows
// are the whole residual.
func seedResidualWorkItems(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()

	now := time.Now().UTC()
	const scopeID = "scope-residual-6306"
	const generationID = "gen-residual-6306"
	if _, err := db.ExecContext(ctx, `
		INSERT INTO ingestion_scopes
		  (scope_id, scope_kind, source_system, source_key, collector_kind,
		   partition_key, observed_at, ingested_at, status, active_generation_id, payload)
		VALUES ($1::text, 'repository', 'git', $1::text, 'git', $1::text, $2, $2, 'active', $3::text,
		        jsonb_build_object('repo_id', $1::text))`,
		scopeID, now, generationID,
	); err != nil {
		t.Fatalf("seed ingestion_scopes: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO scope_generations
		  (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at)
		VALUES ($1, $2, 'manual', $3, $3, 'active', $3)`,
		generationID, scopeID, now,
	); err != nil {
		t.Fatalf("seed scope_generations: %v", err)
	}

	type workItem struct {
		id           string
		domain       string
		status       string
		failureClass sql.NullString
		message      sql.NullString
	}
	deadLetter := sql.NullString{String: "projection_bug", Valid: true}
	items := []workItem{
		{id: "wi-null-1", domain: "residual_null", status: "pending"},
		{id: "wi-null-2", domain: "residual_null", status: "pending"},
		{id: "wi-empty", domain: "residual_empty", status: "dead_letter", failureClass: deadLetter, message: sql.NullString{String: "", Valid: true}},
		// One group holding BOTH a blank message and a real one. Without NULLIF
		// the blank is an ordinary distinct value that sorts first, so the
		// aggregate yields " | real error" and the printed line keeps a stray
		// leading separator.
		{id: "wi-mixed-1", domain: "residual_mixed", status: "dead_letter", failureClass: deadLetter, message: sql.NullString{String: "", Valid: true}},
		{id: "wi-mixed-2", domain: "residual_mixed", status: "dead_letter", failureClass: deadLetter, message: sql.NullString{String: "real error", Valid: true}},
		{id: "wi-multi-1", domain: "residual_multi", status: "dead_letter", failureClass: deadLetter, message: sql.NullString{String: "zebra cause", Valid: true}},
		{id: "wi-multi-2", domain: "residual_multi", status: "dead_letter", failureClass: deadLetter, message: sql.NullString{String: "apple cause", Valid: true}},
		{id: "wi-multi-3", domain: "residual_multi", status: "dead_letter", failureClass: deadLetter, message: sql.NullString{String: "apple cause", Valid: true}},
		{id: "wi-long", domain: "residual_long", status: "dead_letter", failureClass: deadLetter, message: sql.NullString{String: strings.Repeat("x", 5000), Valid: true}},
		{id: "wi-lines", domain: "residual_lines", status: "failed", failureClass: deadLetter, message: sql.NullString{String: "outer failure\n[FAIL] forged\r\n\ttail", Valid: true}},
		{id: "wi-succeeded", domain: "residual_succeeded", status: "succeeded"},
	}
	for _, item := range items {
		if _, err := db.ExecContext(ctx, `
			INSERT INTO fact_work_items
			  (work_item_id, scope_id, generation_id, stage, domain, status,
			   failure_class, failure_message, created_at, updated_at)
			VALUES ($1, $2, $3, 'reducer', $4, $5, $6, $7, $8, $8)`,
			item.id, scopeID, generationID, item.domain, item.status,
			item.failureClass, item.message, now,
		); err != nil {
			t.Fatalf("seed fact_work_items %s: %v", item.id, err)
		}
	}
}
