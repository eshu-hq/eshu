// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/searchbench"
	"github.com/eshu-hq/eshu/go/internal/searchhybrid"
	"github.com/eshu-hq/eshu/go/internal/searchretrieval"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	dsn := flag.String("dsn", envOr("ESHU_BENCH_DSN", "postgres://eshu:change-me@localhost:15432/eshu"), "Postgres DSN for the content corpus")
	repoFlag := flag.String("repo", "", "repo_id to benchmark (default: repository with the most entities)")
	limit := flag.Int("limit", 20, "result limit per query")
	maxDocs := flag.Int("max-docs", 50000, "maximum documents to index")
	queryCount := flag.Int("queries", 30, "number of derived queries")
	rounds := flag.Int("rounds", 3, "measurement rounds per query")
	suitePath := flag.String("suite", "", "validated searchbench QuerySuite JSON for recall/latency cap sweep")
	caps := flag.String("caps", "500,5000,20000,all", "comma-separated corpus caps for --suite mode; use all for the loaded corpus")
	queryTimeout := flag.Duration("query-timeout", 30*time.Second, "per-query timeout for --suite cap sweep")
	flag.Parse()

	if err := run(*dsn, *repoFlag, *limit, *maxDocs, *queryCount, *rounds, *suitePath, *caps, *queryTimeout); err != nil {
		fmt.Fprintln(os.Stderr, "search-bench:", err)
		os.Exit(1)
	}
}

func run(
	dsn string,
	repoID string,
	limit int,
	maxDocs int,
	queryCount int,
	rounds int,
	suitePath string,
	capsFlag string,
	queryTimeout time.Duration,
) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return fmt.Errorf("connect postgres: %w", err)
	}
	defer pool.Close()

	if repoID == "" {
		repoID, err = topRepo(ctx, pool)
		if err != nil {
			return err
		}
	}

	docs, stats, err := loadCorpus(ctx, pool, repoID, maxDocs)
	if err != nil {
		return err
	}
	if len(docs) == 0 {
		return fmt.Errorf("no curated documents for repo %q", repoID)
	}
	if strings.TrimSpace(suitePath) != "" {
		suite, err := loadQuerySuite(suitePath)
		if err != nil {
			return err
		}
		caps, err := parseCorpusCaps(capsFlag, len(docs))
		if err != nil {
			return err
		}
		results, err := runCapSweep(ctx, docs, suite, caps, queryTimeout)
		if err != nil {
			return err
		}
		printCapSweepReport(stats, suitePath, results)
		return nil
	}

	buildStart := time.Now()
	index, err := searchhybrid.NewIndex(docs, searchhybrid.Options{MaxDocuments: maxDocs})
	if err != nil {
		return fmt.Errorf("build index: %w", err)
	}
	buildDuration := time.Since(buildStart)
	backend := searchhybrid.Backend{Index: index}

	queries := deriveQueries(docs, queryCount)
	if len(queries) == 0 {
		return fmt.Errorf("no queries derived from corpus")
	}

	hybridLatency, err := measure(queries, rounds, func(term string) (int, error) {
		results, searchErr := backend.Search(ctx, searchretrieval.Request{
			Query:   term,
			Scope:   searchretrieval.Scope{RepoID: repoID},
			Mode:    searchbench.ModeKeyword,
			Limit:   limit,
			Timeout: 30 * time.Second,
		})
		return len(results), searchErr
	})
	if err != nil {
		return fmt.Errorf("measure hybrid: %w", err)
	}

	postgresLatency, err := measure(queries, rounds, func(term string) (int, error) {
		return pgKeywordSearch(ctx, pool, term, repoID, limit)
	})
	if err != nil {
		return fmt.Errorf("measure postgres: %w", err)
	}

	printReport(stats, index, buildDuration, queries, rounds, hybridLatency, postgresLatency)
	return nil
}

func printReport(
	stats corpusStats,
	index *searchhybrid.Index,
	buildDuration time.Duration,
	queries []string,
	rounds int,
	hybrid latency,
	postgres latency,
) {
	fmt.Printf("# search-bench: design-430 search-lane benchmark\n\n")
	fmt.Printf("Corpus (repo %s):\n", stats.RepoID)
	fmt.Printf("  entity rows scanned : %d\n", stats.EntityRows)
	fmt.Printf("  file rows scanned   : %d\n", stats.FileRows)
	fmt.Printf("  curated documents   : %d (indexed %d, overflow %d)\n", stats.Documents, index.Size(), index.Overflow())
	fmt.Printf("  skipped sensitive   : %d\n", stats.SkippedSensitive)
	fmt.Printf("  skipped excluded    : %d\n", stats.SkippedExcluded)
	fmt.Printf("  skipped no-handle   : %d\n", stats.SkippedNoHandle)
	fmt.Printf("  index build time    : %s\n\n", buildDuration.Round(time.Millisecond))
	fmt.Printf("Queries: %d derived terms x %d rounds\n\n", len(queries), rounds)
	fmt.Printf("%-26s %12s %12s %12s %10s\n", "backend (keyword)", "p50", "p95", "max", "hits")
	printLatency("postgres_content_search", postgres)
	printLatency("in_process_hybrid_bm25", hybrid)
	fmt.Printf("\nNote: NornicDB search-lane arm not measured (canonical NornicDB runs\n")
	fmt.Printf("search-disabled per design 430; no search-enabled curated deployment exists).\n")
	fmt.Printf("Recall/precision require a labeled query suite; this run measures latency,\n")
	fmt.Printf("index build cost, and curated corpus shape over real data.\n")
}

func printLatency(name string, l latency) {
	fmt.Printf(
		"%-26s %12s %12s %12s %10d\n",
		name,
		l.P50.Round(time.Microsecond),
		l.P95.Round(time.Microsecond),
		l.Max.Round(time.Microsecond),
		l.Results,
	)
}

func envOr(key string, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
