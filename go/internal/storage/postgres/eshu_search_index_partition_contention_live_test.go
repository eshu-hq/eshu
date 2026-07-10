// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"testing"
	"time"
)

func TestEshuSearchIndexTermCopyPartitionedParentContentionLive(t *testing.T) {
	db, ctx := openSearchIndexPartitionProofDB(t)
	db.SetMaxOpenConns(24)
	db.SetMaxIdleConns(24)

	table := fmt.Sprintf("eshu_search_index_terms_contention_%d", time.Now().UnixNano())
	createPartitionedSearchIndexTermsProofTable(t, ctx, db, table, 64)
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), fmt.Sprintf("DROP TABLE IF EXISTS %s CASCADE", table))
	})

	const (
		writers   = 16
		docCount  = 120
		termCount = 40
	)
	start := make(chan struct{})
	results := make(chan searchIndexTermContentionResult, writers)
	var wg sync.WaitGroup
	for writerID := 0; writerID < writers; writerID++ {
		wg.Add(1)
		go func(writerID int) {
			defer wg.Done()
			<-start
			scopeID := fmt.Sprintf("scope-contention-%02d", writerID)
			docs, terms, termKeys, frequencies := searchIndexTermContentionRows(writerID, docCount, termCount)
			started := time.Now()
			copied, err := SQLDB{DB: db}.copySearchIndexTermsToTable(
				ctx,
				table,
				scopeID,
				"gen-contention",
				docs,
				terms,
				termKeys,
				frequencies,
			)
			results <- searchIndexTermContentionResult{
				scopeID:   scopeID,
				copied:    copied,
				elapsed:   time.Since(started),
				copyError: err,
			}
		}(writerID)
	}
	close(start)
	wg.Wait()
	close(results)

	var durations []time.Duration
	for result := range results {
		if result.copyError != nil {
			t.Fatalf("writer %s copy error: %v", result.scopeID, result.copyError)
		}
		if want := int64(docCount * termCount); result.copied != want {
			t.Fatalf("writer %s copied %d rows, want %d", result.scopeID, result.copied, want)
		}
		durations = append(durations, result.elapsed)
	}
	sort.Slice(durations, func(i int, j int) bool { return durations[i] < durations[j] })
	maxElapsed := durations[len(durations)-1]
	t.Logf(
		"partitioned COPY contention writers=%d rows_per_writer=%d p50=%s p95=%s max=%s",
		writers,
		docCount*termCount,
		durations[len(durations)/2],
		durations[(len(durations)*95)/100],
		maxElapsed,
	)
	if maxElapsed > time.Minute {
		t.Fatalf("partitioned COPY writer tail latency = %s, want under 1m", maxElapsed)
	}

	var totalRows int64
	if err := db.QueryRowContext(ctx, fmt.Sprintf("SELECT count(*) FROM %s", table)).Scan(&totalRows); err != nil {
		t.Fatalf("count contention rows: %v", err)
	}
	if want := int64(writers * docCount * termCount); totalRows != want {
		t.Fatalf("total rows = %d, want %d", totalRows, want)
	}

	var badScopeCounts int
	if err := db.QueryRowContext(ctx, fmt.Sprintf(`
SELECT count(*)
FROM (
    SELECT scope_id, count(*) AS rows
    FROM %s
    GROUP BY scope_id
    HAVING count(*) <> $1
) bad
`, table), docCount*termCount).Scan(&badScopeCounts); err != nil {
		t.Fatalf("check per-scope rows: %v", err)
	}
	if badScopeCounts != 0 {
		t.Fatalf("%d writer scopes had unexpected row counts", badScopeCounts)
	}
}

type searchIndexTermContentionResult struct {
	scopeID   string
	copied    int64
	elapsed   time.Duration
	copyError error
}

func searchIndexTermContentionRows(
	writerID int,
	docCount int,
	termCount int,
) ([]string, []string, []string, []int) {
	total := docCount * termCount
	documentIDs := make([]string, 0, total)
	terms := make([]string, 0, total)
	termKeys := make([]string, 0, total)
	frequencies := make([]int, 0, total)
	for doc := 0; doc < docCount; doc++ {
		for term := 0; term < termCount; term++ {
			termKey := fmt.Sprintf("writer-%02d-term-%03d", writerID, term)
			documentIDs = append(documentIDs, fmt.Sprintf("writer-%02d-doc-%04d", writerID, doc))
			terms = append(terms, termKey)
			termKeys = append(termKeys, termKey)
			frequencies = append(frequencies, 1+(term%5))
		}
	}
	return documentIDs, terms, termKeys, frequencies
}
