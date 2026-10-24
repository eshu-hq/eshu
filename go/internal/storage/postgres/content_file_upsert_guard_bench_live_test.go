// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8915 disposable PostgreSQL 18 smoke proof that a findings-heavy content_files replay through the real ContentWriter writes under 64 KiB of WAL and that a batch whose rows all changed still writes row WAL, with an opt-in (ESHU_CONTENT_UPSERT_GUARD_BENCH=1) full-size measurement run that asserts nothing; enrolled in the live-postgres-readiness runner; bridges the family ESHU_GENERATION_RETENTION_PROOF_DSN onto ESHU_POSTGRES_TEST_DSN as the retention proofs do
//eshu:readiness:env ESHU_AWS_DRIFT_ARN_PROOF_DISPOSABLE ESHU_AWS_DRIFT_ARN_PROOF_DSN ESHU_DEFERRED_PARTITION_PROOF_DISPOSABLE ESHU_DEFERRED_PARTITION_PROOF_DSN ESHU_GENERATION_RETENTION_PROOF_DISPOSABLE ESHU_GENERATION_RETENTION_PROOF_DSN ESHU_PROJECTOR_CLAIM_DEADLOCK_PROOF_DSN ESHU_REDUCER_FAIRNESS_PROOF_DSN

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/content"
)

// explainedFileUpsert is what one content_files upsert statement cost, read
// from EXPLAIN (ANALYZE, BUFFERS, WAL) of the statement the real ContentWriter
// issued.
type explainedFileUpsert struct {
	executionMS float64
	walBytes    int64
	sharedRead  int64
	sharedDirty int64
}

// explainingFileDB runs the ContentWriter's content_files upsert under
// EXPLAIN (ANALYZE, BUFFERS, WAL) and records the numbers; every other
// statement passes through. EXPLAIN ANALYZE executes the statement, so the
// writer's behavior is unchanged.
type explainingFileDB struct {
	SQLDB
	plans *[]explainedFileUpsert
}

type zeroResult struct{}

func (zeroResult) LastInsertId() (int64, error) { return 0, nil }
func (zeroResult) RowsAffected() (int64, error) { return 0, nil }

func (d explainingFileDB) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	if !strings.HasPrefix(query, "INSERT INTO content_files") {
		return d.SQLDB.ExecContext(ctx, query, args...)
	}
	var raw string
	if err := d.DB.QueryRowContext(ctx, "EXPLAIN (ANALYZE, BUFFERS, WAL, FORMAT JSON) "+query, args...).Scan(&raw); err != nil {
		return nil, err
	}
	var plan []struct {
		Plan struct {
			WALBytes    int64 `json:"WAL Bytes"`
			SharedRead  int64 `json:"Shared Read Blocks"`
			SharedDirty int64 `json:"Shared Dirtied Blocks"`
		} `json:"Plan"`
		ExecutionTime float64 `json:"Execution Time"`
	}
	if err := json.Unmarshal([]byte(raw), &plan); err != nil || len(plan) != 1 {
		return nil, fmt.Errorf("parse explain: %v", err)
	}
	*d.plans = append(*d.plans, explainedFileUpsert{
		executionMS: plan[0].ExecutionTime, walBytes: plan[0].Plan.WALBytes,
		sharedRead: plan[0].Plan.SharedRead, sharedDirty: plan[0].Plan.SharedDirty,
	})
	return zeroResult{}, nil
}

// findingsHeavyCorpus builds the #8915 measurement corpus: files of about
// sizeBytes whose bodies carry findings secret-like lines each, deterministic
// for a seed.
func findingsHeavyCorpus(files, sizeBytes, findings int) []content.Record {
	rng := rand.New(rand.NewSource(8915))
	words := strings.Fields("alpha beta gamma delta config server client handler request response buffer stream parse render index query table column schema module package import export return value result error status header cookie session cache queue worker")
	token := func(n int) string {
		const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
		out := make([]byte, n)
		for i := range out {
			out[i] = alphabet[rng.Intn(len(alphabet))]
		}
		return string(out)
	}
	secret := []func() string{
		func() string { return `password = "` + token(12) + `"` },
		func() string { return `api_key: ` + token(24) },
		func() string { return `client_secret="` + token(20) + `"` },
		func() string { return `token = "` + token(16) + `"` },
	}
	records := make([]content.Record, 0, files)
	for f := 0; f < files; f++ {
		var lines []string
		total := 0
		for total < sizeBytes {
			line := fmt.Sprintf("    func %s_%s(%s, %s) { // %s %s", words[rng.Intn(len(words))], words[rng.Intn(len(words))],
				words[rng.Intn(len(words))], words[rng.Intn(len(words))], words[rng.Intn(len(words))], words[rng.Intn(len(words))])
			lines = append(lines, line)
			total += len(line) + 1
		}
		for _, idx := range rng.Perm(len(lines))[:min(findings, len(lines))] {
			lines[idx] = secret[rng.Intn(len(secret))]()
		}
		records = append(records, content.Record{
			Path: fmt.Sprintf("src/pkg%d/file%d.go", f/10, f), Body: strings.Join(lines, "\n"),
			Metadata: map[string]string{"commit_sha": "sha-1", "language": "go"},
		})
	}
	return records
}

// replayBenchSmokeWALLimit bounds the row WAL one smoke-size unchanged replay
// may write; the unguarded writer wrote megabytes for the same batch.
const replayBenchSmokeWALLimit = 64 * 1024

// changedBatchSmokeWALFloor is the least row WAL a smoke-size batch whose rows
// all changed must write: the guard must not skip a real change.
const changedBatchSmokeWALFloor = 64 * 1024

