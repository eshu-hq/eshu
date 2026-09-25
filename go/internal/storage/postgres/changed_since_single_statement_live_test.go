// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	_ "github.com/jackc/pgx/v5/stdlib"
)

// legacyChangedSinceCountsTail and legacyChangedSinceSamplesTail freeze the two
// statement tails the request path used before #7127 collapsed the diff into one
// statement. The CTE they append to is never copied: each legacy statement is
// derived from the shipped changedSinceClassificationCTEs, so the differential
// always compares the new single statement against the old shape over the same
// classification.
const legacyChangedSinceCountsTail = `
SELECT fact_category, classification, COUNT(*) AS key_count
FROM classified
GROUP BY fact_category, classification
ORDER BY fact_category ASC, classification ASC
`

const legacyChangedSinceSamplesTail = `
SELECT stable_fact_key, fact_kind
FROM classified
WHERE fact_category = $4 AND classification = $5
ORDER BY stable_fact_key ASC
LIMIT $6
`

// countingTxQueryer runs every changed-since statement inside one fixture
// transaction and counts the round trips so a test can assert the statement
// count of a whole diff.
type countingTxQueryer struct {
	tx         *sql.Tx
	statements int
}

func (q *countingTxQueryer) QueryContext(ctx context.Context, query string, args ...any) (db.Rows, error) {
	q.statements++
	return q.tx.QueryContext(ctx, query, args...)
}

// openChangedSinceFixtureTx opens a transaction whose search_path resolves
// fact_records to a temp table holding only the columns the changed-since
// statement reads. Returned cleanup rolls the fixture back.
func openChangedSinceFixtureTx(t *testing.T) (context.Context, *sql.Tx) {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_TEST_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_TEST_DSN to run the changed-since live proof")
	}
	database, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open Postgres: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	t.Cleanup(cancel)
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin isolated fixture: %v", err)
	}
	t.Cleanup(func() { _ = tx.Rollback() })
	if _, err := tx.ExecContext(ctx, `
CREATE TEMP TABLE fact_records (
    scope_id text NOT NULL,
    generation_id text NOT NULL,
    fact_kind text NOT NULL,
    stable_fact_key text NOT NULL,
    is_tombstone boolean NOT NULL,
    payload jsonb NOT NULL
) ON COMMIT DROP;
SET LOCAL search_path = pg_temp;
`); err != nil {
		t.Fatalf("create isolated fact fixture: %v", err)
	}
	return ctx, tx
}

// changedSinceFixtureRow is one fact_records row of the differential fixture.
type changedSinceFixtureRow struct {
	scope      string
	generation string
	kind       string
	key        string
	payload    string
	tombstone  bool
}

