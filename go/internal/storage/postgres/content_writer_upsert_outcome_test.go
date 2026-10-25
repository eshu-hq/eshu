// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/content"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// outcomeFakeDB answers the #8950 pre-read with canned stored rows and
// delegates every other statement to the shared transactional fake, so the
// upsert and reference statements keep their recorded shape.
type outcomeFakeDB struct {
	*transactionalFakeDB
	storedRows [][]any
	readErr    error
}

func (d *outcomeFakeDB) QueryContext(ctx context.Context, query string, args ...any) (db.Rows, error) {
	if query != contentFileOutcomePreReadSQL {
		return d.transactionalFakeDB.QueryContext(ctx, query, args...)
	}
	d.mu.Lock()
	d.queries = append(d.queries, fakeQueryCall{query: query, args: args})
	d.mu.Unlock()
	if d.readErr != nil {
		return nil, d.readErr
	}
	return &queueFakeRows{rows: d.storedRows}, nil
}

// storedFileRow builds one pre-read result row in the column order of
// contentFileOutcomePreReadSQL.
func storedFileRow(repo, path, hash string, size int64, commit string, lines int64, lang string, gen string) []any {
	return []any{
		repo, path, hash,
		sql.NullInt64{Int64: size, Valid: true},
		sql.NullString{String: commit, Valid: commit != ""},
		sql.NullInt64{Int64: lines, Valid: true},
		sql.NullString{String: lang, Valid: lang != ""},
		sql.NullString{},
		sql.NullString{},
		sql.NullBool{},
		sql.NullString{String: gen, Valid: gen != ""},
	}
}

func upsertOutcomeRow(path, body, hash, commit, lang string) preparedFileRow {
	row := preparedFileRow{repoID: "repo-1", path: path, body: body, contentHash: hash, lineCount: lineCount(body)}
	if commit != "" {
		row.commitSHA = commit
	}
	if lang != "" {
		row.language = lang
	}
	return row
}

func TestClassifyContentFileRow(t *testing.T) {
	t.Parallel()

	const body = "package a\n"
	stored := func(mut func(*storedContentFile)) *storedContentFile {
		row := &storedContentFile{
			contentHash: "h1", bodyBytes: sql.NullInt64{Int64: int64(len(body)), Valid: true},
			commitSHA: sql.NullString{String: "c1", Valid: true}, lineCount: sql.NullInt64{Int64: 1, Valid: true},
			language:   sql.NullString{String: "go", Valid: true},
			generation: sql.NullString{String: "g1", Valid: true},
		}
		if mut != nil {
			mut(row)
		}
		return row
	}
	incoming := func(mut func(*preparedFileRow)) preparedFileRow {
		row := upsertOutcomeRow("a.go", body, "h1", "c1", "go")
		if mut != nil {
			mut(&row)
		}
		return row
	}

	cases := []struct {
		name   string
		stored *storedContentFile
		row    preparedFileRow
		tag    any
		want   fileOutcome
	}{
		{"no stored row inserts", nil, incoming(nil), "g1", outcomeInserted},
		{"identical row is skipped by the guard", stored(nil), incoming(nil), "g1", outcomeUnchangedSkipped},
		{"commit only differs", stored(nil), incoming(func(r *preparedFileRow) { r.commitSHA = "c2" }), "g1", outcomeMetadataOnly},
		{"generation only differs", stored(nil), incoming(nil), "g2", outcomeMetadataOnly},
		{"commit and generation differ", stored(nil), incoming(func(r *preparedFileRow) { r.commitSHA = "c2" }), "g2", outcomeMetadataOnly},
		{"commit goes NULL", stored(nil), incoming(func(r *preparedFileRow) { r.commitSHA = nil }), "g1", outcomeMetadataOnly},
		{"generation goes NULL", stored(nil), incoming(nil), nil, outcomeMetadataOnly},
		{"digest differs", stored(nil), incoming(func(r *preparedFileRow) { r.contentHash = "h2" }), "g1", outcomeContentChanged},
		{"digest differs with new lineage", stored(nil), incoming(func(r *preparedFileRow) { r.contentHash = "h2" }), "g2", outcomeContentChanged},
		{"same digest but different body size is not unchanged", stored(nil), incoming(func(r *preparedFileRow) { r.body = body + "x" }), "g1", outcomeContentChanged},
		{"same digest, language changed, lineage moved is not metadata only", stored(nil), incoming(func(r *preparedFileRow) { r.language = "py" }), "g2", outcomeContentChanged},
		{"same digest, language changed, same lineage is rewritten", stored(nil), incoming(func(r *preparedFileRow) { r.language = "py" }), "g1", outcomeContentChanged},
		{"language goes NULL", stored(nil), incoming(func(r *preparedFileRow) { r.language = nil }), "g1", outcomeContentChanged},
		{"line count differs", stored(nil), incoming(func(r *preparedFileRow) { r.lineCount = 9 }), "g1", outcomeContentChanged},
		{"iac_relevant appears", stored(nil), incoming(func(r *preparedFileRow) { r.iacRelevant = true }), "g1", outcomeContentChanged},
		{"stored NULL body size is not provably unchanged", stored(func(s *storedContentFile) { s.bodyBytes = sql.NullInt64{} }), incoming(nil), "g1", outcomeContentChanged},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := classifyContentFileRow(tc.stored, tc.row, tc.tag); got != tc.want {
				t.Fatalf("classifyContentFileRow() = %s, want %s", got, tc.want)
			}
		})
	}
}

