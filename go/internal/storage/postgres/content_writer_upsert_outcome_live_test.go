// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8950 disposable PostgreSQL 18 proof that the content_files upsert outcome classification agrees with the row versions the real guarded upsert writes (a skipped row keeps its xmin, every other class gets a new version), that the pre-read takes no lock and sees only committed state while a writer holds row locks and uncommitted edits, that observed and unobserved writers leave identical rows, and that concurrent observed writers hit no error and count every row once; enrolled in the live-postgres-readiness runner; bridges the family ESHU_GENERATION_RETENTION_PROOF_DSN onto ESHU_POSTGRES_TEST_DSN as the retention proofs do; wall time is logged, not gated
//eshu:readiness:env ESHU_AWS_DRIFT_ARN_PROOF_DISPOSABLE ESHU_AWS_DRIFT_ARN_PROOF_DSN ESHU_DEFERRED_PARTITION_PROOF_DISPOSABLE ESHU_DEFERRED_PARTITION_PROOF_DSN ESHU_GENERATION_RETENTION_PROOF_DISPOSABLE ESHU_GENERATION_RETENTION_PROOF_DSN ESHU_PROJECTOR_CLAIM_DEADLOCK_PROOF_DSN ESHU_REDUCER_FAIRNESS_PROOF_DSN

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/content"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

func newLiveOutcomeWriter(t *testing.T, database *sql.DB) (ContentWriter, *sdkmetric.ManualReader) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	instruments, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments: %v", err)
	}
	// Classify every batch: the correctness proofs need each one, and the
	// sampling policy has its own tests.
	writer := NewContentWriter(SQLDB{DB: database}).WithInstruments(instruments).WithOutcomeSampling(0, 1)
	writer.Now = func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }
	return writer, reader
}

func liveOutcomeRecord(path, body, digest, commit, language string) content.Record {
	return content.Record{
		Path: path, Body: body, Digest: digest,
		Metadata: map[string]string{"commit_sha": commit, "language": language},
	}
}

func countedOutcome(t *testing.T, reader *sdkmetric.ManualReader, outcome string) int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	return sumPoints(t, rm, "eshu_dp_content_file_upserts_total", map[string]string{"outcome": outcome})
}

// TestContentFileUpsertOutcomeMatchesRowVersionsLive proves the classification
// against the ground truth the guarded upsert leaves behind: a row version is
// rewritten (new xmin) exactly when the class is not unchanged_skipped, and a
// new path only exists after the write.
func TestContentFileUpsertOutcomeMatchesRowVersionsLive(t *testing.T) {
	ctx, database := openContentUpsertGuardDatabase(t)
	const repoID = "repo-8950-versions"
	writer, reader := newLiveOutcomeWriter(t, database)
	materialize := func(gen string, records ...content.Record) content.Materialization {
		return content.Materialization{RepoID: repoID, ScopeID: "scope-8950", GenerationID: gen, Records: records}
	}
	seed := materialize("gen-1",
		liveOutcomeRecord("unchanged.go", "package a\n", "ha", "c1", "go"),
		liveOutcomeRecord("commit.go", "package b\n", "hb", "c1", "go"),
		liveOutcomeRecord("edit.go", "package c\n", "hc", "c1", "go"),
		liveOutcomeRecord("lang.go", "package d\n", "hd", "c1", "go"),
	)
	if _, err := writer.Write(ctx, seed); err != nil {
		t.Fatalf("seed Write: %v", err)
	}
	before := readContentFileVersions(t, ctx, database, repoID)

	// Same generation: unchanged.go skips, commit.go moves its commit only.
	next := materialize("gen-1",
		liveOutcomeRecord("unchanged.go", "package a\n", "ha", "c1", "go"),
		liveOutcomeRecord("commit.go", "package b\n", "hb", "c2", "go"),
		liveOutcomeRecord("edit.go", "package c2\n", "hc2", "c1", "go"),
		liveOutcomeRecord("lang.go", "package d\n", "hd", "c1", "python"),
		liveOutcomeRecord("new.go", "package e\n", "he", "c1", "go"),
	)
	want := map[string]fileOutcome{
		"unchanged.go": outcomeUnchangedSkipped, "commit.go": outcomeMetadataOnly,
		"edit.go": outcomeContentChanged, "lang.go": outcomeContentChanged, "new.go": outcomeInserted,
	}
	// Prove the pre-read directly on the real database before the write.
	var batch []preparedFileRow
	for _, rec := range next.Records {
		hash, _ := fileContentHash(rec)
		row := upsertOutcomeRow(rec.Path, rec.Body, hash, rec.Metadata["commit_sha"], rec.Metadata["language"])
		row.repoID = repoID
		batch = append(batch, row)
	}
	got, err := writer.classifyFileBatch(ctx, batch, "gen-1")
	if err != nil {
		t.Fatalf("classifyFileBatch: %v", err)
	}
	for i, row := range batch {
		if got[i] != want[row.path] {
			t.Errorf("%s classified %s, want %s", row.path, got[i], want[row.path])
		}
	}

	if _, err := writer.Write(ctx, next); err != nil {
		t.Fatalf("Write: %v", err)
	}
	after := readContentFileVersions(t, ctx, database, repoID)
	for path, class := range want {
		rewritten := after[path].xmin != before[path].xmin
		if class == outcomeUnchangedSkipped && rewritten {
			t.Errorf("%s predicted unchanged_skipped but the upsert rewrote the row", path)
		}
		if class != outcomeUnchangedSkipped && !rewritten {
			t.Errorf("%s predicted %s but the upsert left the row version alone", path, class)
		}
	}
	for class, wantCount := range map[string]int64{"inserted": 1, "unchanged_skipped": 1, "metadata_only": 1, "content_changed": 2} {
		// The seed Write counted its 4 new rows as inserted too.
		if class == "inserted" {
			wantCount += 4
		}
		if gotCount := countedOutcome(t, reader, class); gotCount != wantCount {
			t.Errorf("counter %s = %d, want %d", class, gotCount, wantCount)
		}
	}
}

