// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #9611 disposable PostgreSQL proof that the per-(package manager, name) probe texts of ListActivePackageManifestDependencyFacts return the same ordered rows as the pre-#9611 flat texts through the same paging and delta overlay path, on a delta-active and a no-delta corpus, for the plain and Maven-family reads; reads ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DSN with its disposable opt-in
//eshu:readiness:env ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DISPOSABLE ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DSN

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// manifestFlatPlainOracle and manifestFlatMavenOracle are the pre-#9611 flat
// texts, frozen byte for byte from the shipped constants at 0650a6a4d2. They
// are the parity oracle: the probe texts must return the same rows.
const manifestFlatPlainOracle = `
SELECT
    fact.fact_id,
    fact.scope_id,
    fact.generation_id,
    fact.fact_kind,
    fact.stable_fact_key,
    fact.schema_version,
    fact.collector_kind,
    fact.fencing_token,
    fact.source_confidence,
    fact.source_system,
    fact.source_fact_key,
    COALESCE(fact.source_uri, ''),
    COALESCE(fact.source_record_id, ''),
    fact.observed_at,
    fact.is_tombstone,
    fact.payload
FROM fact_records AS fact
JOIN ingestion_scopes AS scope
  ON scope.scope_id = fact.scope_id
 AND scope.active_generation_id = fact.generation_id
JOIN scope_generations AS generation
  ON generation.scope_id = fact.scope_id
 AND generation.generation_id = fact.generation_id
WHERE fact.fact_kind = 'content_entity'
  AND fact.source_system = 'git'
  AND fact.payload->>'entity_type' = 'Variable'
  AND fact.payload->'entity_metadata'->>'config_kind' = 'dependency'
  AND fact.payload->'entity_metadata'->>'package_manager' = ANY($1::text[])
  AND (LOWER(TRIM(fact.payload->>'entity_name') COLLATE "C") COLLATE "default") = ANY($2::text[])
  AND generation.status = 'active'
  AND fact.is_tombstone = FALSE
  AND (
    $3::timestamptz IS NULL
    OR (fact.observed_at, fact.fact_id) > ($3::timestamptz, $4::text)
  )
ORDER BY fact.observed_at ASC, fact.fact_id ASC
LIMIT $5
`

const manifestFlatMavenOracle = `
SELECT
    u.fact_id, u.scope_id, u.generation_id, u.fact_kind, u.stable_fact_key,
    u.schema_version, u.collector_kind, u.fencing_token, u.source_confidence,
    u.source_system, u.source_fact_key, u.source_uri, u.source_record_id,
    u.observed_at, u.is_tombstone, u.payload
FROM (
  (SELECT
      fact.fact_id,
      fact.scope_id,
      fact.generation_id,
      fact.fact_kind,
      fact.stable_fact_key,
      fact.schema_version,
      fact.collector_kind,
      fact.fencing_token,
      fact.source_confidence,
      fact.source_system,
      fact.source_fact_key,
      COALESCE(fact.source_uri, '') AS source_uri,
      COALESCE(fact.source_record_id, '') AS source_record_id,
      fact.observed_at,
      fact.is_tombstone,
      fact.payload
  FROM fact_records AS fact
  JOIN ingestion_scopes AS scope
    ON scope.scope_id = fact.scope_id
   AND scope.active_generation_id = fact.generation_id
  JOIN scope_generations AS generation
    ON generation.scope_id = fact.scope_id
   AND generation.generation_id = fact.generation_id
  WHERE fact.fact_kind = 'content_entity'
    AND fact.source_system = 'git'
    AND fact.payload->>'entity_type' = 'Variable'
    AND fact.payload->'entity_metadata'->>'config_kind' = 'dependency'
    AND fact.payload->'entity_metadata'->>'package_manager' = ANY($1::text[])
    AND (LOWER(TRIM(fact.payload->>'entity_name') COLLATE "C") COLLATE "default") = ANY($2::text[])
    AND generation.status = 'active'
    AND fact.is_tombstone = FALSE
    AND (
      $3::timestamptz IS NULL
      OR (fact.observed_at, fact.fact_id) > ($3::timestamptz, $4::text)
    )
  ORDER BY fact.observed_at ASC, fact.fact_id ASC
  LIMIT $5)
  UNION ALL
  (SELECT
      fact.fact_id,
      fact.scope_id,
      fact.generation_id,
      fact.fact_kind,
      fact.stable_fact_key,
      fact.schema_version,
      fact.collector_kind,
      fact.fencing_token,
      fact.source_confidence,
      fact.source_system,
      fact.source_fact_key,
      COALESCE(fact.source_uri, '') AS source_uri,
      COALESCE(fact.source_record_id, '') AS source_record_id,
      fact.observed_at,
      fact.is_tombstone,
      fact.payload
  FROM fact_records AS fact
  JOIN ingestion_scopes AS scope
    ON scope.scope_id = fact.scope_id
   AND scope.active_generation_id = fact.generation_id
  JOIN scope_generations AS generation
    ON generation.scope_id = fact.scope_id
   AND generation.generation_id = fact.generation_id
  WHERE fact.fact_kind = 'content_entity'
    AND fact.source_system = 'git'
    AND fact.payload->>'entity_type' = 'Variable'
    AND fact.payload->'entity_metadata'->>'config_kind' = 'dependency'
    AND fact.payload->'entity_metadata'->>'package_manager' = 'sbt'
    AND fact.payload->'entity_metadata'->>'sbt_binary_artifact' = ANY($2::text[])
    AND (LOWER(TRIM(fact.payload->>'entity_name') COLLATE "C") COLLATE "default") <> ALL($2::text[])
    AND generation.status = 'active'
    AND fact.is_tombstone = FALSE
    AND (
      $3::timestamptz IS NULL
      OR (fact.observed_at, fact.fact_id) > ($3::timestamptz, $4::text)
    )
  ORDER BY fact.observed_at ASC, fact.fact_id ASC
  LIMIT $5)
) AS u
ORDER BY u.observed_at ASC, u.fact_id ASC
LIMIT $5
`