// sumPoints returns the counter sum for one attribute set, or 0.
func sumPoints(t *testing.T, rm metricdata.ResourceMetrics, name string, want map[string]string) int64 {
	t.Helper()
	var total int64
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != name {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("metric %s data = %T, want Sum[int64]", name, m.Data)
			}
			for _, dp := range sum.DataPoints {
				match := true
				for k, v := range want {
					if got, _ := dp.Attributes.Value(attrKey(k)); got.AsString() != v {
						match = false
					}
				}
				if match {
					total += dp.Value
				}
			}
		}
	}
	return total
}

func newOutcomeWriter(t *testing.T, stored [][]any) (ContentWriter, *outcomeFakeDB, *sdkmetric.ManualReader, *bytes.Buffer) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	instruments, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments: %v", err)
	}
	fake := &outcomeFakeDB{transactionalFakeDB: withTransactions(&fakeExecQueryer{}), storedRows: stored}
	var logs bytes.Buffer
	// Classify every batch: the sampling policy has its own tests below.
	writer := NewContentWriter(fake).WithInstruments(instruments).WithLogger(slog.New(slog.NewJSONHandler(&logs, nil))).WithOutcomeSampling(0, 1)
	writer.Now = func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }
	return writer, fake, reader, &logs
}

// TestContentWriterCountsMixedUpsertOutcomes is the hostile batch: one write
// carries all four outcomes plus the "same digest, other column changed" row
// that must not read as unchanged.
func TestContentWriterCountsMixedUpsertOutcomes(t *testing.T) {
	t.Parallel()

	same := "package same\n"
	meta := "package meta\n"
	edited := "package edited\n"
	reclass := "package reclass\n"
	stored := [][]any{
		storedFileRow("repo-1", "same.go", "hs", int64(len(same)), "c1", 1, "go", "g2"),
		storedFileRow("repo-1", "meta.go", "hm", int64(len(meta)), "c1", 1, "go", "g1"),
		storedFileRow("repo-1", "edited.go", "he-old", int64(len(edited)), "c1", 1, "go", "g1"),
		storedFileRow("repo-1", "reclass.go", "hr", int64(len(reclass)), "c1", 1, "go", "g1"),
	}
	writer, fake, reader, logs := newOutcomeWriter(t, stored)

	mat := content.Materialization{
		RepoID: "repo-1", ScopeID: "scope-1", GenerationID: "g2", FullSnapshot: true,
		Records: []content.Record{
			{Path: "new.go", Body: "package new\n", Digest: "hn", Metadata: map[string]string{"commit_sha": "c2", "language": "go"}},
			{Path: "same.go", Body: same, Digest: "hs", Metadata: map[string]string{"commit_sha": "c1", "language": "go"}},
			{Path: "meta.go", Body: meta, Digest: "hm", Metadata: map[string]string{"commit_sha": "c2", "language": "go"}},
			{Path: "edited.go", Body: edited, Digest: "he-new", Metadata: map[string]string{"commit_sha": "c2", "language": "go"}},
			{Path: "reclass.go", Body: reclass, Digest: "hr", Metadata: map[string]string{"commit_sha": "c2", "language": "python"}},
		},
	}
	// same.go is stored at g2, the generation the writer stamps, so only it
	// matches on every compared column.

	if _, err := writer.Write(context.Background(), mat); err != nil {
		t.Fatalf("Write() error = %v, want nil", err)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	for outcome, want := range map[string]int64{
		telemetry.ContentFileUpsertOutcomeInserted:         1,
		telemetry.ContentFileUpsertOutcomeUnchangedSkipped: 1,
		telemetry.ContentFileUpsertOutcomeMetadataOnly:     1,
		telemetry.ContentFileUpsertOutcomeContentChanged:   2,
	} {
		got := sumPoints(t, rm, "eshu_dp_content_file_upserts_total", map[string]string{"outcome": outcome, "snapshot": "full"})
		if got != want {
			t.Errorf("%s count = %d, want %d", outcome, got, want)
		}
	}
	if got := sumPoints(t, rm, "eshu_dp_content_file_upserts_total", map[string]string{"snapshot": "delta"}); got != 0 {
		t.Errorf("delta count = %d, want 0 for a full snapshot", got)
	}
	if got, want := sumPoints(t, rm, "eshu_dp_content_file_upsert_body_bytes_total", map[string]string{"outcome": "metadata_only"}), int64(len(meta)); got != want {
		t.Errorf("metadata_only bytes = %d, want %d", got, want)
	}
	if got, want := sumPoints(t, rm, "eshu_dp_content_file_upsert_body_bytes_total", map[string]string{"outcome": "unchanged_skipped"}), int64(len(same)); got != want {
		t.Errorf("unchanged_skipped bytes = %d, want %d", got, want)
	}

	// The upsert itself is untouched: one guarded INSERT, no extra statement
	// from the pre-read, and the pre-read is a plain SELECT.
	inserts := 0
	for _, exec := range fake.execs {
		if strings.Contains(exec.query, "INSERT INTO content_files") {
			inserts++
			if !strings.HasSuffix(strings.TrimSpace(exec.query), strings.TrimSpace(upsertContentFileBatchSuffix)) {
				t.Fatalf("upsert no longer ends with the guarded conflict clause")
			}
		}
	}
	if inserts != 1 {
		t.Fatalf("content_files upserts = %d, want 1", inserts)
	}

	line := logLineForStage(t, logs.String(), "upsert_files")
	for key, want := range map[string]float64{
		"inserted_count": 1, "unchanged_skipped_count": 1, "metadata_only_count": 1,
		"content_changed_count": 2, "classified_batch_count": 1, "sampled_out_batch_count": 0, "classify_failed_batch_count": 0,
	} {
		if got, ok := line[key].(float64); !ok || got != want {
			t.Errorf("upsert_files log %s = %v, want %v", key, line[key], want)
		}
	}
	if line["snapshot"] != "full" {
		t.Errorf("upsert_files log snapshot = %v, want full", line["snapshot"])
	}
	if _, ok := line["classify_seconds"].(float64); !ok {
		t.Errorf("upsert_files log has no classify_seconds: %v", line)
	}
}

func TestContentWriterDeltaSnapshotLabel(t *testing.T) {
	t.Parallel()

	writer, _, reader, _ := newOutcomeWriter(t, nil)
	mat := content.Materialization{
		RepoID: "repo-1", ScopeID: "scope-1", GenerationID: "g1",
		Records: []content.Record{{Path: "a.go", Body: "package a\n", Metadata: map[string]string{"language": "go"}}},
	}
	if _, err := writer.Write(context.Background(), mat); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if got := sumPoints(t, rm, "eshu_dp_content_file_upserts_total", map[string]string{"outcome": "inserted", "snapshot": "delta"}); got != 1 {
		t.Fatalf("delta inserted = %d, want 1", got)
	}
}

func TestContentWriterPreReadFailureDoesNotFailWrite(t *testing.T) {
	t.Parallel()

	writer, fake, reader, logs := newOutcomeWriter(t, nil)
	fake.readErr = errors.New("connection reset")
	mat := content.Materialization{
		RepoID: "repo-1", ScopeID: "scope-1", GenerationID: "g1",
		Records: []content.Record{{Path: "a.go", Body: "package a\n"}},
	}
	if _, err := writer.Write(context.Background(), mat); err != nil {
		t.Fatalf("Write() error = %v, want nil: the pre-read is observability only", err)
	}
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if got := sumPoints(t, rm, "eshu_dp_content_file_upserts_total", nil); got != 0 {
		t.Fatalf("counted %d rows for an unclassified batch, want 0", got)
	}
	if !strings.Contains(logs.String(), "content_file_upsert_outcome.classify_failed") {
		t.Fatalf("no classify_failed warning logged:\n%s", logs.String())
	}
	line := logLineForStage(t, logs.String(), "upsert_files")
	if got, _ := line["classify_failed_batch_count"].(float64); got != 1 {
		t.Fatalf("classify_failed_batch_count = %v, want 1", line["classify_failed_batch_count"])
	}
	if got := sumPoints(t, rm, "eshu_dp_content_file_upsert_unclassified_rows_total", map[string]string{"reason": "classify_failed", "snapshot": "delta"}); got != 1 {
		t.Fatalf("classify_failed unclassified rows = %d, want 1: the written row must still be accounted for", got)
	}
}

func TestContentWriterUpsertFailureCountsNothing(t *testing.T) {
	t.Parallel()

	writer, fake, reader, _ := newOutcomeWriter(t, nil)
	// The reference delete runs first; fail the content_files upsert after it.
	fake.execErrors = []error{nil, errors.New("boom")}
	mat := content.Materialization{
		RepoID: "repo-1", ScopeID: "scope-1", GenerationID: "g1",
		Records: []content.Record{{Path: "a.go", Body: "package a\n"}},
	}
	if _, err := writer.Write(context.Background(), mat); err == nil {
		t.Fatal("Write() error = nil, want the upsert failure")
	}
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if got := sumPoints(t, rm, "eshu_dp_content_file_upserts_total", nil); got != 0 {
		t.Fatalf("counted %d rows for a failed upsert, want 0", got)
	}
}

// TestContentWriterWithoutObserversSkipsPreRead pins that a writer with
// neither a logger nor instruments pays nothing for the classification.
func TestContentWriterWithoutObserversSkipsPreRead(t *testing.T) {
	t.Parallel()

	fake := &outcomeFakeDB{transactionalFakeDB: withTransactions(&fakeExecQueryer{})}
	writer := NewContentWriter(fake)
	writer.Now = func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }
	mat := content.Materialization{
		RepoID: "repo-1", ScopeID: "scope-1", GenerationID: "g1",
		Records: []content.Record{{Path: "a.go", Body: "package a\n"}},
	}
	if _, err := writer.Write(context.Background(), mat); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	for _, q := range fake.queries {
		if q.query == contentFileOutcomePreReadSQL {
			t.Fatal("pre-read ran with no logger and no instruments")
		}
	}
}

