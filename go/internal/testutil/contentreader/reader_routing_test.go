// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contentreader_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"strings"
	"sync"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/testutil/contentreader"
)

// scanRoutedRepo runs one content-shaped query with a repository bind value
// and returns the repo column of its single row, so a routing test can assert
// which queued result answered.
func scanRoutedRepo(t *testing.T, db *sql.DB, repo string) string {
	t.Helper()

	rows, err := db.QueryContext(context.Background(), "SELECT repo_id, relative_path FROM content_files WHERE repo_id = $1", repo)
	if err != nil {
		t.Fatalf("QueryContext(repo %q) error = %v, want nil", repo, err)
	}
	defer func() {
		_ = rows.Close()
	}()

	if !rows.Next() {
		t.Fatalf("QueryContext(repo %q) returned no rows, want one", repo)
	}
	var got, path string
	if err := rows.Scan(&got, &path); err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	return got
}

// routedFileResults queues one same-shape answer per repository, routed by
// the leading repo_id bind, in helm-then-terraform queue order.
func routedFileResults() []contentreader.ReaderQueryResult {
	return []contentreader.ReaderQueryResult{
		{
			Columns:   []string{"repo_id", "relative_path"},
			Rows:      [][]driver.Value{{"repo-helm", "deploy/policy.yaml"}},
			RouteArgs: []driver.Value{"repo-helm"},
		},
		{
			Columns:   []string{"repo_id", "relative_path"},
			Rows:      [][]driver.Value{{"repo-terraform", "deploy/policy.yaml"}},
			RouteArgs: []driver.Value{"repo-terraform"},
		},
	}
}

// TestRoutedResultsAnswerByBindValuesRegardlessOfQueueOrder proves a routed
// answer is consumed by bind match, not queue position: the terraform query
// is issued first although its answer sits second in the queue.
func TestRoutedResultsAnswerByBindValuesRegardlessOfQueueOrder(t *testing.T) {
	t.Parallel()

	db := contentreader.OpenReaderTestDB(t, routedFileResults())

	if got := scanRoutedRepo(t, db, "repo-terraform"); got != "repo-terraform" {
		t.Fatalf("terraform query answered with repo %q", got)
	}
	if got := scanRoutedRepo(t, db, "repo-helm"); got != "repo-helm" {
		t.Fatalf("helm query answered with repo %q", got)
	}
}

// TestRoutedResultsAnswerConcurrentSameShapeReads proves the fan-out case the
// routing exists for: two goroutines issuing same-shape reads concurrently
// each get their own repository's rows, whichever arrives first.
func TestRoutedResultsAnswerConcurrentSameShapeReads(t *testing.T) {
	t.Parallel()

	db := contentreader.OpenReaderTestDB(t, routedFileResults())

	var wg sync.WaitGroup
	got := make([]string, 2)
	for i, repo := range []string{"repo-helm", "repo-terraform"} {
		wg.Add(1)
		go func(i int, repo string) {
			defer wg.Done()
			got[i] = scanRoutedRepo(t, db, repo)
		}(i, repo)
	}
	wg.Wait()

	if got[0] != "repo-helm" || got[1] != "repo-terraform" {
		t.Fatalf("concurrent reads answered with %q, want each query's own repo", got)
	}
}

// TestRoutedHeadRefusesPositionalConsumption proves a routed answer never
// leaks positionally: a query that matches no routed result fails loudly
// while routed answers are still queued, instead of silently taking another
// query's rows.
func TestRoutedHeadRefusesPositionalConsumption(t *testing.T) {
	t.Parallel()

	db := contentreader.OpenReaderTestDB(t, routedFileResults())

	_, err := db.QueryContext(context.Background(), "SELECT repo_id, relative_path FROM content_files WHERE repo_id = $1", "repo-other")
	if err == nil || !strings.Contains(err.Error(), "routed") {
		t.Fatalf("unmatched query error = %v, want a routing-gap failure", err)
	}
}

// TestRoutedResultsMatchIntAgainstInt64Bind proves routing tolerates driver
// conversion: a RouteArgs written as a plain int matches the int64 bind the
// driver actually delivers, the same tolerance WantArgs already applies.
func TestRoutedResultsMatchIntAgainstInt64Bind(t *testing.T) {
	t.Parallel()

	db := contentreader.OpenReaderTestDB(t, []contentreader.ReaderQueryResult{
		{
			Columns:   []string{"n"},
			Rows:      [][]driver.Value{{int64(50)}},
			RouteArgs: []driver.Value{50},
		},
	})

	rows, err := db.QueryContext(context.Background(), "SELECT n WHERE n = $1", 50)
	if err != nil {
		t.Fatalf("QueryContext(limit 50) error = %v, want nil", err)
	}
	defer func() {
		_ = rows.Close()
	}()

	if !rows.Next() {
		t.Fatalf("QueryContext(limit 50) returned no rows, want one")
	}
	var got int64
	if err := rows.Scan(&got); err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	if got != 50 {
		t.Fatalf("routed query answered with n = %d, want 50", got)
	}
}
