// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	summarystore "github.com/eshu-hq/eshu/go/internal/storage/postgres/status/summary"
)

// TestActiveWorkSummaryProjectsWideFieldsOnlyForConsumers guards the measured
// #7009 spill fix on the SQL actually used by the status snapshot.
func TestActiveWorkSummaryProjectsWideFieldsOnlyForConsumers(t *testing.T) {
	t.Parallel()

	if err := checkActiveWorkSummaryProjection(activeWorkSummaryQuery); err != nil {
		t.Fatal(err)
	}
	original := "SELECT " + activeWorkSummaryColumns + "\n  " + activeWorkSummaryWorkInput
	widened := strings.Replace(activeWorkSummaryQuery, original,
		"SELECT work.*\n  "+activeWorkSummaryWorkInput, 1)
	if widened == activeWorkSummaryQuery {
		t.Fatal("wildcard positive control did not change the production CTE")
	}
	if err := checkActiveWorkSummaryProjection(widened); err == nil {
		t.Fatal("wildcard positive control passed the projection guard")
	}
	appended := strings.Replace(activeWorkSummaryQuery, original,
		"SELECT "+activeWorkSummaryColumns+",\n         *\n  "+activeWorkSummaryWorkInput, 1)
	if appended == activeWorkSummaryQuery {
		t.Fatal("trailing wildcard positive control did not change the production CTE")
	}
	if err := checkActiveWorkSummaryProjection(appended); err == nil {
		t.Fatal("trailing wildcard positive control passed the projection guard")
	}
}

// TestActiveWorkSummaryDecodesStoredSummaryRowsUnchanged proves the status
// summary read model stores rows the production decoder reads unchanged (#7009):
// section rows encoded into the stored payload, decoded again, and fed in order
// to activeWorkSummary.add produce the same summary as feeding the source rows
// directly. Reversing the stored order changes the summary, so the comparison
// can fail. The live comparison with readActiveWorkSummary is the reader slice's.
func TestActiveWorkSummaryDecodesStoredSummaryRowsUnchanged(t *testing.T) {
	t.Parallel()

	source := []summarystore.Entry{
		{Section: activeWorkSectionStage, Ordinal: 0, JSON: `{"stage":"reducer","status":"pending","count":4}`},
		{Section: activeWorkSectionStage, Ordinal: 1, JSON: `{"stage":"projector","status":"claimed","count":2}`},
		{Section: activeWorkSectionBacklog, Ordinal: 0, JSON: `{"domain":"workload","outstanding_count":5,"in_flight_count":1,"retrying_count":0,"dead_letter_count":0,"failed_count":0,"oldest_outstanding_age_seconds":12.5}`},
		{Section: activeWorkSectionQueue, Ordinal: 0, JSON: `{"total_count":9,"outstanding_count":6,"pending_count":4,"in_flight_count":2,"retrying_count":0,"succeeded_count":3,"dead_letter_count":0,"failed_count":0,"provenance_edge_identity_upgrade_applied":true,"provenance_edge_identity_upgrade_required":0,"oldest_outstanding_age_seconds":12.5,"overdue_claim_count":1}`},
		{Section: activeWorkSectionBlockage, Ordinal: 0, JSON: `{"stage":"reducer","domain":"workload","conflict_domain":"repo","conflict_key":"r1","blocked_count":3,"oldest_blocked_age_seconds":7}`},
		{Section: activeWorkSectionFailure, Ordinal: 0, JSON: `{"stage":"reducer","domain":"workload","status":"dead_letter","work_item_id":"w-1","scope_id":"s-1","generation_id":"g-1","failure_class":"boom","failure_message":"m","failure_details":"d","updated_at":"2026-10-06T12:00:00Z"}`},
	}
	build := func(entries []summarystore.Entry) activeWorkSummary {
		built := activeWorkSummary{
			StageCounts:    []statuspkg.StageStatusCount{},
			DomainBacklogs: []statuspkg.DomainBacklog{},
			Blockages:      []statuspkg.QueueBlockage{},
		}
		for _, entry := range entries {
			if err := built.add(entry.Section, entry.JSON); err != nil {
				t.Fatalf("add(%s): %v", entry.Section, err)
			}
		}
		return built
	}
	want := build(source)
	if len(want.StageCounts) != 2 || want.Queue.Total != 9 || want.LatestFailure == nil || len(want.Blockages) != 1 {
		t.Fatalf("source summary is not populated: %+v", want)
	}

	payload, err := summarystore.EncodeEntries(source)
	if err != nil {
		t.Fatalf("EncodeEntries(): %v", err)
	}
	stored, err := summarystore.DecodeEntries(payload)
	if err != nil {
		t.Fatalf("DecodeEntries(): %v", err)
	}
	if got := build(stored); !reflect.DeepEqual(got, want) {
		t.Fatalf("stored summary = %+v, want %+v", got, want)
	}

	reversed := append([]summarystore.Entry(nil), stored...)
	for i, j := 0, len(reversed)-1; i < j; i, j = i+1, j-1 {
		reversed[i], reversed[j] = reversed[j], reversed[i]
	}
	if got := build(reversed); reflect.DeepEqual(got, want) {
		t.Fatal("reversing the stored order did not change the summary: the comparison cannot fail")
	}
}

