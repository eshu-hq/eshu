// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
)

func lifecycleRow(
	scopeID, generationID, scopeKind, sourceSystem, collectorKind, activeGen string,
	isActive bool,
	trigger, freshnessHint, generationStatus string,
	observed, ingested, activated, superseded any,
	total, outstanding, inflight, retrying, succeeded, failed, deadLetter int64,
	failureClass, failureMessage, failureWorkItemStatus string,
	failureObserved any,
) []any {
	return []any{
		scopeID, generationID, scopeKind, sourceSystem, collectorKind, activeGen,
		isActive, trigger, freshnessHint, generationStatus,
		observed, ingested, activated, superseded,
		total, outstanding, inflight, retrying, succeeded, failed, deadLetter,
		failureClass, failureMessage, failureWorkItemStatus, failureObserved,
	}
}

func TestListGenerationLifecycleActiveRecord(t *testing.T) {
	t.Parallel()

	observed := time.Date(2026, 6, 9, 10, 0, 0, 0, time.UTC)
	ingested := observed.Add(time.Minute)
	activated := observed.Add(2 * time.Minute)
	queryer := &fakeQueryer{responses: []fakeRows{{rows: [][]any{
		lifecycleRow(
			"git-repository-scope:acme/app", "gen-active", "repository", "github", "git", "gen-active",
			true, "snapshot", "head_sha", "active",
			observed, ingested, activated, nil,
			3, 1, 1, 0, 1, 0, 0,
			"", "", "", nil,
		),
	}}}}
	store := NewStatusStore(queryer)

	page, err := store.ListGenerationLifecycle(context.Background(), statuspkg.GenerationLifecycleFilter{
		ScopeID: "git-repository-scope:acme/app",
		Limit:   50,
	})
	if err != nil {
		t.Fatalf("ListGenerationLifecycle() error = %v", err)
	}
	if len(page.Records) != 1 {
		t.Fatalf("records = %d, want 1", len(page.Records))
	}
	rec := page.Records[0]
	if !rec.IsActive {
		t.Fatalf("IsActive = false, want true")
	}
	if rec.Status != "active" || rec.CurrentActiveGenerationID != "gen-active" {
		t.Fatalf("unexpected record: %+v", rec)
	}
	if rec.ObservedAt != "2026-06-09T10:00:00Z" || rec.ActivatedAt != "2026-06-09T10:02:00Z" {
		t.Fatalf("timestamps wrong: %+v", rec)
	}
	if rec.SupersededAt != "" {
		t.Fatalf("SupersededAt = %q, want empty", rec.SupersededAt)
	}
	if rec.QueueStatus.Total != 3 || rec.QueueStatus.Succeeded != 1 || rec.QueueStatus.InFlight != 1 {
		t.Fatalf("queue rollup wrong: %+v", rec.QueueStatus)
	}
	if rec.LatestFailure != nil {
		t.Fatalf("LatestFailure = %+v, want nil", rec.LatestFailure)
	}
	if page.Truncated {
		t.Fatalf("Truncated = true, want false")
	}
}

func TestListGenerationLifecyclePendingBuildingRecord(t *testing.T) {
	t.Parallel()

	observed := time.Date(2026, 6, 9, 11, 0, 0, 0, time.UTC)
	queryer := &fakeQueryer{responses: []fakeRows{{rows: [][]any{
		lifecycleRow(
			"git-repository-scope:acme/app", "gen-pending", "repository", "github", "git", "",
			false, "snapshot", "head_sha", "pending",
			observed, observed, nil, nil,
			2, 2, 0, 0, 0, 0, 0,
			"", "", "", nil,
		),
	}}}}
	store := NewStatusStore(queryer)

	page, err := store.ListGenerationLifecycle(context.Background(), statuspkg.GenerationLifecycleFilter{
		Repository: "acme/app",
		Limit:      50,
	})
	if err != nil {
		t.Fatalf("ListGenerationLifecycle() error = %v", err)
	}
	rec := page.Records[0]
	if rec.Status != "pending" || rec.IsActive {
		t.Fatalf("unexpected pending record: %+v", rec)
	}
	if rec.QueueStatus.Outstanding != 2 {
		t.Fatalf("Outstanding = %d, want 2", rec.QueueStatus.Outstanding)
	}
}

func TestListGenerationLifecycleSupersededRecord(t *testing.T) {
	t.Parallel()

	observed := time.Date(2026, 6, 9, 9, 0, 0, 0, time.UTC)
	superseded := observed.Add(time.Hour)
	queryer := &fakeQueryer{responses: []fakeRows{{rows: [][]any{
		lifecycleRow(
			"git-repository-scope:acme/app", "gen-old", "repository", "github", "git", "gen-new",
			false, "snapshot", "head_sha", "superseded",
			observed, observed, observed, superseded,
			1, 0, 0, 0, 1, 0, 0,
			"", "", "", nil,
		),
	}}}}
	store := NewStatusStore(queryer)

	page, err := store.ListGenerationLifecycle(context.Background(), statuspkg.GenerationLifecycleFilter{
		GenerationID: "gen-old",
		Limit:        50,
	})
	if err != nil {
		t.Fatalf("ListGenerationLifecycle() error = %v", err)
	}
	rec := page.Records[0]
	if rec.Status != "superseded" || rec.SupersededAt != "2026-06-09T10:00:00Z" {
		t.Fatalf("unexpected superseded record: %+v", rec)
	}
	if rec.CurrentActiveGenerationID != "gen-new" || rec.IsActive {
		t.Fatalf("superseded should not be active and points to gen-new: %+v", rec)
	}
}