// manifestParitySeed seeds 12 git scopes. Each has a superseded old full and
// a current full of 160 dependency rows sharing three observed_at values (so
// (observed_at, fact_id) ties are common): npm "hot" written as hot, Hot and
// " HOT " (folded to one name with 40 to 80 rows a scope, more than a page
// across the fleet), npm warm-N, pypi "hot" (same name, other manager),
// gradle and maven "g:a", sbt rows reached by entity_name and by
// sbt_binary_artifact (the Maven-family second branch), and every 13th row
// tombstoned. Scopes p-00 to p-02 then get an active delta over the current
// full; the test seeds their repository facts and delta rows.
const manifestParitySeed = `
INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status, payload, active_generation_id)
SELECT 'p-'||lpad(n::text,2,'0'), 'repository', 'git', 'p-'||n, 'git', 'p-'||n, now(), now(), 'active', '{}'::jsonb, 'p-'||lpad(n::text,2,'0')||'-full'
FROM generate_series(0, 11) n;
INSERT INTO scope_generations (scope_id, generation_id, trigger_kind, is_delta, source_commit_sha, observed_at, ingested_at, status, activated_at)
SELECT 'p-'||lpad(n::text,2,'0'), 'p-'||lpad(n::text,2,'0')||'-old', 'sync', false, 'o', now()-interval '3 day', now()-interval '3 day', 'superseded', now()-interval '3 day'
FROM generate_series(0, 11) n;
INSERT INTO scope_generations (scope_id, generation_id, trigger_kind, is_delta, source_commit_sha, observed_at, ingested_at, status, activated_at)
SELECT 'p-'||lpad(n::text,2,'0'), 'p-'||lpad(n::text,2,'0')||'-full', 'sync', false, 'c0', now()-interval '2 day', now()-interval '2 day', 'active', now()-interval '2 day'
FROM generate_series(0, 11) n;
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
SELECT gen.gid||'-'||lpad(i::text,3,'0'), gen.sid, gen.gid, 'content_entity', 'content_entity:'||gen.sid||':'||i, 'git', 'content_entity:'||gen.sid||':'||i,
       gen.at + (i % 3) * interval '1 second', now(), i % 13 = 0,
       jsonb_build_object('repo_id', gen.sid, 'relative_path', 'm/'||(i % 4)||'/package.json',
         'entity_type', 'Variable', 'entity_name', d.name,
         'entity_metadata', jsonb_strip_nulls(jsonb_build_object('config_kind', 'dependency', 'package_manager', d.manager,
           'sbt_binary_artifact', d.artifact, 'version', '1.'||i)))
FROM (
  SELECT 'p-'||lpad(n::text,2,'0') AS sid, 'p-'||lpad(n::text,2,'0')||g.suffix AS gid, g.at
  FROM generate_series(0, 11) n,
       (VALUES ('-old', timestamptz '2026-01-01 00:00:00+00'), ('-full', timestamptz '2026-02-01 00:00:00+00')) AS g(suffix, at)
) AS gen
CROSS JOIN generate_series(0, 159) i
CROSS JOIN LATERAL (
  SELECT CASE i % 8
           WHEN 0 THEN 'npm' WHEN 1 THEN 'npm' WHEN 2 THEN 'npm' WHEN 3 THEN 'pypi'
           WHEN 4 THEN 'gradle' WHEN 5 THEN 'maven' WHEN 6 THEN 'sbt' ELSE 'npm' END AS manager,
         CASE i % 8
           WHEN 0 THEN 'hot' WHEN 1 THEN 'Hot' WHEN 2 THEN ' HOT ' WHEN 3 THEN 'hot'
           WHEN 4 THEN 'g:a' WHEN 5 THEN 'G:A' WHEN 6 THEN (CASE WHEN i % 16 = 6 THEN 'org:lib' ELSE 'org:lib_2.13' END)
           ELSE 'warm-'||(i % 5) END AS name,
         CASE WHEN i % 16 = 6 THEN 'org:lib_2.13' END AS artifact
) AS d;
`