func checkActiveWorkSummaryProjection(query string) error {
	_, rest, ok := strings.Cut(query, "active_fact_work_items AS MATERIALIZED (\n")
	if !ok {
		return fmt.Errorf("summary has no materialized active work CTE")
	}
	cte, _, ok := strings.Cut(rest, "\n),\n")
	if !ok {
		return fmt.Errorf("summary has no materialized active work CTE boundary")
	}
	projection, _, ok := strings.Cut(cte, "\n  "+activeWorkSummaryWorkInput)
	if !ok {
		return fmt.Errorf("summary has no active work projection boundary")
	}
	if regexp.MustCompile(`(?:SELECT|,)\s*(?:[A-Za-z_][A-Za-z_0-9]*\.)?\*`).MatchString(projection) {
		return fmt.Errorf("summary materializes a wildcard projection")
	}
	required := map[string]string{
		"work_item_id":    `CASE WHEN work.status IN ('pending', 'claimed', 'running', 'retrying', 'failed', 'dead_letter') THEN work.work_item_id END AS work_item_id`,
		"scope_id":        `CASE WHEN work.status IN ('pending', 'claimed', 'running', 'retrying', 'failed', 'dead_letter') THEN work.scope_id END AS scope_id`,
		"generation_id":   `CASE WHEN work.status IN ('pending', 'claimed', 'running', 'retrying', 'failed', 'dead_letter') THEN work.generation_id END AS generation_id`,
		"domain":          `CASE WHEN work.status IN ('pending', 'claimed', 'running', 'retrying', 'failed', 'dead_letter') THEN work.domain END AS domain`,
		"conflict_domain": `CASE WHEN work.stage = 'reducer' AND work.status IN ('pending', 'claimed', 'running', 'retrying') THEN work.conflict_domain END AS conflict_domain`,
		"conflict_key":    `CASE WHEN work.stage = 'reducer' AND work.status IN ('pending', 'claimed', 'running', 'retrying') THEN work.conflict_key END AS conflict_key`,
		"payload": `CASE WHEN work.stage = 'reducer'
                   AND work.status IN ('pending', 'retrying', 'claimed', 'running')
              THEN work.payload END AS payload`,
		"failure_class": `CASE WHEN work.status IN ('retrying', 'failed', 'dead_letter')
              THEN work.failure_class END AS failure_class`,
		"failure_message": `CASE WHEN work.status IN ('retrying', 'failed', 'dead_letter')
              THEN work.failure_message END AS failure_message`,
		"failure_details": `CASE WHEN work.status IN ('retrying', 'failed', 'dead_letter')
              THEN work.failure_details END AS failure_details`,
	}
	for column, clause := range required {
		if strings.Count(projection, "work."+column) != 1 || !strings.Contains(projection, clause) {
			return fmt.Errorf("summary does not conditionally project %s", column)
		}
	}
	return nil
}

// statusSemanticsRows renders every row as pipe-joined text so the expected
// rows stay readable; numeric ages are rounded to whole seconds.
func statusSemanticsRows(ctx context.Context, t *testing.T, conn *sql.Conn, query string, args ...any) []string {
	t.Helper()
	rows, err := conn.QueryContext(ctx, query, args...)
	if err != nil {
		t.Fatalf("query: %v\n%s", err, query)
	}
	defer func() { _ = rows.Close() }()
	cols, err := rows.Columns()
	if err != nil {
		t.Fatalf("columns: %v", err)
	}
	var out []string
	for rows.Next() {
		values := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			t.Fatalf("scan: %v", err)
		}
		fields := make([]string, len(values))
		for i, value := range values {
			switch typed := value.(type) {
			case float64:
				fields[i] = fmt.Sprintf("%.0f", typed)
			case string:
				// pgx renders numeric EXTRACT(EPOCH ...) results as text.
				fields[i] = typed
				if parsed, parseErr := strconv.ParseFloat(typed, 64); parseErr == nil && strings.Contains(typed, ".") {
					fields[i] = fmt.Sprintf("%.0f", parsed)
				}
			case time.Time:
				fields[i] = typed.UTC().Format(time.RFC3339)
			default:
				fields[i] = fmt.Sprint(typed)
			}
		}
		out = append(out, strings.Join(fields, "|"))
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	return out
}

