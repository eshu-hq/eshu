// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
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