// manifestParityRead is one read the parity proof compares.
type manifestParityRead struct {
	name       string
	ecosystems []string
	names      []string
}

var manifestParityReads = []manifestParityRead{
	{"npm hot and warm, duplicated", []string{"npm"}, []string{"hot", "hot", "warm-1", "warm-3", "missing"}},
	{"npm and pypi", []string{"npm", "pypi"}, []string{"hot", "warm-0"}},
	{"maven family beside npm", []string{"maven", "npm"}, []string{"g:a", "org:lib_2.13", "hot"}},
	{"no match", []string{"npm"}, []string{"absent-1", "absent-2"}},
}

// TestManifestDependencyProbeTextsMatchFlatTextsLive is the #9611 parity
// proof: for every read, on a corpus with delta-active scopes and again with
// none, the shipped texts and the frozen pre-#9611 texts return the same rows
// in the same order through listManifestDependencyFactsWithQuery.
func TestManifestDependencyProbeTextsMatchFlatTextsLive(t *testing.T) {
	f := openDeltaManifestFixture(t)
	f.exec("seed parity corpus", manifestParitySeed)
	for _, sid := range []string{"p-00", "p-01", "p-02"} {
		delta := sid + "-delta"
		f.generation(sid, deltaManifestGeneration{id: delta, isDelta: true, commit: "c1", baseline: "c0", unionBase: "c0"})
		f.deltaRepository(sid, delta, []string{"m/0/package.json"}, []string{"m/1/package.json"})
		// Re-emit one base key with a new version, tombstone another base
		// key, and add a new hot row.
		f.pin(sid, delta, deltaManifestPin{factID: delta + "-reemit", key: "content_entity:" + sid + ":8", name: "hot", path: "m/0/package.json", version: "2"})
		f.pin(sid, delta, deltaManifestPin{factID: delta + "-tomb", key: "content_entity:" + sid + ":16", name: "hot", path: "m/0/package.json", version: "2", tombstone: true})
		f.pin(sid, delta, deltaManifestPin{factID: delta + "-new", key: "content_entity:" + sid + ":new", name: "Hot", path: "m/0/package.json", version: "2"})
		f.activate(sid, delta)
	}
	f.exec("analyze", "ANALYZE fact_records; ANALYZE scope_generations; ANALYZE ingestion_scopes")

	assertManifestProbeParity(t, f, "delta-active", true)
	for _, sid := range []string{"p-00", "p-01", "p-02"} {
		f.activate(sid, sid+"-full")
	}
	assertManifestProbeParity(t, f, "no delta", false)
}