// statusSemanticsWorkItemIDs keeps the work_item_id field (the first
// "w-"-prefixed field) of each rendered row.
func statusSemanticsWorkItemIDs(rows []string) []string {
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		for _, field := range strings.Split(row, "|") {
			if strings.HasPrefix(field, "w-") {
				ids = append(ids, field)
				break
			}
		}
	}
	return ids
}

// TestReadActiveWorkSummaryEntriesKeepsStatementRows proves the read model
// writer's pass (#7009) runs the live active-work statement byte for byte at
// the given asOf and returns every row as an entry, in statement order, with
// the section JSON text untouched.
func TestReadActiveWorkSummaryEntriesKeepsStatementRows(t *testing.T) {
	t.Parallel()

	queueJSON := `{"total_count":3,"outstanding_count":2,"pending_count":2,"in_flight_count":0,"retrying_count":0,"succeeded_count":1,"dead_letter_count":0,"failed_count":0,"provenance_edge_identity_upgrade_applied":false,"provenance_edge_identity_upgrade_required":0,"oldest_outstanding_age_seconds":12.5,"overdue_claim_count":0}`
	queryer := &recordingSummaryQueryer{rows: [][]any{
		{"backlog", int64(1), `{"domain":"d1","outstanding_count":2,"in_flight_count":0,"retrying_count":0,"dead_letter_count":0,"failed_count":0,"oldest_outstanding_age_seconds":12.5}`},
		{"queue", int64(1), queueJSON},
		{"stage", int64(1), `{"stage":"reducer","status":"pending","count":2}`},
		{"stage", int64(2), `{"stage":"reducer","status":"succeeded","count":1}`},
	}}
	asOf := time.Date(2026, 10, 6, 12, 0, 0, 0, time.FixedZone("EDT", -4*60*60))

	entries, err := ReadActiveWorkSummaryEntries(context.Background(), queryer, asOf)
	if err != nil {
		t.Fatalf("ReadActiveWorkSummaryEntries() error = %v", err)
	}
	if queryer.query != activeWorkSummaryQuery {
		t.Fatal("ReadActiveWorkSummaryEntries() did not run activeWorkSummaryQuery byte for byte")
	}
	if len(queryer.args) != 1 {
		t.Fatalf("statement args = %#v, want one asOf argument", queryer.args)
	}
	if bound, ok := queryer.args[0].(time.Time); !ok || !bound.Equal(asOf) || bound.Location() != time.UTC {
		t.Fatalf("statement $1 = %#v, want %v in UTC", queryer.args[0], asOf.UTC())
	}
	if len(entries) != len(queryer.rows) {
		t.Fatalf("entries = %d, want %d", len(entries), len(queryer.rows))
	}
	for i, row := range queryer.rows {
		got := entries[i]
		if got.Section != row[0] || got.Ordinal != row[1] || got.JSON != row[2] {
			t.Fatalf("entry %d = %#v, want %v", i, got, row)
		}
	}
}

// TestReadActiveWorkSummaryEntriesRejectsUndecodableRows proves a row the
// live decoder would reject is never handed to the writer, so the model can
// never store an answer the reader cannot decode.
func TestReadActiveWorkSummaryEntriesRejectsUndecodableRows(t *testing.T) {
	t.Parallel()

	for name, row := range map[string][]any{
		"invalid json":    {"stage", int64(1), `{not json`},
		"unknown section": {"surprise", int64(1), `{}`},
		"bad count":       {"stage", int64(1), `{"stage":"reducer","status":"pending","count":"many"}`},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			queryer := &recordingSummaryQueryer{rows: [][]any{row}}
			entries, err := ReadActiveWorkSummaryEntries(context.Background(), queryer, time.Now())
			if err == nil {
				t.Fatalf("ReadActiveWorkSummaryEntries() = %#v, nil; want a decode error", entries)
			}
		})
	}
}

// TestReadActiveWorkSummaryEntriesEmptyResult proves an empty statement
// result is an empty, non-nil entry list, which encodes as [] and not null.
func TestReadActiveWorkSummaryEntriesEmptyResult(t *testing.T) {
	t.Parallel()

	entries, err := ReadActiveWorkSummaryEntries(context.Background(), &recordingSummaryQueryer{}, time.Now())
	if err != nil {
		t.Fatalf("ReadActiveWorkSummaryEntries() error = %v", err)
	}
	if entries == nil || len(entries) != 0 {
		t.Fatalf("entries = %#v, want an empty non-nil slice", entries)
	}
}