// TestContentFileOutcomePreReadSQLIsLockFreeAndContentFree pins the cost and
// concurrency contract: a plain MVCC SELECT with no row lock that reads the
// body only through octet_length.
func TestContentFileOutcomePreReadSQLIsLockFreeAndContentFree(t *testing.T) {
	t.Parallel()

	q := strings.ToUpper(contentFileOutcomePreReadSQL)
	for _, banned := range []string{"FOR UPDATE", "FOR SHARE", "FOR NO KEY", "FOR KEY SHARE", "SKIP LOCKED", "NOWAIT", "INSERT", "UPDATE ", "DELETE"} {
		if strings.Contains(q, banned) {
			t.Fatalf("pre-read contains %q; it must be a lock-free SELECT", banned)
		}
	}
	bare := regexp.MustCompile(`(?i)(^|[^_a-z])content([^_a-z]|$)`)
	stripped := strings.ReplaceAll(contentFileOutcomePreReadSQL, "octet_length(c.content)", "")
	stripped = strings.ReplaceAll(stripped, "content_files", "")
	stripped = strings.ReplaceAll(stripped, "content_hash", "")
	if bare.MatchString(stripped) {
		t.Fatalf("pre-read reads the content column outside octet_length:\n%s", contentFileOutcomePreReadSQL)
	}
}