func assertManifestProbeParity(t *testing.T, f *deltaManifestFixture, state string, wantBase bool) {
	t.Helper()
	sawBase, sawMultiPage, sawArtifact := false, false, false
	for _, read := range manifestParityReads {
		managers, query := activePackageManifestDependencyQuery(read.ecosystems)
		oracle := manifestFlatPlainOracle
		if query == listActivePackageManifestDependencyFactsWithSBTQuery {
			oracle = manifestFlatMavenOracle
		}
		got, _, err := f.store.listManifestDependencyFactsWithQuery(f.ctx, time.Now(), managers, query, read.names, false)
		if err != nil {
			t.Fatalf("%s %s: shipped read: %v", state, read.name, err)
		}
		want, _, err := f.store.listManifestDependencyFactsWithQuery(f.ctx, time.Now(), managers, oracle, read.names, false)
		if err != nil {
			t.Fatalf("%s %s: oracle read: %v", state, read.name, err)
		}
		if len(got) != len(want) {
			t.Fatalf("%s %s: shipped read returned %d rows, oracle %d", state, read.name, len(got), len(want))
		}
		for i := range want {
			if !manifestParityRowEqual(got[i], want[i]) {
				t.Fatalf("%s %s: row %d differs:\n got %+v\nwant %+v", state, read.name, i, got[i], want[i])
			}
			if got[i].GenerationID == "p-00-full" && state == "delta-active" {
				sawBase = true
			}
			if metadata, ok := got[i].Payload["entity_metadata"].(map[string]any); ok && metadata["sbt_binary_artifact"] != nil {
				sawArtifact = true
			}
		}
		if len(got) > listFactsByKindPageSize {
			sawMultiPage = true
		}
		t.Logf("%s %s: %d rows identical", state, read.name, len(got))
	}
	// The corpus must reach the paths the proof claims.
	if sawBase != wantBase || !sawMultiPage || !sawArtifact {
		t.Fatalf("%s: corpus coverage base=%v (want %v) multi-page=%v sbt-artifact=%v", state, sawBase, wantBase, sawMultiPage, sawArtifact)
	}
}

// manifestParityRowEqual compares every envelope field, payload included.
// Both sides come from the same scanner, so their times share a location.
func manifestParityRowEqual(a, b facts.Envelope) bool {
	return reflect.DeepEqual(a, b)
}

// manifestPlanSeed is a fleet-shaped corpus for the plan check: 400 git
// scopes, each with a superseded and an active full of 120 npm dependency
// rows over a skewed vocabulary of 4,000 names. On a handful of scopes the
// planner may drive each probe from the scopes instead, which is cheap there;
// the #9611 cost is the fleet shape.
const manifestPlanSeed = `
INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status, payload, active_generation_id)
SELECT 'q-'||n, 'repository', 'git', 'q-'||n, 'git', 'q-'||n, now(), now(), 'active', '{}'::jsonb, 'q-'||n||'-full'
FROM generate_series(1, 400) n;
INSERT INTO scope_generations (scope_id, generation_id, trigger_kind, is_delta, source_commit_sha, observed_at, ingested_at, status, activated_at)
SELECT 'q-'||n, 'q-'||n||g.suffix, 'sync', false, g.suffix, now(), now(), g.status, now()
FROM generate_series(1, 400) n, (VALUES ('-old', 'superseded'), ('-full', 'active')) AS g(suffix, status);
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, payload)
SELECT 'q-'||n||g.suffix||'-'||i, 'q-'||n, 'q-'||n||g.suffix, 'content_entity', 'q:'||n||':'||i, 'git', 'q:'||n||':'||i, now(), now(),
       jsonb_build_object('repo_id', 'q-'||n, 'entity_type', 'Variable', 'entity_name', 'pkg-'||((n * 7919 + i * 104729) % 4000 * ((n + i) % 4000) / 4000),
         'entity_metadata', jsonb_build_object('config_kind', 'dependency', 'package_manager', 'npm'))
FROM generate_series(1, 400) n, (VALUES ('-old'), ('-full')) AS g(suffix), generate_series(0, 119) i;
ANALYZE fact_records; ANALYZE scope_generations; ANALYZE ingestion_scopes;
`