func TestListGenerationLifecycleFailedRecordCarriesLatestFailure(t *testing.T) {
	t.Parallel()

	observed := time.Date(2026, 6, 9, 8, 0, 0, 0, time.UTC)
	failureObserved := observed.Add(5 * time.Minute)
	queryer := &fakeQueryer{responses: []fakeRows{{rows: [][]any{
		lifecycleRow(
			"git-repository-scope:acme/app", "gen-failed", "repository", "github", "git", "",
			false, "snapshot", "head_sha", "failed",
			observed, observed, nil, nil,
			1, 0, 0, 0, 0, 1, 0,
			"parser_panic", "panic decoding tree", "failed",
			failureObserved,
		),
	}}}}
	store := NewStatusStore(queryer)

	page, err := store.ListGenerationLifecycle(context.Background(), statuspkg.GenerationLifecycleFilter{
		ScopeID: "git-repository-scope:acme/app",
		Status:  "failed",
		Limit:   50,
	})
	if err != nil {
		t.Fatalf("ListGenerationLifecycle() error = %v", err)
	}
	rec := page.Records[0]
	if rec.LatestFailure == nil {
		t.Fatalf("LatestFailure = nil, want failure")
	}
	if rec.LatestFailure.FailureClass != "parser_panic" ||
		rec.LatestFailure.FailureMessage != "panic decoding tree" ||
		rec.LatestFailure.WorkItemStatus != "failed" ||
		rec.LatestFailure.ObservedAt != "2026-06-09T08:05:00Z" {
		t.Fatalf("unexpected failure: %+v", rec.LatestFailure)
	}
	if rec.QueueStatus.Failed != 1 {
		t.Fatalf("Failed = %d, want 1", rec.QueueStatus.Failed)
	}
}

func TestListGenerationLifecycleTruncatesAtLimit(t *testing.T) {
	t.Parallel()

	observed := time.Date(2026, 6, 9, 7, 0, 0, 0, time.UTC)
	rows := make([][]any, 0, 3)
	for i := 0; i < 3; i++ {
		rows = append(rows, lifecycleRow(
			"git-repository-scope:acme/app", "gen-"+string(rune('a'+i)), "repository", "github", "git", "",
			false, "snapshot", "", "completed",
			observed, observed, nil, nil,
			0, 0, 0, 0, 0, 0, 0,
			"", "", "", nil,
		))
	}
	queryer := &fakeQueryer{responses: []fakeRows{{rows: rows}}}
	store := NewStatusStore(queryer)

	// limit 2 means the reader fetched limit+1 = 3 rows and must report
	// truncation while trimming the page back to 2.
	page, err := store.ListGenerationLifecycle(context.Background(), statuspkg.GenerationLifecycleFilter{
		CollectorKind: "git",
		Limit:         2,
	})
	if err != nil {
		t.Fatalf("ListGenerationLifecycle() error = %v", err)
	}
	if len(page.Records) != 2 {
		t.Fatalf("records = %d, want 2", len(page.Records))
	}
	if !page.Truncated {
		t.Fatalf("Truncated = false, want true")
	}
}

func TestListGenerationLifecycleSkippedRefreshHasNoOutstandingQueue(t *testing.T) {
	t.Parallel()

	// An unchanged/skipped refresh keeps the prior generation active and emits
	// no new work, so the active generation shows zero outstanding queue rows.
	observed := time.Date(2026, 6, 9, 6, 0, 0, 0, time.UTC)
	queryer := &fakeQueryer{responses: []fakeRows{{rows: [][]any{
		lifecycleRow(
			"git-repository-scope:acme/app", "gen-active", "repository", "github", "git", "gen-active",
			true, "snapshot", "unchanged", "completed",
			observed, observed, observed, nil,
			4, 0, 0, 0, 4, 0, 0,
			"", "", "", nil,
		),
	}}}}
	store := NewStatusStore(queryer)

	page, err := store.ListGenerationLifecycle(context.Background(), statuspkg.GenerationLifecycleFilter{
		ScopeID: "git-repository-scope:acme/app",
		Limit:   50,
	})
	if err != nil {
		t.Fatalf("ListGenerationLifecycle() error = %v", err)
	}
	rec := page.Records[0]
	if rec.FreshnessHint != "unchanged" {
		t.Fatalf("FreshnessHint = %q, want unchanged", rec.FreshnessHint)
	}
	if rec.QueueStatus.Outstanding != 0 || rec.QueueStatus.Succeeded != 4 {
		t.Fatalf("skipped-refresh queue wrong: %+v", rec.QueueStatus)
	}
}

func TestListGenerationLifecyclePassesNormalizedLimitPlusOne(t *testing.T) {
	t.Parallel()

	queryer := &fakeQueryer{responses: []fakeRows{{}}}
	store := NewStatusStore(queryer)

	if _, err := store.ListGenerationLifecycle(context.Background(), statuspkg.GenerationLifecycleFilter{
		Limit: statuspkg.MaxGenerationLifecycleLimit + 100,
	}); err != nil {
		t.Fatalf("ListGenerationLifecycle() error = %v", err)
	}
	if len(queryer.queries) != 1 {
		t.Fatalf("query count = %d, want 1", len(queryer.queries))
	}
	if !strings.Contains(queryer.queries[0], "FROM scope_generations AS generation") {
		t.Fatalf("unexpected query:\n%s", queryer.queries[0])
	}
}

func TestListGenerationLifecycleRequiresQueryer(t *testing.T) {
	t.Parallel()

	var store StatusStore
	if _, err := store.ListGenerationLifecycle(context.Background(), statuspkg.GenerationLifecycleFilter{}); err == nil {
		t.Fatal("expected error for nil queryer")
	}
}