// TestActiveWorkSummarySourceSHA256PinsStatementText proves the rolling
// upgrade fence digests the exact statement text, so any change to the
// statement changes the digest the reader compares.
func TestActiveWorkSummarySourceSHA256PinsStatementText(t *testing.T) {
	t.Parallel()

	sum := sha256.Sum256([]byte(activeWorkSummaryQuery))
	if got, want := ActiveWorkSummarySourceSHA256(), hex.EncodeToString(sum[:]); got != want {
		t.Fatalf("ActiveWorkSummarySourceSHA256() = %q, want %q", got, want)
	}
	other := sha256.Sum256([]byte(activeWorkSummaryQuery + " "))
	if ActiveWorkSummarySourceSHA256() == hex.EncodeToString(other[:]) {
		t.Fatal("digest does not change with the statement text")
	}
}

// recordingSummaryQueryer answers any query with rows and records the last
// statement text and arguments.
type recordingSummaryQueryer struct {
	rows  [][]any
	query string
	args  []any
}

func (q *recordingSummaryQueryer) QueryContext(_ context.Context, query string, args ...any) (db.Rows, error) {
	q.query = query
	q.args = args
	return &fakeRows{rows: q.rows}, nil
}

// TestAgeAdvanceReachesEveryDecodedDuration proves the summary package's age
// key table names the keys this package's decoder reads as durations: the
// aged entries, decoded by the production decoder, differ from the unaged
// decode by exactly the age in the three duration fields and nowhere else.
func TestAgeAdvanceReachesEveryDecodedDuration(t *testing.T) {
	t.Parallel()

	decode := func(entries []summarystore.Entry) activeWorkSummary {
		var out activeWorkSummary
		for _, entry := range entries {
			if err := out.add(entry.Section, entry.JSON); err != nil {
				t.Fatal(err)
			}
		}
		return out
	}
	base := ageGuardEntries()
	aged, err := summarystore.AddAge(base, 20*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	before, after := decode(base), decode(aged)
	if got := after.Queue.OldestOutstandingAge - before.Queue.OldestOutstandingAge; got != 20*time.Second {
		t.Fatalf("queue age moved %v, want 20s", got)
	}
	if got := after.DomainBacklogs[0].OldestAge - before.DomainBacklogs[0].OldestAge; got != 20*time.Second {
		t.Fatalf("backlog age moved %v, want 20s", got)
	}
	if got := after.Blockages[0].OldestAge - before.Blockages[0].OldestAge; got != 20*time.Second {
		t.Fatalf("blockage age moved %v, want 20s", got)
	}
	after.Queue.OldestOutstandingAge = before.Queue.OldestOutstandingAge
	after.DomainBacklogs[0].OldestAge = before.DomainBacklogs[0].OldestAge
	after.Blockages[0].OldestAge = before.Blockages[0].OldestAge
	b1, _ := json.Marshal(before)
	b2, _ := json.Marshal(after)
	if string(b1) != string(b2) {
		t.Fatalf("aging changed something other than the three durations:\n%s\n%s", b1, b2)
	}
}

// ageGuardEntries is one realistic row of each section that carries a duration.
func ageGuardEntries() []summarystore.Entry {
	return []summarystore.Entry{
		{Section: "backlog", Ordinal: 1, JSON: `{"domain":"repo_dependency","outstanding_count":2,"in_flight_count":0,"retrying_count":0,"dead_letter_count":0,"failed_count":0,"oldest_outstanding_age_seconds":7.5}`},
		{Section: "blockage", Ordinal: 1, JSON: `{"stage":"reducer","domain":"d","conflict_domain":"c","conflict_key":"k","blocked_count":1,"oldest_blocked_age_seconds":3}`},
		{Section: "queue", Ordinal: 1, JSON: `{"total_count":4,"outstanding_count":2,"pending_count":2,"in_flight_count":0,"retrying_count":0,"succeeded_count":2,"dead_letter_count":0,"failed_count":0,"provenance_edge_identity_upgrade_applied":false,"provenance_edge_identity_upgrade_required":0,"oldest_outstanding_age_seconds":5,"overdue_claim_count":0}`},
		{Section: "stage", Ordinal: 1, JSON: `{"stage":"reducer","status":"pending","count":2}`},
		{Section: "mode", Ordinal: 1, JSON: `{"mode":"grouped","estimate":0.25}`},
	}
}