// TestClassifierComparesEveryGuardColumn keeps the classifier in step with the
// upsert guard. The guarded columns are parsed from the shipped
// upsertContentFileConflictClause; each one needs a mutation here, and every
// mutation of a guarded column must stop the row reading as unchanged_skipped.
// A guard column added without a classifier change fails the key check; a
// classifier that ignores a column fails the mutation check.
func TestClassifierComparesEveryGuardColumn(t *testing.T) {
	t.Parallel()

	guard := guardTuplesPattern.FindStringSubmatch(upsertContentFileConflictClause)
	if guard == nil {
		t.Fatal("guard tuples not found in upsertContentFileConflictClause")
	}
	var guardColumns []string
	for _, item := range splitColumns(guard[1]) {
		guardColumns = append(guardColumns, strings.TrimPrefix(item, "content_files."))
	}
	// content is compared by its own OR leg, not in the tuples.
	guardColumns = append(guardColumns, contentFileSeparateCompare)

	const body = "package a\n"
	mutations := map[string]func(*preparedFileRow, *any){
		"commit_sha":       func(r *preparedFileRow, _ *any) { r.commitSHA = "c-other" },
		"content_hash":     func(r *preparedFileRow, _ *any) { r.contentHash = "h-other" },
		"line_count":       func(r *preparedFileRow, _ *any) { r.lineCount++ },
		"language":         func(r *preparedFileRow, _ *any) { r.language = "py" },
		"artifact_type":    func(r *preparedFileRow, _ *any) { r.artifactType = "template" },
		"template_dialect": func(r *preparedFileRow, _ *any) { r.templateDialect = "jinja" },
		"iac_relevant":     func(r *preparedFileRow, _ *any) { r.iacRelevant = true },
		"generation_id":    func(_ *preparedFileRow, tag *any) { *tag = "g-other" },
		"content":          func(r *preparedFileRow, _ *any) { r.body = body + "x" },
	}
	for _, column := range guardColumns {
		if mutations[column] == nil {
			t.Errorf("guard column %q has no classifier mutation; teach classifyContentFileRow about it", column)
		}
	}
	for column := range mutations {
		if !slices.Contains(guardColumns, column) {
			t.Errorf("mutation %q is not a guard column of the shipped clause", column)
		}
	}

	stored := &storedContentFile{
		contentHash: "h1", bodyBytes: sql.NullInt64{Int64: int64(len(body)), Valid: true},
		commitSHA: sql.NullString{String: "c1", Valid: true}, lineCount: sql.NullInt64{Int64: 1, Valid: true},
		language: sql.NullString{String: "go", Valid: true}, generation: sql.NullString{String: "g1", Valid: true},
	}
	// The baseline carries NULL artifact_type, template_dialect and iac_relevant,
	// so setting any of them is a NULL-to-value transition.
	for _, column := range guardColumns {
		mutate := mutations[column]
		if mutate == nil {
			continue
		}
		row := upsertOutcomeRow("a.go", body, "h1", "c1", "go")
		var tag any = "g1"
		if got := classifyContentFileRow(stored, row, tag); got != outcomeUnchangedSkipped {
			t.Fatalf("baseline classified %s, want unchanged_skipped", got)
		}
		mutate(&row, &tag)
		if got := classifyContentFileRow(stored, row, tag); got == outcomeUnchangedSkipped {
			t.Errorf("changing guard column %q still reads unchanged_skipped", column)
		}
	}
}
