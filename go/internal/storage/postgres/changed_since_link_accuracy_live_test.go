// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	linksfreshnessstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/freshness/links"
)

// G1 of #7127 ruling 8.6: the link writer's state after each link equals the
// aggregate of the activating generation, and its link deltas equal an
// independent diff built from the shipped changedSinceClassificationCTEs.
// Two planted writers, derived from the shipped statements, must fail: one
// without the tombstone predicate and one without the indexed_at
// normalization.

// linkAccuracyFact is one fact_records row of the G1 fixture.
type linkAccuracyFact struct {
	kind, key, payload, uri string
	tombstone               bool
}

func linkAccuracyGenerations() [][]linkAccuracyFact {
	g0 := []linkAccuracyFact{
		{kind: "file", key: "file:a.go", payload: `{"sha":"1"}`, uri: "a.go"},
		{kind: "file", key: "file:b.go", payload: `{"sha":"1"}`, uri: "b.go"},
		{kind: "file", key: "file:gone.go", payload: `{"sha":"1"}`, uri: "gone.go"},
		{kind: "file", key: "file:dropped.go", payload: `{"sha":"1"}`, uri: "dropped.go"},
		{kind: "content_entity", key: "ent:a.F", payload: `{"n":"F","indexed_at":"t0"}`, uri: "a.go"},
		{kind: "content_entity", key: "ent:b.G", payload: `{"n":"G","indexed_at":"t0"}`, uri: "b.go"},
		{kind: "content_entity", key: "ent:dup", payload: `{"v":1,"indexed_at":"t0"}`, uri: "a.go"},
		{kind: "content_entity", key: "ent:dup", payload: `{"v":2,"indexed_at":"t0"}`, uri: "a.go"},
		{kind: "content_entity", key: "ent:scalar", payload: `5`, uri: "a.go"},
		{kind: "repository", key: "repo", payload: `{"n":"r"}`},
		{kind: "kind_a", key: "kindchange", payload: `{"x":1}`},
		{kind: "reducer_thing", key: "reducer_only", payload: `{"x":1}`},
	}
	g1 := []linkAccuracyFact{
		{kind: "file", key: "file:a.go", payload: `{"sha":"2"}`, uri: "a.go"},
		{kind: "file", key: "file:b.go", payload: `{"sha":"1"}`, uri: "b.go"},
		{kind: "file", key: "file:gone.go", tombstone: true, uri: "gone.go"},
		{kind: "file", key: "file:new.go", payload: `{"sha":"1"}`, uri: "new.go"},
		{kind: "file", key: "file:never.go", tombstone: true, uri: "never.go"},
		{kind: "content_entity", key: "ent:a.F", payload: `{"n":"F","indexed_at":"t1"}`, uri: "a.go"},
		{kind: "content_entity", key: "ent:b.G", payload: `{"n":"G2","indexed_at":"t1"}`, uri: "b.go"},
		{kind: "content_entity", key: "ent:dup", payload: `{"v":1,"indexed_at":"t1"}`, uri: "a.go"},
		{kind: "content_entity", key: "ent:dup", payload: `{"v":3,"indexed_at":"t1"}`, uri: "a.go"},
		{kind: "content_entity", key: "ent:scalar", payload: `5`, uri: "a.go"},
		{kind: "repository", key: "repo", payload: `{"n":"r"}`},
		{kind: "kind_b", key: "kindchange", payload: `{"x":1}`},
		{kind: "reducer_thing", key: "reducer_only", payload: `{"x":2}`},
	}
	g2 := []linkAccuracyFact{
		{kind: "file", key: "file:a.go", payload: `{"sha":"2"}`, uri: "a.go"},
		{kind: "file", key: "file:a.go", tombstone: true, uri: "a.go"},
		{kind: "file", key: "file:b.go", payload: `{"sha":"3"}`, uri: "b.go"},
		{kind: "file", key: "file:new.go", payload: `{"sha":"1"}`, uri: "new.go"},
		{kind: "file", key: "file:gone.go", payload: `{"sha":"back"}`, uri: "gone.go"},
		{kind: "content_entity", key: "ent:a.F", payload: `{"n":"F","indexed_at":"t2"}`, uri: "a.go"},
		{kind: "content_entity", key: "ent:dup", payload: `{"v":1,"indexed_at":"t2"}`, uri: "a.go"},
		{kind: "content_entity", key: "ent:dup", payload: `{"v":1,"indexed_at":"t2"}`, uri: "a.go"},
		{kind: "content_entity", key: "ent:dup", payload: `{"v":3,"indexed_at":"t2"}`, uri: "a.go"},
		{kind: "content_entity", key: "ent:scalar", payload: `6`, uri: "a.go"},
		{kind: "repository", key: "repo", payload: `{"n":"r"}`},
		{kind: "kind_b", key: "kindchange", payload: `{"x":1}`},
	}
	return [][]linkAccuracyFact{g0, g1, g2}
}

