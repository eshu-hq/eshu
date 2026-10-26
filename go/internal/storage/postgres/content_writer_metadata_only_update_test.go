// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/content"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// metadataOnlySetPattern extracts the UPDATE ... SET list.
var metadataOnlySetPattern = regexp.MustCompile(`(?is)^UPDATE\s+content_files\s+AS\s+c\s+SET\s+(.*?)\s+FROM\s+\(VALUES`)

// TestMetadataOnlyUpdateSETAssignsOnlyLineage pins the HOT contract: the
// metadata UPDATE assigns exactly commit_sha and generation_id. Assigning
// content (or its generated content_lower copy) or indexed_at would cost the
// HOT update the statement exists for.
func TestMetadataOnlyUpdateSETAssignsOnlyLineage(t *testing.T) {
	t.Parallel()

	set := metadataOnlySetPattern.FindStringSubmatch(metadataOnlyContentFileUpdatePrefix)
	if set == nil {
		t.Fatal("SET list not found in metadataOnlyContentFileUpdatePrefix")
	}
	var assigned []string
	for _, item := range splitColumns(set[1]) {
		parts := strings.SplitN(item, "=", 2)
		if len(parts) != 2 {
			t.Fatalf("SET item %q is not an assignment", item)
		}
		target := strings.TrimSpace(parts[0])
		source := strings.TrimSpace(parts[1])
		if strings.Contains(target, ".") || source != "v."+target {
			t.Fatalf("SET item %q must move a VALUES column onto the row", item)
		}
		assigned = append(assigned, target)
	}
	slices.Sort(assigned)
	if want := []string{"commit_sha", "generation_id"}; !slices.Equal(assigned, want) {
		t.Fatalf("SET assigns %v, want %v", assigned, want)
	}
}

// TestMetadataOnlyUpdateRechecksEveryGuardColumn keeps the UPDATE's WHERE in
// step with the guarded upsert: it must recheck every equality the guard
// compares (so a concurrent change between the pre-read and the UPDATE misses
// and falls through to the upsert), plus the lineage-differ predicate. The
// only guard column it must not carry is indexed_at, which it never moves.
func TestMetadataOnlyUpdateRechecksEveryGuardColumn(t *testing.T) {
	t.Parallel()

	guard := guardTuplesPattern.FindStringSubmatch(upsertContentFileConflictClause)
	if guard == nil {
		t.Fatal("guard tuples not found in upsertContentFileConflictClause")
	}
	var guardColumns []string
	for _, item := range splitColumns(guard[1]) {
		guardColumns = append(guardColumns, strings.TrimPrefix(item, "content_files."))
	}
	guardColumns = append(guardColumns, contentFileSeparateCompare)

	recheck := map[string]string{
		"commit_sha":       "c.commit_sha IS DISTINCT FROM v.commit_sha",
		"content_hash":     "c.content_hash = v.content_hash",
		"line_count":       "c.line_count IS NOT DISTINCT FROM v.line_count",
		"language":         "c.language IS NOT DISTINCT FROM v.language",
		"artifact_type":    "c.artifact_type IS NOT DISTINCT FROM v.artifact_type",
		"template_dialect": "c.template_dialect IS NOT DISTINCT FROM v.template_dialect",
		"iac_relevant":     "c.iac_relevant IS NOT DISTINCT FROM v.iac_relevant",
		"generation_id":    "c.generation_id IS DISTINCT FROM v.generation_id",
		"content":          "c.content IS NOT DISTINCT FROM v.body",
	}
	for _, column := range guardColumns {
		fragment, ok := recheck[column]
		if !ok {
			t.Errorf("guard column %q has no recheck in the metadata UPDATE; teach the UPDATE about it", column)
			continue
		}
		if !strings.Contains(metadataOnlyContentFileUpdateSuffix, fragment) {
			t.Errorf("metadata UPDATE suffix lacks %q for guard column %q", fragment, column)
		}
	}
	for column := range recheck {
		if !slices.Contains(guardColumns, column) {
			t.Errorf("recheck column %q is not a guard column of the shipped clause", column)
		}
	}
	if !strings.Contains(metadataOnlyContentFileUpdateSuffix, "octet_length(c.content) = v.body_len") {
		t.Errorf("metadata UPDATE suffix lacks the body-size recheck")
	}
	if strings.Contains(metadataOnlyContentFileUpdateSuffix, "indexed_at") {
		t.Errorf("metadata UPDATE must not mention indexed_at: moving it costs the HOT update")
	}
	if !strings.Contains(metadataOnlyContentFileUpdateSuffix, "RETURNING c.repo_id, c.relative_path") {
		t.Errorf("metadata UPDATE suffix lacks the RETURNING keys the fall-through needs")
	}
}