// runUpsertGuardBench writes a findings-heavy batch through the real
// ContentWriter, then replays it `rounds` times, either unchanged or with
// every file edited, and logs each replay's statement time, WAL, buffers, write
// wall, and the upsert_files stage line. By default it runs a small corpus and
// asserts a WAL bound; with ESHU_CONTENT_UPSERT_GUARD_BENCH=1 it runs the
// measurement corpus (50 files of about 469 KB, 491 findings each) and asserts
// nothing, so it can be run on each side of a change.
// ESHU_CONTENT_UPSERT_GUARD_BENCH_ROUNDS sets the replay count.
func runUpsertGuardBench(t *testing.T, changed bool) {
	t.Helper()
	full := os.Getenv("ESHU_CONTENT_UPSERT_GUARD_BENCH") == "1"
	files, sizeBytes, findings, rounds := 6, 64*1024, 60, 2
	if full {
		files, sizeBytes, findings, rounds = 50, 469*1024, 491, 3
	}
	if raw := os.Getenv("ESHU_CONTENT_UPSERT_GUARD_BENCH_ROUNDS"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 {
			t.Fatalf("ESHU_CONTENT_UPSERT_GUARD_BENCH_ROUNDS = %q, want a positive integer", raw)
		}
		rounds = parsed
	}
	ctx, db := openContentUpsertGuardDatabase(t)
	records := findingsHeavyCorpus(files, sizeBytes, findings)
	var plans []explainedFileUpsert
	var stageLog bytes.Buffer
	writer := NewContentWriter(explainingFileDB{SQLDB: SQLDB{DB: db}, plans: &plans}).
		WithLogger(slog.New(slog.NewTextHandler(&stageLog, nil)))
	clock := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	writer.Now = func() time.Time { return clock }
	mat := content.Materialization{RepoID: "repo-8915-bench", ScopeID: "scope-8915", GenerationID: "gen-1", Records: records}

	if _, err := writer.Write(ctx, mat); err != nil {
		t.Fatalf("seed Write: %v", err)
	}
	plans = nil
	var wallMS, execMS, walMB []float64
	for r := 1; r <= rounds; r++ {
		clock = clock.Add(time.Hour)
		round := records
		if changed {
			round = make([]content.Record, len(records))
			for i, rec := range records {
				rec.Body += fmt.Sprintf("\n// edit %d\n", r)
				round[i] = rec
			}
		}
		mat.Records = round
		stageLog.Reset()
		start := time.Now()
		if _, err := writer.Write(ctx, mat); err != nil {
			t.Fatalf("replay %d: %v", r, err)
		}
		wall := float64(time.Since(start).Milliseconds())
		if len(plans) != r {
			t.Fatalf("replay %d recorded %d plans, want %d (one batch statement per replay)", r, len(plans), r)
		}
		p := plans[r-1]
		wallMS, execMS, walMB = append(wallMS, wall), append(execMS, p.executionMS), append(walMB, float64(p.walBytes)/(1<<20))
		t.Logf("round %d: write wall %.0f ms, statement %.0f ms, WAL %.2f MiB, shared read %d, dirtied %d blocks",
			r, wall, p.executionMS, float64(p.walBytes)/(1<<20), p.sharedRead, p.sharedDirty)
		if full {
			for _, line := range strings.Split(stageLog.String(), "\n") {
				if strings.Contains(line, "stage=upsert_files") {
					t.Logf("round %d stage log: %s", r, line)
				}
			}
		}
		switch {
		case !full && !changed && p.walBytes > replayBenchSmokeWALLimit:
			t.Errorf("replay %d wrote %d WAL bytes for an unchanged batch, want <= %d", r, p.walBytes, replayBenchSmokeWALLimit)
		case !full && changed && p.walBytes < changedBatchSmokeWALFloor:
			t.Errorf("round %d wrote %d WAL bytes for a batch whose rows all changed, want >= %d: the guard must not skip a real change", r, p.walBytes, changedBatchSmokeWALFloor)
		}
	}
	if full {
		// The residual of write wall over statement time is client work; time the
		// reference extraction the file batch runs after its INSERT on its own.
		rows := make([]preparedFileRow, len(records))
		for i, rec := range records {
			rows[i] = preparedFileRow{repoID: mat.RepoID, path: rec.Path, body: rec.Body}
		}
		start := time.Now()
		refs := contentReferencesForFiles(rows)
		t.Logf("reference extraction alone over the corpus: %d ms, %d references", time.Since(start).Milliseconds(), len(refs))
	}
	median := func(v []float64) float64 {
		s := slices.Clone(v)
		slices.Sort(s)
		return s[len(s)/2]
	}
	t.Logf("median of %d rounds: write wall %.0f ms, statement %.0f ms, WAL %.2f MiB", rounds, median(wallMS), median(execMS), median(walMB))
}

// TestContentFileUpsertGuardReplayBenchLive reports the before/after of the
// #8915 guard for an identical replay (see runUpsertGuardBench):
//
//	ESHU_CONTENT_UPSERT_GUARD_BENCH=1 ESHU_POSTGRES_TEST_DSN=... \
//	  go test ./internal/storage/postgres -run ContentFileUpsertGuardReplayBench -count=1 -v
func TestContentFileUpsertGuardReplayBenchLive(t *testing.T) { runUpsertGuardBench(t, false) }

// TestContentFileUpsertGuardChangedBatchBenchLive reports the same numbers for a
// batch whose rows all change, the path the guard adds a compare to.
func TestContentFileUpsertGuardChangedBatchBenchLive(t *testing.T) { runUpsertGuardBench(t, true) }