func openLinkAccuracyDB(t *testing.T) (context.Context, *sql.DB) {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_TEST_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_TEST_DSN to run the changed-since link accuracy proof")
	}
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("random: %v", err)
	}
	name := "cs7127_g1_" + hex.EncodeToString(b)
	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open admin: %v", err)
	}
	t.Cleanup(func() { _ = admin.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	t.Cleanup(cancel)
	if _, err := admin.ExecContext(ctx, "CREATE DATABASE "+name); err != nil {
		t.Fatalf("create database: %v", err)
	}
	t.Cleanup(func() {
		_, _ = admin.ExecContext(context.Background(), "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
	})
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse dsn: %v", err)
	}
	u.Path = "/" + name
	database, err := sql.Open("pgx", u.String())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := ApplyBootstrap(ctx, SQLDB{DB: database}); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	return ctx, database
}

func seedLinkAccuracyScope(t *testing.T, ctx context.Context, database *sql.DB, scopeID string) {
	t.Helper()
	epoch := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	mustExec := func(query string, args ...any) {
		t.Helper()
		if _, err := database.ExecContext(ctx, query, args...); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	mustExec(`INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind,
        partition_key, observed_at, ingested_at, status) VALUES ($1, 'repository', 'git', $1, 'git', $1, $2, $2, 'active')`,
		scopeID, epoch)
	for gi, facts := range linkAccuracyGenerations() {
		generationID := fmt.Sprintf("%s-g%d", scopeID, gi)
		mustExec(`INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, is_delta, observed_at,
            ingested_at, status, activated_at) VALUES ($1, $2, 'snapshot', FALSE, $3, $3, 'superseded', $3)`,
			generationID, scopeID, epoch.Add(time.Duration(gi)*time.Hour))
		for fi, f := range facts {
			var uri any
			if f.uri != "" {
				uri = f.uri
			}
			payload := f.payload
			if payload == "" {
				payload = "{}"
			}
			mustExec(`INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
                source_system, source_fact_key, source_uri, observed_at, ingested_at, is_tombstone, payload)
                VALUES ($1, $2, $3, $4, $5, 'git', $5, $6, $7, $7, $8, $9::jsonb)`,
				fmt.Sprintf("%s/%d", generationID, fi), scopeID, generationID, f.kind, f.key, uri, epoch, f.tombstone, payload)
		}
	}
}

func linkAccuracyLines(t *testing.T, ctx context.Context, database *sql.DB, query string, args ...any) []string {
	t.Helper()
	rows, err := database.QueryContext(ctx, query, args...)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scan: %v", err)
		}
		out = append(out, line)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	return out
}

func linkAccuracyDigest(lines []string) string {
	sum := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	return hex.EncodeToString(sum[:])
}

// linkAccuracyMismatches compares one link (prior -> current) of scopeID with
// the oracles and returns the mismatches it found.
func linkAccuracyMismatches(t *testing.T, ctx context.Context, database *sql.DB, scopeID, prior, current string) []string {
	t.Helper()
	var mismatches []string
	stateLines := linkAccuracyLines(t, ctx, database, `
SELECT fact_category || '|' || stable_fact_key || '|' || fact_kind || '|' || encode(state, 'hex')
FROM changed_since_key_state WHERE scope_id = $1 ORDER BY 1`, scopeID)
	aggregateLines := linkAccuracyLines(t, ctx, database, `
SELECT cat || '|' || stable_fact_key || '|' || MIN(fact_kind) || '|'
       || encode(sha256(decode(array_to_string(array_agg(encode(h, 'hex') ORDER BY h), ''), 'hex')), 'hex')
FROM (
    SELECT CASE fact_kind WHEN 'file' THEN 'files' WHEN 'content_entity' THEN 'content_entities' ELSE 'facts' END AS cat,
           stable_fact_key, fact_kind,
           sha256(convert_to((`+changedSincePayloadDigestInput+`)::text, 'UTF8')) AS h
    FROM fact_records
    WHERE scope_id = $1 AND generation_id = $2 AND is_tombstone = FALSE AND `+changedSinceExcludeReducerDerivedKinds+`
) AS rows
GROUP BY cat, stable_fact_key
ORDER BY 1`, scopeID, current)
	if linkAccuracyDigest(stateLines) != linkAccuracyDigest(aggregateLines) {
		mismatches = append(mismatches, fmt.Sprintf("state at %s: %v, aggregate: %v", current, stateLines, aggregateLines))
	}
	if prior == "" {
		return mismatches
	}
	oracle := linkAccuracyLines(t, ctx, database, changedSinceClassificationCTEs+`
SELECT fact_category || '|' || stable_fact_key || '|' || classification || '|' || fact_kind
FROM classified WHERE classification <> 'unchanged' ORDER BY 1`, scopeID, prior, current)
	deltas := linkAccuracyLines(t, ctx, database, `
SELECT fact_category || '|' || stable_fact_key || '|' || classification || '|' || COALESCE(current_fact_kind, prior_fact_kind)
FROM changed_since_link_deltas
WHERE scope_id = $1 AND generation_id = $2 AND prior_generation_id = $3
  AND classification NOT IN ('unchanged', 'dropped')
ORDER BY 1`, scopeID, current, prior)
	if linkAccuracyDigest(oracle) != linkAccuracyDigest(deltas) {
		mismatches = append(mismatches, fmt.Sprintf("deltas %s->%s: %v, oracle: %v", prior, current, deltas, oracle))
	}
	oracleUnchanged := linkAccuracyLines(t, ctx, database, changedSinceClassificationCTEs+`
SELECT fact_category || '=' || count(*) FROM classified WHERE classification = 'unchanged'
GROUP BY fact_category ORDER BY 1`, scopeID, prior, current)
	linkUnchanged := linkAccuracyLines(t, ctx, database, `
SELECT category || '=' || keys - changed FROM (
    SELECT c.category, c.keys,
           COALESCE((SELECT sum(key_count) FROM changed_since_link_bucket_counts AS b
                     WHERE b.scope_id = $1 AND b.generation_id = $2 AND b.prior_generation_id = $3
                       AND b.fact_category = c.category AND b.classification IN ('added', 'updated')), 0) AS changed
    FROM changed_since_links AS l,
         LATERAL (VALUES ('content_entities', l.content_entities_keys), ('facts', l.facts_keys), ('files', l.files_keys))
             AS c(category, keys)
    WHERE l.scope_id = $1 AND l.generation_id = $2 AND l.prior_generation_id = $3
) AS counted WHERE keys - changed > 0 ORDER BY 1`, scopeID, current, prior)
	if linkAccuracyDigest(oracleUnchanged) != linkAccuracyDigest(linkUnchanged) {
		mismatches = append(mismatches, fmt.Sprintf("unchanged %s->%s: %v, oracle: %v", prior, current, linkUnchanged, oracleUnchanged))
	}
	return mismatches
}

// runPlantedLinks runs a planted root and incremental statement pair
// directly, the way LinkWriter runs the shipped pair.
func runPlantedLinks(t *testing.T, ctx context.Context, database *sql.DB, scopeID, rootSQL, incrementalSQL string) {
	t.Helper()
	now := time.Now().UTC()
	if _, err := database.ExecContext(ctx, rootSQL, scopeID, scopeID+"-g0", linksfreshnessstore.DigestVersion, now); err != nil {
		t.Fatalf("planted root: %v", err)
	}
	for gi := 1; gi <= 2; gi++ {
		prior, current := fmt.Sprintf("%s-g%d", scopeID, gi-1), fmt.Sprintf("%s-g%d", scopeID, gi)
		if _, err := database.ExecContext(ctx, incrementalSQL, scopeID, current, prior, linksfreshnessstore.DigestVersion, now); err != nil {
			t.Fatalf("planted incremental %s: %v", current, err)
		}
	}
}

func plant(t *testing.T, statement, old, replacement string) string {
	t.Helper()
	if !strings.Contains(statement, old) {
		t.Fatalf("planted writer anchor %q is not in the shipped statement", old)
	}
	return strings.ReplaceAll(statement, old, replacement)
}

func TestChangedSinceLinkAccuracyAgainstClassificationOracle(t *testing.T) {
	ctx, database := openLinkAccuracyDB(t)

	// GREEN: the shipped writer, through LinkWriter.
	const green = "g1-green"
	seedLinkAccuracyScope(t, ctx, database, green)
	for gi := 0; gi <= 2; gi++ {
		if _, err := database.ExecContext(ctx, `INSERT INTO changed_since_activations
            (scope_id, generation_id, prior_generation_id, source, activated_at) VALUES ($1, $2, NULL, 'backfill', now())`,
			green, fmt.Sprintf("%s-g%d", green, gi)); err != nil {
			t.Fatalf("journal: %v", err)
		}
	}
	writer := linksfreshnessstore.NewLinkWriter(SQLDB{DB: database})
	for gi := 0; gi <= 2; gi++ {
		result, err := writer.LinkNext(ctx, green)
		if err != nil {
			t.Fatalf("link %d: %v", gi, err)
		}
		prior := ""
		if gi > 0 {
			prior = fmt.Sprintf("%s-g%d", green, gi-1)
		}
		if result.PriorGenerationID != prior {
			t.Fatalf("link %d prior = %q, want %q", gi, result.PriorGenerationID, prior)
		}
		if mismatches := linkAccuracyMismatches(t, ctx, database, green, prior, fmt.Sprintf("%s-g%d", green, gi)); len(mismatches) > 0 {
			t.Fatalf("shipped writer disagrees with the oracles:\n%s", strings.Join(mismatches, "\n"))
		}
	}

	// RED: planted writers must disagree with the oracles.
	noTombstone := func(statement string) string {
		statement = plant(t, statement, ` FILTER (WHERE NOT is_tombstone)`, ``)
		return plant(t, statement, `CASE WHEN is_tombstone THEN NULL
                    ELSE sha256(convert_to((`+linksfreshnessstore.PayloadDigestInput+`)::text, 'UTF8')) END`,
			`sha256(convert_to((`+linksfreshnessstore.PayloadDigestInput+`)::text, 'UTF8'))`)
	}
	noNormalization := func(statement string) string {
		return plant(t, statement, linksfreshnessstore.PayloadDigestInput, `payload`)
	}
	for name, planted := range map[string]func(string) string{
		"without-tombstone-predicate": noTombstone,
		"without-indexed-at":          noNormalization,
	} {
		scopeID := "g1-red-" + name
		seedLinkAccuracyScope(t, ctx, database, scopeID)
		runPlantedLinks(t, ctx, database, scopeID, planted(linksfreshnessstore.RootLinkSQL), planted(linksfreshnessstore.IncrementalLinkSQL))
		var found []string
		for gi := 0; gi <= 2; gi++ {
			prior := ""
			if gi > 0 {
				prior = fmt.Sprintf("%s-g%d", scopeID, gi-1)
			}
			found = append(found, linkAccuracyMismatches(t, ctx, database, scopeID, prior, fmt.Sprintf("%s-g%d", scopeID, gi))...)
		}
		if len(found) == 0 {
			t.Fatalf("planted writer %s passed the oracles; the gate cannot see that defect", name)
		}
		t.Logf("planted writer %s: %d oracle mismatches (RED as required)", name, len(found))
	}
}