// TestPartitionMetadataOnlyBatch splits a classified batch on the outcome and
// keeps an unclassified batch on the single statement.
func TestPartitionMetadataOnlyBatch(t *testing.T) {
	t.Parallel()

	batch := []preparedFileRow{
		{repoID: "r", path: "meta.go"},
		{repoID: "r", path: "changed.go"},
		{repoID: "r", path: "inserted.go"},
		{repoID: "r", path: "skipped.go"},
	}
	outcomes := []fileOutcome{outcomeMetadataOnly, outcomeContentChanged, outcomeInserted, outcomeUnchangedSkipped}
	meta, rest := partitionMetadataOnlyBatch(batch, outcomes)
	if len(meta) != 1 || meta[0].path != "meta.go" {
		t.Fatalf("meta = %v, want [meta.go]", meta)
	}
	if len(rest) != 3 {
		t.Fatalf("rest = %v, want the other three rows", rest)
	}

	if meta, rest := partitionMetadataOnlyBatch(batch, nil); len(meta) != 0 || len(rest) != len(batch) {
		t.Fatalf("unclassified batch split into %d meta and %d rest, want 0 and %d", len(meta), len(rest), len(batch))
	}
	if meta, rest := partitionMetadataOnlyBatch(nil, nil); len(meta) != 0 || len(rest) != 0 {
		t.Fatalf("empty batch split into %d meta and %d rest, want 0 and 0", len(meta), len(rest))
	}
}