// TestContentFileUpsertOutcomeProxyLimitLive pins the one case the content-free
// pre-read cannot see: a caller-supplied digest and a body size that both match
// while the body differs. The guard compares the text and rewrites the row; the
// classification reads it as unchanged. A redact content-store policy produces
// that shape by design (it keeps the source digest and stores the policy
// output), so the implementation PR for #8950 counts from the statement's own
// result instead of this proxy.
func TestContentFileUpsertOutcomeProxyLimitLive(t *testing.T) {
	ctx, database := openContentUpsertGuardDatabase(t)
	const repoID = "repo-8950-proxy"
	writer, _ := newLiveOutcomeWriter(t, database)
	materialize := func(body string) content.Materialization {
		return content.Materialization{
			RepoID: repoID, ScopeID: "scope-8950", GenerationID: "gen-1",
			Records: []content.Record{liveOutcomeRecord("liar.go", body, "same-digest", "c1", "go")},
		}
	}
	if _, err := writer.Write(ctx, materialize("package aaaa\n")); err != nil {
		t.Fatalf("seed Write: %v", err)
	}
	before := readContentFileVersions(t, ctx, database, repoID)
	row := upsertOutcomeRow("liar.go", "package bbbb\n", "same-digest", "c1", "go")
	row.repoID = repoID
	got, err := writer.classifyFileBatch(ctx, []preparedFileRow{row}, "gen-1")
	if err != nil {
		t.Fatalf("classifyFileBatch: %v", err)
	}
	if got[0] != outcomeUnchangedSkipped {
		t.Fatalf("proxy classified %s, want unchanged_skipped (the documented proxy limit)", got[0])
	}
	if _, err := writer.Write(ctx, materialize("package bbbb\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	after := readContentFileVersions(t, ctx, database, repoID)
	if after["liar.go"].xmin == before["liar.go"].xmin {
		t.Fatal("the guard skipped a row whose body differs; the #8915 content compare regressed")
	}
}

// TestContentFileOutcomePreReadTakesNoLockLive holds row locks and an
// uncommitted edit on the rows a batch is about to classify, then proves the
// pre-read returns at once with the committed state.
func TestContentFileOutcomePreReadTakesNoLockLive(t *testing.T) {
	ctx, database := openContentUpsertGuardDatabase(t)
	const repoID = "repo-8950-nolock"
	writer, _ := newLiveOutcomeWriter(t, database)
	if _, err := writer.Write(ctx, content.Materialization{
		RepoID: repoID, ScopeID: "scope-8950", GenerationID: "gen-1",
		Records: []content.Record{liveOutcomeRecord("locked.go", "package a\n", "ha", "c1", "go")},
	}); err != nil {
		t.Fatalf("seed Write: %v", err)
	}

	holder, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin holder: %v", err)
	}
	defer func() { _ = holder.Rollback() }()
	if _, err := holder.ExecContext(ctx,
		`UPDATE content_files SET content = 'package edited\n', content_hash = 'hx' WHERE repo_id = $1 AND relative_path = 'locked.go'`, repoID); err != nil {
		t.Fatalf("holder update: %v", err)
	}

	row := upsertOutcomeRow("locked.go", "package a\n", "ha", "c1", "go")
	row.repoID = repoID
	readCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	start := time.Now()
	got, err := writer.classifyFileBatch(readCtx, []preparedFileRow{row}, "gen-1")
	if err != nil {
		t.Fatalf("pre-read blocked or failed behind a row lock after %s: %v", time.Since(start), err)
	}
	if got[0] != outcomeUnchangedSkipped {
		t.Fatalf("pre-read saw %s, want the committed state (unchanged_skipped)", got[0])
	}
	var locks int
	if err := database.QueryRowContext(ctx,
		`SELECT count(*) FROM pg_locks WHERE locktype = 'transactionid' AND NOT granted`).Scan(&locks); err != nil {
		t.Fatalf("read pg_locks: %v", err)
	}
	if locks != 0 {
		t.Fatalf("%d ungranted transaction locks after the pre-read, want 0", locks)
	}
}

// TestContentFileUpsertObserversDoNotChangeRowsLive writes the same sequence
// through an observed and an unobserved writer and requires identical rows and
// identical rewrite decisions.
func TestContentFileUpsertObserversDoNotChangeRowsLive(t *testing.T) {
	ctx, database := openContentUpsertGuardDatabase(t)
	observed, _ := newLiveOutcomeWriter(t, database)
	plain := NewContentWriter(SQLDB{DB: database})
	plain.Now = observed.Now
	sequence := func(writer ContentWriter, repoID string) map[string]contentFileVersionRow {
		steps := [][]content.Record{
			{liveOutcomeRecord("a.go", "package a\n", "ha", "c1", "go"), liveOutcomeRecord("b.go", "package b\n", "hb", "c1", "go")},
			{liveOutcomeRecord("a.go", "package a\n", "ha", "c2", "go"), liveOutcomeRecord("b.go", "package b2\n", "hb2", "c1", "go")},
			{liveOutcomeRecord("a.go", "package a\n", "ha", "c2", "go"), liveOutcomeRecord("b.go", "package b2\n", "hb2", "c1", "go")},
		}
		for i, records := range steps {
			if _, err := writer.Write(ctx, content.Materialization{
				RepoID: repoID, ScopeID: "scope-8950", GenerationID: fmt.Sprintf("gen-%d", i+1), Records: records,
			}); err != nil {
				t.Fatalf("Write step %d: %v", i, err)
			}
		}
		return readContentFileVersions(t, ctx, database, repoID)
	}
	withObs := sequence(observed, "repo-8950-observed")
	without := sequence(plain, "repo-8950-plain")
	for path, a := range withObs {
		b := without[path]
		if a.content != b.content || a.commitSHA != b.commitSHA || a.generation != b.generation || !a.indexedAt.Equal(b.indexedAt) {
			t.Errorf("%s differs: observed %+v, plain %+v", path, a, b)
		}
	}
	if len(withObs) != len(without) {
		t.Fatalf("row count observed %d, plain %d", len(withObs), len(without))
	}
}

// TestContentFileUpsertOutcomeConcurrentWritersLive runs observed writers on
// the same keys at once: no error or deadlock, and every written row is counted
// exactly once whatever the interleaving classified it as.
func TestContentFileUpsertOutcomeConcurrentWritersLive(t *testing.T) {
	ctx, database := openContentUpsertGuardDatabase(t)
	const repoID = "repo-8950-concurrent"
	writer, reader := newLiveOutcomeWriter(t, database)
	var records []content.Record
	for i := range 40 {
		records = append(records, liveOutcomeRecord(fmt.Sprintf("f%02d.go", i), fmt.Sprintf("package f%02d\n", i), fmt.Sprintf("h%02d", i), "c1", "go"))
	}
	const workers, rounds = 6, 5
	var wg sync.WaitGroup
	errs := make(chan error, workers*rounds)
	for w := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for r := range rounds {
				batch := append([]content.Record(nil), records...)
				if w%2 == 1 {
					// Editing sessions change a third of the files.
					for i := 0; i < len(batch); i += 3 {
						batch[i] = liveOutcomeRecord(batch[i].Path, batch[i].Body+fmt.Sprintf("// w%d r%d\n", w, r), fmt.Sprintf("hw%dr%d-%d", w, r, i), "c1", "go")
					}
				}
				if _, err := writer.Write(ctx, content.Materialization{
					RepoID: repoID, ScopeID: "scope-8950", GenerationID: fmt.Sprintf("gen-%d-%d", w, r), Records: batch,
				}); err != nil {
					errs <- err
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if strings.Contains(err.Error(), "40P01") || strings.Contains(err.Error(), "deadlock") {
			t.Fatalf("deadlock under concurrent observed writers: %v", err)
		}
		t.Fatalf("concurrent Write: %v", err)
	}
	var total int64
	for _, class := range fileOutcomeNames {
		total += countedOutcome(t, reader, class)
	}
	if want := int64(workers * rounds * len(records)); total != want {
		t.Fatalf("counted %d rows, want %d (every written row exactly once)", total, want)
	}
}
