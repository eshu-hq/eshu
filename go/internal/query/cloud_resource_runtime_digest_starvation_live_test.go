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

	"github.com/eshu-hq/eshu/go/internal/query/supplychain"
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
	// eligibilityDecoyDigests is how many extra digests
	// TestCloudResourceRuntimeDigestBoundCountsEligibleRowsOnlyLive asks for
	// beyond the one it seeds. They exist only to push the per-digest bound down
	// to its floor.
	//
	// The bound is the 200-row page budget shared across the requested digests
	// (supplyChainCloudRuntimeProbePerDigestLimit), so asking for ONE digest
	// computes a 200-row bound. No fixture of a dozen rows can overflow that, and
	// a bound-before-filter implementation would return the eligible row anyway
	// and pass. 20 decoys put the request at 21 digests, where 200/21 = 9 falls
	// under the floor and the bound becomes 10 — small enough for the seeded
	// ineligible rows to exhaust it.
	eligibilityDecoyDigests = 20
	// eligibilityDecoyDigestOffset keeps the decoy digest numbers clear of the
	// one digest the eligibility corpus seeds rows for.
	eligibilityDecoyDigestOffset = 100
)

func TestCloudResourceRuntimeDigestPerDigestBoundPreventsStarvationLive(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_TEST_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_TEST_DSN to run the live per-digest starvation proof")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	db, _ := openRuntimeDigestFixtureDB(t, ctx, dsn)
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
		if count > supplychain.SupplyChainCloudRuntimeProbePerDigestMinResults {
			t.Fatalf("digest %s returned %d rows, want at most %d: the bound must still hold",
				digest, count, supplychain.SupplyChainCloudRuntimeProbePerDigestMinResults)
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
//
// db must come from openRuntimeDigestFixtureDB: these are ordinary tables and
// they land in that helper's private schema.
func seedRuntimeDigestStarvationCorpus(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()

	total := starvationHotDigestResources + starvationOtherDigests*20
	statements := []string{
		`CREATE TABLE ingestion_scopes (
          scope_id text PRIMARY KEY, scope_kind text NOT NULL, source_key text NOT NULL,
          active_generation_id text
        )`,
		`CREATE TABLE scope_generations (
          generation_id text PRIMARY KEY, scope_id text NOT NULL, status text NOT NULL
        )`,
		`CREATE TABLE fact_records (
          fact_id text PRIMARY KEY, scope_id text NOT NULL, generation_id text NOT NULL,
          is_tombstone boolean NOT NULL
        )`,
		`CREATE TABLE graph_node_owner (uid text PRIMARY KEY, winning_row jsonb NOT NULL)`,
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

// TestCloudResourceRuntimeDigestBoundCountsEligibleRowsOnlyLive closes the
// regression a per-digest bound introduces if it is applied in the wrong place.
//
// Bounding first and filtering after looks cheaper and is wrong: a digest whose
// first N `(arn, uid)` rows are stale, tombstoned, or outside the caller's
// grants would return NOTHING, even though a later row is current and
// authorized. That is a genuinely running vulnerable image reported as not
// running — the same class of wrong answer the per-digest bound exists to fix,
// reached from the other direction.
//
// The starvation test above cannot catch it: every row it seeds is eligible.
// This one seeds a digest whose first `perDigestLimit` rows are ALL ineligible,
// followed by one that is not, and requires the eligible row to come back.
//
// The request carries eligibilityDecoyDigests unseeded digests alongside the
// real one so the shared budget lands on its floor. Without them the bound is
// 200 and no fixture this size can reach it, which would leave the test unable
// to fail against a bound-before-filter query.
func TestCloudResourceRuntimeDigestBoundCountsEligibleRowsOnlyLive(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_TEST_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_TEST_DSN to run the live eligible-rows-only proof")
	}
	digest := starvationDigest(1)
	digests := make([]string, 0, eligibilityDecoyDigests+1)
	digests = append(digests, digest)
	for i := 1; i <= eligibilityDecoyDigests; i++ {
		digests = append(digests, starvationDigest(eligibilityDecoyDigestOffset+i))
	}

	// Guard the guard. If a future budget change lifts the per-digest bound above
	// the seeded ineligible rows, this test silently stops being able to fail —
	// exactly the hole it was written to close.
	perDigestLimit := supplyChainCloudRuntimeProbePerDigestLimit(len(digests))
	ineligible := perDigestLimit + 5
	if perDigestLimit != supplychain.SupplyChainCloudRuntimeProbePerDigestMinResults {
		t.Fatalf(
			"per-digest limit for %d digests = %d, want the floor %d: the decoy digest count must drive the "+
				"bound to its floor, or the fixture cannot overflow it and the test cannot fail",
			len(digests), perDigestLimit, supplychain.SupplyChainCloudRuntimeProbePerDigestMinResults,
		)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	db, _ := openRuntimeDigestFixtureDB(t, ctx, dsn)
	seedRuntimeDigestEligibilityCorpus(t, ctx, db, ineligible)
	store := NewPostgresCloudResourceListStore(db)

	matches, err := store.CurrentAuthorizedCloudResourcesByDigest(ctx, digests, true, nil, nil)
	if err != nil {
		t.Fatalf("CurrentAuthorizedCloudResourcesByDigest() error = %v, want nil", err)
	}
	if len(matches) != 1 {
		t.Fatalf(
			"matches = %d, want exactly 1: %d ineligible rows sort BEFORE the eligible one and the per-digest "+
				"bound is %d, so a bound applied before the eligibility predicate spends the whole budget on "+
				"tombstoned rows, discards the eligible one, and reports a running image as not running",
			len(matches), ineligible, perDigestLimit,
		)
	}
	if got, want := matches[0].UID, "uid-eligible"; got != want {
		t.Fatalf("matches[0].UID = %q, want %q", got, want)
	}
	if got := matches[0].Digest; got != digest {
		t.Fatalf("matches[0].Digest = %q, want %q", got, digest)
	}
}

// seedRuntimeDigestEligibilityCorpus seeds one digest carrying `ineligible`
// tombstoned rows whose arns sort FIRST, then one current, authorized row. The
// caller passes a count above the per-digest bound so those rows alone can
// exhaust it. Ordering is what makes the test sharp: the ineligible rows are the
// ones a naive bound would consume.
//
// db must come from openRuntimeDigestFixtureDB: these are ordinary tables and
// they land in that helper's private schema.
func seedRuntimeDigestEligibilityCorpus(t *testing.T, ctx context.Context, db *sql.DB, ineligible int) {
	t.Helper()

	statements := []string{
		`CREATE TABLE ingestion_scopes (
          scope_id text PRIMARY KEY, scope_kind text NOT NULL, source_key text NOT NULL,
          active_generation_id text
        )`,
		`CREATE TABLE scope_generations (
          generation_id text PRIMARY KEY, scope_id text NOT NULL, status text NOT NULL
        )`,
		`CREATE TABLE fact_records (
          fact_id text PRIMARY KEY, scope_id text NOT NULL, generation_id text NOT NULL,
          is_tombstone boolean NOT NULL
        )`,
		`CREATE TABLE graph_node_owner (uid text PRIMARY KEY, winning_row jsonb NOT NULL)`,
		`INSERT INTO ingestion_scopes VALUES ('scope:allowed','repository','repository:allowed','generation:allowed')`,
		`INSERT INTO scope_generations VALUES ('generation:allowed','scope:allowed','active')`,
		fmt.Sprintf(`INSERT INTO fact_records
          SELECT 'fact-stale-' || lpad(value::text, 4, '0'), 'scope:allowed', 'generation:allowed', true
          FROM generate_series(1, %d) AS value`, ineligible),
		`INSERT INTO fact_records VALUES ('fact-eligible', 'scope:allowed', 'generation:allowed', false)`,
		fmt.Sprintf(`INSERT INTO graph_node_owner
          SELECT 'uid-stale-' || lpad(value::text, 4, '0'),
                 jsonb_build_object(
                   'source_fact_id', 'fact-stale-' || lpad(value::text, 4, '0'),
                   'resource_type', 'aws_ec2_instance',
                   'running_image_digest', %s,
                   'arn', 'arn:example:compute:::aaa-' || lpad(value::text, 4, '0')
                 )
          FROM generate_series(1, %d) AS value`,
			quoteLiteral(starvationDigest(1)), ineligible),
		fmt.Sprintf(`INSERT INTO graph_node_owner VALUES ('uid-eligible',
           jsonb_build_object(
             'source_fact_id', 'fact-eligible',
             'resource_type', 'aws_ec2_instance',
             'running_image_digest', %s,
             'arn', 'arn:example:compute:::zzz-0001'
           ))`, quoteLiteral(starvationDigest(1))),
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

// quoteLiteral renders a fixture string as a SQL literal. The values here are
// test-authored digests, never caller input.
func quoteLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}