// TestObservedBatchFallsThroughMissedMetadataRows proves the wiring: the
// batch writer sends metadata-classified rows to the UPDATE, and a row the
// UPDATE did not touch (its RETURNING missed it, as after a concurrent edit)
// goes through the guarded upsert instead of being dropped.
func TestObservedBatchFallsThroughMissedMetadataRows(t *testing.T) {
	t.Parallel()

	metaBody := "package meta\n"
	changedBody := "package changed\n"
	stored := [][]any{
		storedFileRow("repo-1", "meta.go", "hm", int64(len(metaBody)), "c1", 1, "go", "g1"),
		storedFileRow("repo-1", "missed.go", "hm", int64(len(metaBody)), "c1", 1, "go", "g1"),
		storedFileRow("repo-1", "changed.go", "he-old", int64(len(metaBody)), "c1", 1, "go", "g1"),
	}
	writer, fake, reader, _ := newOutcomeWriter(t, stored)
	// The UPDATE touches only meta.go: missed.go behaves as if a concurrent
	// edit moved it under the pre-read.
	fake.updatedRows = [][]any{{"repo-1", "meta.go"}}

	mat := content.Materialization{
		RepoID: "repo-1", ScopeID: "scope-1", GenerationID: "g2", FullSnapshot: true,
		Records: []content.Record{
			{Path: "meta.go", Body: metaBody, Digest: "hm", Metadata: map[string]string{"commit_sha": "c2", "language": "go"}},
			{Path: "missed.go", Body: metaBody, Digest: "hm", Metadata: map[string]string{"commit_sha": "c2", "language": "go"}},
			{Path: "changed.go", Body: changedBody, Digest: "he-new", Metadata: map[string]string{"commit_sha": "c2", "language": "go"}},
		},
	}
	if _, err := writer.Write(context.Background(), mat); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	inner := fake.fakeExecQueryer
	var updateQuery, upsertQuery string
	for _, q := range inner.queries {
		if strings.HasPrefix(strings.TrimSpace(q.query), "UPDATE content_files") {
			updateQuery = q.query
		}
	}
	for _, e := range inner.execs {
		if strings.Contains(e.query, "INSERT INTO content_files") {
			upsertQuery = e.query
		}
	}
	if updateQuery == "" {
		t.Fatal("no metadata UPDATE issued")
	}
	if strings.Count(updateQuery, "($") != 2 {
		t.Fatalf("UPDATE carries %d value groups, want 2 (both metadata-classified rows)", strings.Count(updateQuery, "($"))
	}
	// Untyped multi-row VALUES parameters default to text and break the
	// size/count comparisons (42883 on the real database): every placeholder
	// carries its cast.
	for _, cast := range []string{"$1::text", "$6::bigint", "$8::integer", "$12::boolean"} {
		if !strings.Contains(updateQuery, cast) {
			t.Errorf("UPDATE lacks placeholder cast %q", cast)
		}
	}
	if upsertQuery == "" {
		t.Fatal("no guarded upsert issued for the fall-through rows")
	}
	if strings.Count(upsertQuery, "($") != 2 {
		t.Fatalf("upsert carries %d value groups, want 2 (missed.go and changed.go)", strings.Count(upsertQuery, "($"))
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	for outcome, want := range map[string]int64{
		telemetry.ContentFileUpsertOutcomeMetadataOnly:   2,
		telemetry.ContentFileUpsertOutcomeContentChanged: 1,
	} {
		got := sumPoints(t, rm, "eshu_dp_content_file_upserts_total", map[string]string{"outcome": outcome, "snapshot": "full"})
		if got != want {
			t.Errorf("%s count = %d, want %d", outcome, got, want)
		}
	}
}

// TestContentFileWriteConflictClass pins the retryable set: deadlocks and
// serialization failures retry, everything else (including nil) returns.
func TestContentFileWriteConflictClass(t *testing.T) {
	t.Parallel()

	if got := contentFileWriteConflictClass(nil); got != "" {
		t.Errorf("nil error class = %q, want none", got)
	}
	if got := contentFileWriteConflictClass(errors.New("boom")); got != "" {
		t.Errorf("plain error class = %q, want none", got)
	}
	for code, want := range map[string]string{"40P01": "deadlock", "40001": "serialization_failure", "23505": ""} {
		if got := contentFileWriteConflictClass(&pgconn.PgError{Code: code}); got != want {
			t.Errorf("code %s class = %q, want %q", code, got, want)
		}
	}
}

// TestObservedBatchRetriesDeadlockedUpdate proves the conflict discipline: a
// deadlocked metadata UPDATE is retried with a fresh pre-read, the Write
// succeeds, and the row is counted once.
func TestObservedBatchRetriesDeadlockedUpdate(t *testing.T) {
	t.Parallel()

	metaBody := "package meta\n"
	stored := [][]any{
		storedFileRow("repo-1", "meta.go", "hm", int64(len(metaBody)), "c1", 1, "go", "g1"),
	}
	writer, fake, reader, logs := newOutcomeWriter(t, stored)
	fake.updateErrs = []error{&pgconn.PgError{Code: "40P01", Message: "deadlock detected"}}

	mat := content.Materialization{
		RepoID: "repo-1", ScopeID: "scope-1", GenerationID: "g2", FullSnapshot: true,
		Records: []content.Record{
			{Path: "meta.go", Body: metaBody, Digest: "hm", Metadata: map[string]string{"commit_sha": "c2", "language": "go"}},
		},
	}
	if _, err := writer.Write(context.Background(), mat); err != nil {
		t.Fatalf("Write() error = %v, want nil after a retried deadlock", err)
	}

	updates := 0
	for _, q := range fake.queries {
		if strings.HasPrefix(strings.TrimSpace(q.query), "UPDATE content_files") {
			updates++
		}
	}
	if updates != 2 {
		t.Fatalf("metadata UPDATE issued %d times, want 2 (deadlock plus retry)", updates)
	}
	if !strings.Contains(logs.String(), "content_file_write.conflict_retry") {
		t.Errorf("no conflict_retry warning logged:\n%s", logs.String())
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if got := sumPoints(t, rm, "eshu_dp_content_file_upserts_total", map[string]string{"outcome": "metadata_only", "snapshot": "full"}); got != 1 {
		t.Errorf("metadata_only count = %d, want 1 (counted once, for the attempt that succeeded)", got)
	}
}

// TestUnobservedBatchKeepsSingleStatement proves the prototype changes
// nothing for a writer without observers: one INSERT, no UPDATE.
func TestUnobservedBatchKeepsSingleStatement(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := &fakeExecQueryer{}
	writer := NewContentWriter(withTransactions(db))
	writer.Now = func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }

	batch := []preparedFileRow{
		{repoID: "r", path: "a.go", commitSHA: "c2", body: "package a\n", contentHash: "h", lineCount: 1, language: "go"},
	}
	if err := writer.upsertContentFileBatch(ctx, batch, writer.Now(), "g2"); err != nil {
		t.Fatalf("upsertContentFileBatch: %v", err)
	}
	for _, q := range db.queries {
		if strings.HasPrefix(strings.TrimSpace(q.query), "UPDATE content_files") {
			t.Fatalf("unobserved writer issued a metadata UPDATE")
		}
	}
	if len(db.queries) != 0 {
		t.Fatalf("unobserved writer issued %d queries, want none (no pre-read)", len(db.queries))
	}
	inserts := 0
	for _, e := range db.execs {
		if strings.Contains(e.query, "INSERT INTO content_files") {
			inserts++
		}
	}
	if inserts != 1 {
		t.Fatalf("unobserved writer issued %d content_files inserts, want 1", inserts)
	}
}

// TestObservedBatchConflictRetryEmitsCounter proves the #8950 conflict_retry
// warning has a counter behind it: a retried deadlock emits
// eshu_dp_content_file_write_conflict_retries_total once with
// failure_class=deadlock, alongside the warning log.
func TestObservedBatchConflictRetryEmitsCounter(t *testing.T) {
	t.Parallel()

	metaBody := "package meta\n"
	stored := [][]any{
		storedFileRow("repo-1", "meta.go", "hm", int64(len(metaBody)), "c1", 1, "go", "g1"),
	}
	writer, fake, reader, logs := newOutcomeWriter(t, stored)
	fake.updateErrs = []error{&pgconn.PgError{Code: "40P01", Message: "deadlock detected"}}

	mat := content.Materialization{
		RepoID: "repo-1", ScopeID: "scope-1", GenerationID: "g2", FullSnapshot: true,
		Records: []content.Record{
			{Path: "meta.go", Body: metaBody, Digest: "hm", Metadata: map[string]string{"commit_sha": "c2", "language": "go"}},
		},
	}
	if _, err := writer.Write(context.Background(), mat); err != nil {
		t.Fatalf("Write() error = %v, want nil after a retried deadlock", err)
	}
	if !strings.Contains(logs.String(), "content_file_write.conflict_retry") {
		t.Errorf("no conflict_retry warning logged:\n%s", logs.String())
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if got := sumPoints(t, rm, "eshu_dp_content_file_write_conflict_retries_total", map[string]string{"failure_class": "deadlock"}); got != 1 {
		t.Errorf("conflict retry count = %d, want 1", got)
	}
}
