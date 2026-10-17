// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"log"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// unchangedListingRows is the (work_item_id, unchanged) shape the
// deployable_unit_correlation reopen listing returns: every succeeded item
// above the replay floor, flagged unchanged when its recorded input
// fingerprint equals the live one (#8283).
func unchangedListingRows(items ...struct {
	id        string
	unchanged bool
},
) queueFakeRows {
	rows := make([][]any, 0, len(items))
	for _, item := range items {
		rows = append(rows, []any{item.id, item.unchanged})
	}
	return queueFakeRows{rows: rows}
}

type unchangedItem = struct {
	id        string
	unchanged bool
}

// TestReopenDeployableUnitCorrelationSkipsUnchangedItems pins the behavior of
// the reopen loop for the one domain that carries an input fingerprint: an
// item whose recorded fingerprint still equals the live one stays succeeded
// (no reopen UPDATE is issued for it), a changed item and an item with no
// recorded row reopen, and the block reports the skipped count.
func TestReopenDeployableUnitCorrelationSkipsUnchangedItems(t *testing.T) {
	t.Parallel()

	recorder, tracer := recordingTracer()
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{unchangedListingRows(
			unchangedItem{"wi-unchanged", true},
			unchangedItem{"wi-changed", false},
			unchangedItem{"wi-no-row", false},
		)},
		execResults: []sql.Result{rowsAffectedResult{rowsAffected: 1}, rowsAffectedResult{rowsAffected: 1}},
	}
	store := IngestionStore{database: db, Now: func() time.Time { return time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC) }}

	if err := store.ReopenSucceededReducerWorkItems(
		context.Background(), tracer, nil, []string{"deployable_unit_correlation"},
	); err != nil {
		t.Fatalf("ReopenSucceededReducerWorkItems() error = %v, want nil", err)
	}

	var reopened []string
	for _, exec := range db.execs {
		reopened = append(reopened, exec.args[1].(string))
	}
	if got, want := strings.Join(reopened, ","), "wi-changed,wi-no-row"; got != want {
		t.Fatalf("reopened work items = %q, want %q (the unchanged item must stay succeeded)", got, want)
	}
	span := singleReopenSpan(t, recorder)
	if span.domain != "deployable_unit_correlation" ||
		span.ints["reopened"] != 2 || span.ints["skipped_unchanged"] != 1 || span.ints["skipped_by_memo"] != 0 {
		t.Fatalf("reopen span = %+v, want reopened=2 skipped_unchanged=1 skipped_by_memo=0", span)
	}
}

// TestReopenBlanketDomainsNeverSkipUnchanged pins that the other three
// correlation domains keep their blanket reopen: their listing has no
// fingerprint column, every listed item reopens, and skipped_unchanged is 0.
func TestReopenBlanketDomainsNeverSkipUnchanged(t *testing.T) {
	t.Parallel()

	for _, domain := range []string{
		"kubernetes_correlation_materialization",
		"container_image_identity",
		"aws_cloud_runtime_drift",
	} {
		t.Run(domain, func(t *testing.T) {
			t.Parallel()
			recorder, tracer := recordingTracer()
			db := &fakeExecQueryer{
				queryResponses: []queueFakeRows{{rows: [][]any{{"wi-1"}, {"wi-2"}}}},
				execResults:    []sql.Result{rowsAffectedResult{rowsAffected: 1}, rowsAffectedResult{rowsAffected: 1}},
			}
			store := IngestionStore{database: db, Now: time.Now}
			if err := store.ReopenSucceededReducerWorkItems(context.Background(), tracer, nil, []string{domain}); err != nil {
				t.Fatalf("reopen error = %v, want nil", err)
			}
			if got, want := len(db.execs), 2; got != want {
				t.Fatalf("reopen exec count = %d, want %d (blanket domain must reopen every listed item)", got, want)
			}
			span := singleReopenSpan(t, recorder)
			if span.ints["reopened"] != 2 || span.ints["skipped_unchanged"] != 0 {
				t.Fatalf("reopen span = %+v, want reopened=2 skipped_unchanged=0", span)
			}
			if got := db.queries[0].query; got != listSucceededReducerWorkItemsByDomainQuery {
				t.Fatalf("domain %s listing is not the blanket listing", domain)
			}
		})
	}
}