func insertChangedSinceFixtureRows(ctx context.Context, t *testing.T, tx *sql.Tx, rows []changedSinceFixtureRow) {
	t.Helper()
	for _, row := range rows {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO fact_records (scope_id, generation_id, fact_kind, stable_fact_key, is_tombstone, payload)
VALUES ($1, $2, $3, $4, $5, $6::jsonb)`,
			row.scope, row.generation, row.kind, row.key, row.tombstone, row.payload); err != nil {
			t.Fatalf("insert %s/%s/%s: %v", row.scope, row.generation, row.key, err)
		}
	}
}

// pair adds the same key to the prior and current generations of scope s1 with
// the given payloads. An empty payload omits that side.
func pair(kind, key, priorPayload, currentPayload string) []changedSinceFixtureRow {
	var rows []changedSinceFixtureRow
	if priorPayload != "" {
		rows = append(rows, changedSinceFixtureRow{"s1", "prior", kind, key, priorPayload, false})
	}
	if currentPayload != "" {
		rows = append(rows, changedSinceFixtureRow{"s1", "current", kind, key, currentPayload, false})
	}
	return rows
}

// changedSinceDifferentialFixture seeds every category (files,
// content_entities, facts) and every classification (added, updated, unchanged,
// retired, superseded) for scope s1 between generations prior and current, plus
// noise that the diff must ignore: an older and a later generation of s1, the
// same generation ids under a different scope, prior-side tombstones, and a
// tombstone with no active row on either side.
func changedSinceDifferentialFixture() []changedSinceFixtureRow {
	var rows []changedSinceFixtureRow
	add := func(more []changedSinceFixtureRow) { rows = append(rows, more...) }

	// files: 30 unchanged (bucket larger than every tested limit), 2 updated,
	// 3 added, 1 retired (tombstoned in current), 2 superseded (gone in current).
	for i := 0; i < 30; i++ {
		add(pair("file", fmt.Sprintf("file/u%02d", i), `{"h":"same"}`, `{"h":"same"}`))
	}
	add(pair("file", "file/m01", `{"h":"a"}`, `{"h":"b"}`))
	add(pair("file", "file/m02", `{"h":"a"}`, `{"h":"c"}`))
	for i := 1; i <= 3; i++ {
		add(pair("file", fmt.Sprintf("file/a%d", i), "", `{"h":"new"}`))
	}
	add(pair("file", "file/r1", `{"h":"a"}`, ""))
	rows = append(rows, changedSinceFixtureRow{"s1", "current", "file", "file/r1", `{"h":"a"}`, true})
	add(pair("file", "file/s1", `{"h":"a"}`, ""))
	add(pair("file", "file/s2", `{"h":"a"}`, ""))

	// content_entities: 27 unchanged, exactly 25 updated (== the default limit),
	// 1 added.
	for i := 0; i < 27; i++ {
		add(pair("content_entity", fmt.Sprintf("entity/u%02d", i), `{"n":"same"}`, `{"n":"same"}`))
	}
	for i := 0; i < 25; i++ {
		add(pair("content_entity", fmt.Sprintf("entity/m%02d", i), `{"n":"a"}`, `{"n":"b"}`))
	}
	add(pair("content_entity", "entity/a01", "", `{"n":"new"}`))

	// facts: the duplicate-payload multiset cases, mixed fact kinds, ordering
	// hazards (case and underscore), retired, added, unchanged.
	add(pair("test", "fact/dup_changed", `{"v":"G"}`, `{"v":"G"}`))
	rows = append(rows,
		changedSinceFixtureRow{"s1", "prior", "test", "fact/dup_changed", `{"v":"A"}`, false},
		changedSinceFixtureRow{"s1", "current", "test", "fact/dup_changed", `{"v":"B"}`, false},
		changedSinceFixtureRow{"s1", "prior", "test", "fact/reordered", `{"v":"G"}`, false},
		changedSinceFixtureRow{"s1", "prior", "test", "fact/reordered", `{"v":"A"}`, false},
		changedSinceFixtureRow{"s1", "current", "test", "fact/reordered", `{"v":"A"}`, false},
		changedSinceFixtureRow{"s1", "current", "test", "fact/reordered", `{"v":"G"}`, false},
		changedSinceFixtureRow{"s1", "prior", "test", "fact/multiplicity", `{"v":"A"}`, false},
		changedSinceFixtureRow{"s1", "prior", "test", "fact/multiplicity", `{"v":"A"}`, false},
		changedSinceFixtureRow{"s1", "current", "test", "fact/multiplicity", `{"v":"A"}`, false},
	)
	add(pair("aws_resource", "fact/singleton", `{"v":"A"}`, `{"v":"B"}`))
	add(pair("aws_resource", "fact/ret1", `{"v":"A"}`, ""))
	rows = append(rows, changedSinceFixtureRow{"s1", "current", "aws_resource", "fact/ret1", `{"v":"A"}`, true})
	add(pair("k8s_object", "fact/ret2", `{"v":"A"}`, ""))
	rows = append(rows, changedSinceFixtureRow{"s1", "current", "k8s_object", "fact/ret2", `{"v":"A"}`, true})
	for i := 1; i <= 4; i++ {
		kind := "aws_resource"
		if i%2 == 0 {
			kind = "k8s_object"
		}
		add(pair(kind, fmt.Sprintf("fact/add%d", i), "", `{"v":"new"}`))
	}
	for i := 1; i <= 3; i++ {
		add(pair("aws_resource", fmt.Sprintf("fact/u%d", i), `{"v":"same"}`, `{"v":"same"}`))
	}
	add(pair("aws_resource", "fact/X_Upper", "", `{"v":"new"}`))
	add(pair("aws_resource", "fact/_under", "", `{"v":"new"}`))
	// A current key whose only prior row is a tombstone counts as added.
	rows = append(rows, changedSinceFixtureRow{"s1", "prior", "aws_resource", "fact/prior_tomb", `{"v":"A"}`, true})
	add(pair("aws_resource", "fact/prior_tomb", "", `{"v":"A"}`))
	// A tombstone with no active row on either side contributes nothing.
	rows = append(rows, changedSinceFixtureRow{"s1", "current", "aws_resource", "fact/tomb_only", `{"v":"A"}`, true})

	// One key in all three categories, so a category-blind ordering or join
	// would collapse them: updated file, unchanged entity, added fact.
	add(pair("file", "shared/key", `{"h":"a"}`, `{"h":"b"}`))
	add(pair("content_entity", "shared/key", `{"n":"a"}`, `{"n":"a"}`))
	add(pair("aws_resource", "shared/key", "", `{"v":"a"}`))

	// Noise the scope/generation filter must exclude.
	for _, generation := range []string{"old", "later"} {
		rows = append(rows,
			changedSinceFixtureRow{"s1", generation, "file", "file/noise-" + generation, `{"h":"x"}`, false},
			changedSinceFixtureRow{"s1", generation, "file", "file/u00", `{"h":"different"}`, false},
		)
	}
	rows = append(rows,
		changedSinceFixtureRow{"s2", "prior", "file", "file/u00", `{"h":"one"}`, false},
		changedSinceFixtureRow{"s2", "current", "file", "file/u00", `{"h":"two"}`, false},
		changedSinceFixtureRow{"s2", "current", "file", "file/other-scope-only", `{"h":"x"}`, false},
	)
	return rows
}

// legacyChangedSinceCategories reproduces the pre-#7127 request flow: one counts
// statement, then one samples statement per non-empty (category,
// classification) bucket, each re-evaluating the whole diff. It returns the
// assembled categories and the number of statements issued.
func legacyChangedSinceCategories(
	ctx context.Context,
	t *testing.T,
	queryer *countingTxQueryer,
	scopeID, prior, current string,
	sampleLimit int,
) []statuspkg.ChangedSinceCategoryDelta {
	t.Helper()
	countsQuery := changedSinceClassificationCTEs + legacyChangedSinceCountsTail
	samplesQuery := changedSinceClassificationCTEs + legacyChangedSinceSamplesTail

	rows, err := queryer.QueryContext(ctx, countsQuery, scopeID, prior, current)
	if err != nil {
		t.Fatalf("legacy counts: %v", err)
	}
	counts := map[statuspkg.ChangedSinceCategory]statuspkg.ChangedSinceCounts{}
	for rows.Next() {
		var category, classification string
		var keyCount int64
		if err := rows.Scan(&category, &classification, &keyCount); err != nil {
			t.Fatalf("legacy counts scan: %v", err)
		}
		bucket := counts[statuspkg.ChangedSinceCategory(category)]
		applyChangedSinceCount(&bucket, classification, int(keyCount))
		counts[statuspkg.ChangedSinceCategory(category)] = bucket
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("legacy counts rows: %v", err)
	}
	_ = rows.Close()

	categories := make([]statuspkg.ChangedSinceCategoryDelta, 0, len(statuspkg.ChangedSinceCategories))
	for _, category := range statuspkg.ChangedSinceCategories {
		delta := statuspkg.ChangedSinceCategoryDelta{Category: category, Counts: counts[category]}
		samples := map[statuspkg.ChangedSinceClassification][]statuspkg.ChangedSinceSample{}
		truncated := map[statuspkg.ChangedSinceClassification]bool{}
		for _, classification := range statuspkg.ChangedSinceClassifications {
			if changedSinceClassificationCount(counts[category], classification) == 0 {
				continue
			}
			sampleRows, err := queryer.QueryContext(
				ctx, samplesQuery, scopeID, prior, current, string(category), string(classification), sampleLimit+1,
			)
			if err != nil {
				t.Fatalf("legacy samples: %v", err)
			}
			bucket := make([]statuspkg.ChangedSinceSample, 0, sampleLimit)
			for sampleRows.Next() {
				var sample statuspkg.ChangedSinceSample
				if err := sampleRows.Scan(&sample.StableFactKey, &sample.FactKind); err != nil {
					t.Fatalf("legacy samples scan: %v", err)
				}
				bucket = append(bucket, sample)
			}
			if err := sampleRows.Err(); err != nil {
				t.Fatalf("legacy samples rows: %v", err)
			}
			_ = sampleRows.Close()
			if len(bucket) > sampleLimit {
				bucket = bucket[:sampleLimit]
				truncated[classification] = true
			}
			if len(bucket) > 0 {
				samples[classification] = bucket
			}
		}
		if len(samples) > 0 {
			delta.Samples = samples
		}
		if len(truncated) > 0 {
			delta.Truncated = truncated
		}
		categories = append(categories, delta)
	}
	return categories
}

// TestChangedSinceSingleStatementMatchesLegacyFlowLive is the #7127 PR-1
// differential: the single-statement diff returns exactly the counts and
// ordered bounded samples of the old counts-plus-per-bucket flow, across every
// category and classification, for sample limits below, equal to, and above
// bucket sizes, and it does so in one round trip.
func TestChangedSinceSingleStatementMatchesLegacyFlowLive(t *testing.T) {
	ctx, tx := openChangedSinceFixtureTx(t)
	insertChangedSinceFixtureRows(ctx, t, tx, changedSinceDifferentialFixture())

	// The derivation guard: both statement shapes must be built on the shipped
	// CTE constant, never a hand copy of it.
	for name, query := range map[string]string{
		"single statement": changedSinceDeltaQuery,
		"legacy counts":    changedSinceClassificationCTEs + legacyChangedSinceCountsTail,
		"legacy samples":   changedSinceClassificationCTEs + legacyChangedSinceSamplesTail,
	} {
		if !strings.HasPrefix(query, changedSinceClassificationCTEs) {
			t.Fatalf("%s is not derived from changedSinceClassificationCTEs", name)
		}
	}

	sawTruncated := false
	for _, limit := range []int{1, 2, 25, 26, 28, 200} {
		t.Run(fmt.Sprintf("limit_%d", limit), func(t *testing.T) {
			legacyQueryer := &countingTxQueryer{tx: tx}
			want := legacyChangedSinceCategories(ctx, t, legacyQueryer, "s1", "prior", "current", limit)

			newQueryer := &countingTxQueryer{tx: tx}
			got, err := StatusStore{queryer: newQueryer}.changedSinceCategories(ctx, "s1", "prior", "current", limit)
			if err != nil {
				t.Fatalf("changedSinceCategories: %v", err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("single statement diverges from legacy flow at limit %d\n got: %+v\nwant: %+v", limit, got, want)
			}
			if newQueryer.statements != 1 {
				t.Fatalf("single-statement diff issued %d statements, want 1", newQueryer.statements)
			}
			if legacyQueryer.statements < 2 {
				t.Fatalf("legacy flow issued %d statements; the statement counter is not measuring the flow", legacyQueryer.statements)
			}
			for _, category := range got {
				if len(category.Truncated) > 0 {
					sawTruncated = true
				}
			}
		})
	}
	if !sawTruncated {
		t.Fatal("fixture never exercised a truncated bucket")
	}

	// Pin the fixture's intent so a fixture edit cannot make the differential
	// vacuous: every classification must be non-empty somewhere.
	got, err := StatusStore{queryer: &countingTxQueryer{tx: tx}}.changedSinceCategories(ctx, "s1", "prior", "current", 25)
	if err != nil {
		t.Fatalf("changedSinceCategories: %v", err)
	}
	var total statuspkg.ChangedSinceCounts
	for _, category := range got {
		total.Added += category.Counts.Added
		total.Updated += category.Counts.Updated
		total.Unchanged += category.Counts.Unchanged
		total.Retired += category.Counts.Retired
		total.Superseded += category.Counts.Superseded
	}
	if total.Added == 0 || total.Updated == 0 || total.Unchanged == 0 || total.Retired == 0 || total.Superseded == 0 {
		t.Fatalf("fixture is missing a classification: %+v", total)
	}
}
