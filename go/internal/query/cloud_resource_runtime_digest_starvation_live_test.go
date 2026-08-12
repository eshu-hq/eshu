// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build integration

package query

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// The #5789 starvation regression, at scale, against real rows.
//
// The runtime probe promotes a supply-chain finding to
// deployment_truth_tier=runtime_confirmed when a current, authorized
// CloudResource runs the finding's subject digest. It used to bound the
// owner-ledger read with ONE total-row cap across every digest on the page,
// applied after ORDER BY (digest, arn, uid).
//
// A total cap does not share. One widely-deployed image spends the whole budget
// before the ordered scan reaches any other digest, so every other finding on
// that page silently keeps its CI-declared tier. It is not a truncated answer,
// it is a missing one — and it is invisible, because a finding with no runtime
// evidence looks exactly like a finding whose image runs nowhere.
//
// Measured on this corpus before the fix: 1 digest represented out of 21.

const (
	// starvationHotDigestResources is deliberately far above the old 200-row
	// total cap, so the hot digest alone could exhaust it.
	starvationHotDigestResources = 600
	// starvationOtherDigests are the findings that used to be starved.
	starvationOtherDigests = 20
)

func TestCloudResourceRuntimeDigestPerDigestBoundPreventsStarvationLive(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_TEST_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_TEST_DSN to run the live per-digest starvation proof")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open Postgres: %v", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)
	db.SetConnMaxIdleTime(0)
	t.Cleanup(func() { _ = db.Close() })

	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	seedRuntimeDigestStarvationCorpus(t, ctx, db)
	store := NewPostgresCloudResourceListStore(db)

	digests := make([]string, 0, starvationOtherDigests+1)
	digests = append(digests, starvationDigest(0))
	for i := 1; i <= starvationOtherDigests; i++ {
		digests = append(digests, starvationDigest(i))
	}

	matches, err := store.CurrentAuthorizedCloudResourcesByDigest(ctx, digests, true, nil, nil)
	if err != nil {
		t.Fatalf("CurrentAuthorizedCloudResourcesByDigest() error = %v, want nil", err)
	}

	perDigest := map[string]int{}
	for _, match := range matches {
		perDigest[match.Digest]++
	}

	// Every requested digest must be represented. This is the assertion the old
	// global cap fails: it returned rows for the hot digest only.
	var starved []string
	for _, digest := range digests {
		if perDigest[digest] == 0 {
			starved = append(starved, digest)
		}
	}
	if len(starved) > 0 {
		sort.Strings(starved)
		t.Fatalf(
			"%d of %d digests got NO runtime evidence (hot digest returned %d rows): a total-row cap starves "+
				"every other finding on the page, which silently keeps its CI-declared tier\nstarved: %v",
			len(starved), len(digests), perDigest[starvationDigest(0)], starved,
		)
	}

	// Still bounded: no digest may exceed the per-digest limit, so one hot image
	// cannot widen the probe's work either.
	for digest, count := range perDigest {
		if count > supplyChainCloudRuntimeProbePerDigestMaxResults {
			t.Fatalf("digest %s returned %d rows, want at most %d: the bound must still hold",
				digest, count, supplyChainCloudRuntimeProbePerDigestMaxResults)
		}
	}

	// Deterministic: a security evidence field must not vary run to run. The
	// same call twice must return the same uids in the same order.
	repeat, err := store.CurrentAuthorizedCloudResourcesByDigest(ctx, digests, true, nil, nil)
	if err != nil {
		t.Fatalf("CurrentAuthorizedCloudResourcesByDigest() repeat error = %v, want nil", err)
	}
	if len(repeat) != len(matches) {
		t.Fatalf("repeat returned %d rows, first call returned %d: the bound is not deterministic", len(repeat), len(matches))
	}
	for i := range matches {
		if repeat[i] != matches[i] {
			t.Fatalf("repeat row %d = %+v, first call = %+v: ordering must be reproducible", i, repeat[i], matches[i])
		}
	}
}

// starvationDigest renders digest i as a valid-shaped sha256 reference.
func starvationDigest(i int) string {
	return fmt.Sprintf("sha256:%064d", i)
}

// seedRuntimeDigestStarvationCorpus builds the skewed shape the issue
// describes: one digest on a large fleet, plus many ordinary digests. All rows
// are current, authorized, and non-tombstoned, so freshness and authorization
// cannot be what excludes anything — only the candidate bound can.
func seedRuntimeDigestStarvationCorpus(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()

	total := starvationHotDigestResources + starvationOtherDigests*20
	statements := []string{
		`CREATE TEMP TABLE ingestion_scopes (
          scope_id text PRIMARY KEY, scope_kind text NOT NULL, source_key text NOT NULL,
          active_generation_id text
        )`,
		`CREATE TEMP TABLE scope_generations (
          generation_id text PRIMARY KEY, scope_id text NOT NULL, status text NOT NULL
        )`,
		`CREATE TEMP TABLE fact_records (
          fact_id text PRIMARY KEY, scope_id text NOT NULL, generation_id text NOT NULL,
          is_tombstone boolean NOT NULL
        )`,
		`CREATE TEMP TABLE graph_node_owner (uid text PRIMARY KEY, winning_row jsonb NOT NULL)`,
		`INSERT INTO ingestion_scopes VALUES ('scope:allowed','repository','repository:allowed','generation:allowed')`,
		`INSERT INTO scope_generations VALUES ('generation:allowed','scope:allowed','active')`,
		fmt.Sprintf(`INSERT INTO fact_records
          SELECT 'fact-' || lpad(value::text, 6, '0'), 'scope:allowed', 'generation:allowed', false
          FROM generate_series(1, %d) AS value`, total),
		fmt.Sprintf(`INSERT INTO graph_node_owner
          SELECT 'uid-' || lpad(value::text, 6, '0'),
                 jsonb_build_object(
                   'source_fact_id', 'fact-' || lpad(value::text, 6, '0'),
                   'resource_type', 'aws_ec2_instance',
                   'running_image_digest',
                     CASE WHEN value <= %d
                          THEN 'sha256:' || lpad('0', 64, '0')
                          ELSE 'sha256:' || lpad((((value - %d - 1) %% %d) + 1)::text, 64, '0') END,
                   'arn', 'arn:example:compute:::resource/' || lpad(value::text, 6, '0')
                 )
          FROM generate_series(1, %d) AS value`,
			starvationHotDigestResources, starvationHotDigestResources, starvationOtherDigests, total),
		`CREATE INDEX graph_node_owner_cloud_resource_runtime_digest_idx
           ON graph_node_owner (((winning_row->>'running_image_digest')), ((winning_row->>'arn')), uid)
           WHERE winning_row->>'resource_type' IS NOT NULL
             AND NULLIF(BTRIM(winning_row->>'running_image_digest'), '') IS NOT NULL
             AND NULLIF(BTRIM(winning_row->>'arn'), '') IS NOT NULL`,
		`ANALYZE ingestion_scopes`,
		`ANALYZE scope_generations`,
		`ANALYZE fact_records`,
		`ANALYZE graph_node_owner`,
	}
	for i, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatalf("seed statement %d: %v\n%s", i, err, statement)
		}
	}
}