// TestManifestDependencyProbePlanUsesTheFoldedIndexLive pins the #9611 plan
// shape on a fleet-shaped corpus: each requested pair is an Index Cond probe
// of the folded entity index, and the flat pre-#9611 text fails the same
// check (the seeded violation).
func TestManifestDependencyProbePlanUsesTheFoldedIndexLive(t *testing.T) {
	f := openDeltaManifestFixture(t)
	f.exec("seed plan corpus", manifestPlanSeed)
	for name, cell := range map[string]struct {
		query    string
		managers []string
	}{
		"plain":        {listActivePackageManifestDependencyFactsQuery, []string{"npm"}},
		"maven_family": {listActivePackageManifestDependencyFactsWithSBTQuery, []string{"gradle", "maven", "sbt"}},
	} {
		if violations := manifestProbePlanViolations(t, f, cell.query, cell.managers); len(violations) > 0 {
			t.Errorf("%s plan departs from the per-name probe:\n  %s", name, strings.Join(violations, "\n  "))
		}
	}
	// Seeded violation: the pre-#9611 flat text must fail the same check.
	if len(manifestProbePlanViolations(t, f, manifestFlatPlainOracle, []string{"npm"})) == 0 {
		t.Error("plan check is quiet on the flat pre-#9611 text")
	}
}

// manifestPlanNames mixes hot and rare names of the plan corpus with a
// Maven coordinate.
var manifestPlanNames = []string{"pkg-0", "pkg-1", "pkg-2", "pkg-17", "pkg-1999", "pkg-3998", "g:a"}

// manifestPlanNode is the part of an EXPLAIN (FORMAT JSON) node the plan
// check reads.
type manifestPlanNode struct {
	NodeType  string             `json:"Node Type"`
	Relation  string             `json:"Relation Name"`
	IndexName string             `json:"Index Name"`
	IndexCond string             `json:"Index Cond"`
	Filter    string             `json:"Filter"`
	Plans     []manifestPlanNode `json:"Plans"`
}

// manifestProbePlanViolations explains query with real binds through the read's
// own exec mode (a custom plan) and requires the #9611 shape: fact_records is
// read only through the folded entity index, with both the package manager and
// the folded name in its Index Cond and no entity_name Filter. A flat scan of
// the manager prefix would carry the name as a Filter. It returns every
// departure.
func manifestProbePlanViolations(t *testing.T, f *deltaManifestFixture, query string, managers []string) []string {
	t.Helper()
	rows, err := f.database.QueryContext(f.ctx, "EXPLAIN (FORMAT JSON) "+query,
		manifestDependencyQueryExecMode, managers, manifestPlanNames, nil, "", listFactsByKindPageSize)
	if err != nil {
		t.Fatalf("explain: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var raw string
	if !rows.Next() || rows.Scan(&raw) != nil {
		t.Fatalf("explain returned no plan: %v", rows.Err())
	}
	var plans []struct {
		Plan manifestPlanNode `json:"Plan"`
	}
	if err := json.Unmarshal([]byte(raw), &plans); err != nil || len(plans) != 1 {
		t.Fatalf("decode plan: %v", err)
	}
	// The Maven-family second branch reads the sbt prefix and filters the
	// folded name with <> ALL by design; only the probe is checked there.
	plain := !strings.Contains(query, "<> ALL($2::text[])")
	var violations []string
	var probes int
	var walk func(node manifestPlanNode)
	walk = func(node manifestPlanNode) {
		probe := node.IndexName == "fact_records_active_package_dependency_folded_entity_idx" &&
			strings.Contains(node.IndexCond, "package_manager") && strings.Contains(node.IndexCond, "lower(")
		if probe {
			probes++
		}
		if plain && node.Relation == "fact_records" && !probe {
			violations = append(violations, fmt.Sprintf("fact_records read by %s %q (cond %q), want only the folded-index probe", node.NodeType, node.IndexName, node.IndexCond))
		}
		if plain && node.Relation == "fact_records" && strings.Contains(node.Filter, "entity_name") {
			violations = append(violations, fmt.Sprintf("fact_records %s filters entity_name: %s", node.NodeType, node.Filter))
		}
		for _, child := range node.Plans {
			walk(child)
		}
	}
	walk(plans[0].Plan)
	if probes != 1 {
		violations = append(violations, fmt.Sprintf("%d folded-index probes on manager and name, want 1", probes))
	}
	return violations
}