// TestReopenDeployableUnitCorrelationListingErrorIsNotASilentSkip is the error
// path of the gate: when the fingerprint listing fails the reopen returns the
// error with the domain named and issues no reopen UPDATE, so a broken
// listing can never read as "everything unchanged". The maintenance pass runs
// this inside the one reopen transaction, so the error also rolls back the
// deployment_mapping and code_import_repo_edge reopens of that pass.
func TestReopenDeployableUnitCorrelationListingErrorIsNotASilentSkip(t *testing.T) {
	var logs bytes.Buffer
	original := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(original) })

	db := &fakeExecQueryer{queryResponses: []queueFakeRows{{err: errors.New("relation \"reducer_work_input_fingerprints\" does not exist")}}}
	store := IngestionStore{database: db, Now: time.Now}

	err := store.ReopenSucceededReducerWorkItems(context.Background(), nil, nil, []string{"deployable_unit_correlation"})
	if err == nil {
		t.Fatal("ReopenSucceededReducerWorkItems() error = nil, want the listing failure")
	}
	if !strings.Contains(err.Error(), "list succeeded deployable_unit_correlation work items") {
		t.Fatalf("error = %v, want listing context naming the domain", err)
	}
	if len(db.execs) != 0 {
		t.Fatalf("reopen execs after a failed listing = %d, want 0", len(db.execs))
	}
	if strings.Contains(logs.String(), "reducer_work_items_reopened") {
		t.Fatalf("a failed listing logged a reopened line: %s", logs.String())
	}
}

// TestReopenCompletionLogCarriesSkippedUnchanged pins the operator-facing log
// fields: the structured reopened line names skipped_unchanged so an operator
// can tell a skipped wave from an empty one.
func TestReopenCompletionLogCarriesSkippedUnchanged(t *testing.T) {
	var logs bytes.Buffer
	original := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(original) })

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{unchangedListingRows(
			unchangedItem{"wi-a", true}, unchangedItem{"wi-b", true}, unchangedItem{"wi-c", false},
		)},
		execResults: []sql.Result{rowsAffectedResult{rowsAffected: 1}},
	}
	store := IngestionStore{database: db, Now: time.Now}
	if err := store.ReopenSucceededReducerWorkItems(
		context.Background(), nil, nil, []string{"deployable_unit_correlation"},
	); err != nil {
		t.Fatalf("reopen error = %v, want nil", err)
	}
	want := "reducer_work_items_reopened domain=deployable_unit_correlation count=1 skipped_unchanged=2"
	if !strings.Contains(logs.String(), want) {
		t.Fatalf("log = %q, want a line containing %q", logs.String(), want)
	}
}

// TestReopenSkippedUnchangedCounterCountsPerDomain pins the per-domain metric
// an operator reads to see how much of a wave the fingerprint gate removed: it
// counts the skipped items for the gated domain, next to the unchanged
// eshu_dp_correlation_reopened_total.
func TestReopenSkippedUnchangedCounterCountsPerDomain(t *testing.T) {
	t.Parallel()

	instruments, reader := newDeriveMeter(t)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{unchangedListingRows(
			unchangedItem{"wi-a", true}, unchangedItem{"wi-b", true}, unchangedItem{"wi-c", false},
		)},
		execResults: []sql.Result{rowsAffectedResult{rowsAffected: 1}},
	}
	store := IngestionStore{database: db, Now: time.Now}
	if err := store.ReopenSucceededReducerWorkItems(
		context.Background(), nil, instruments, []string{"deployable_unit_correlation"},
	); err != nil {
		t.Fatalf("reopen error = %v, want nil", err)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect metrics: %v", err)
	}
	got := map[string]int64{}
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				continue
			}
			for _, point := range sum.DataPoints {
				domain, _ := point.Attributes.Value("domain")
				got[m.Name+"/"+domain.AsString()] += point.Value
			}
		}
	}
	if v := got["eshu_dp_correlation_reopen_skipped_unchanged_total/deployable_unit_correlation"]; v != 2 {
		t.Fatalf("skipped_unchanged counter = %d, want 2; all = %v", v, got)
	}
	if v := got["eshu_dp_correlation_reopened_total/deployable_unit_correlation"]; v != 1 {
		t.Fatalf("reopened counter = %d, want 1 (only the changed item); all = %v", v, got)
	}
}
