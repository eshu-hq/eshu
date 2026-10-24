// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8915 opt-in (ESHU_CONTENT_UPSERT_GUARD_BENCH=1) benchmark of a findings-heavy content_files replay through the real ContentWriter under EXPLAIN ANALYZE; asserts nothing and skips in the runner unless opted in; enrolled so every live test file is registered; bridges the family ESHU_GENERATION_RETENTION_PROOF_DSN onto ESHU_POSTGRES_TEST_DSN as the retention proofs do
//eshu:readiness:env ESHU_AWS_DRIFT_ARN_PROOF_DISPOSABLE ESHU_AWS_DRIFT_ARN_PROOF_DSN ESHU_DEFERRED_PARTITION_PROOF_DISPOSABLE ESHU_DEFERRED_PARTITION_PROOF_DSN ESHU_GENERATION_RETENTION_PROOF_DISPOSABLE ESHU_GENERATION_RETENTION_PROOF_DSN ESHU_PROJECTOR_CLAIM_DEADLOCK_PROOF_DSN ESHU_REDUCER_FAIRNESS_PROOF_DSN

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
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

// TestContentFileUpsertGuardReplayBenchLive reports the before/after of the
// #8915 guard on the findings-heavy corpus: it writes 50 files of about 469 KB
// with 491 findings each through the real ContentWriter, then replays the
// identical batch and reports each replay's EXPLAIN (ANALYZE, BUFFERS, WAL)
// numbers. It asserts nothing; run it on each side of a change and compare.
//
//	ESHU_CONTENT_UPSERT_GUARD_BENCH=1 ESHU_POSTGRES_TEST_DSN=... \
//	  go test ./internal/storage/postgres -run ContentFileUpsertGuardReplayBench -count=1 -v
//
// ESHU_CONTENT_UPSERT_GUARD_BENCH_ROUNDS sets the replay count (default 3).
func TestContentFileUpsertGuardReplayBenchLive(t *testing.T) {
	if os.Getenv("ESHU_CONTENT_UPSERT_GUARD_BENCH") != "1" {
		t.Skip("set ESHU_CONTENT_UPSERT_GUARD_BENCH=1 to run the #8915 replay benchmark")
	}
	rounds := 3
	if raw := os.Getenv("ESHU_CONTENT_UPSERT_GUARD_BENCH_ROUNDS"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 {
			t.Fatalf("ESHU_CONTENT_UPSERT_GUARD_BENCH_ROUNDS = %q, want a positive integer", raw)
		}
		rounds = parsed
	}
	ctx, db := openContentUpsertGuardDatabase(t)
	records := findingsHeavyCorpus(50, 469*1024, 491)
	var plans []explainedFileUpsert
	writer := NewContentWriter(explainingFileDB{SQLDB: SQLDB{DB: db}, plans: &plans})
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
		t.Logf("replay %d: write wall %.0f ms, statement %.0f ms, WAL %.2f MiB, shared read %d, dirtied %d blocks",
			r, wall, p.executionMS, float64(p.walBytes)/(1<<20), p.sharedRead, p.sharedDirty)
	}
	median := func(v []float64) float64 {
		s := slices.Clone(v)
		slices.Sort(s)
		return s[len(s)/2]
	}
	t.Logf("median of %d replays: write wall %.0f ms, statement %.0f ms, WAL %.2f MiB", rounds, median(wallMS), median(execMS), median(walMB))
}
