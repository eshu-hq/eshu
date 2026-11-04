// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build perf9991_manifest_delta

package postgres

// Opt-in #9991 measurement harness for ListActivePackageManifestDependencyFacts
// (build tag perf9991_manifest_delta; not a ledger live test, and no CI runner
// passes the tag) on a delta-active corpus and on the same corpus with every
// full active. It
// counts every statement, inside and outside the read's repeatable-read
// transaction, by statement kind, counts BEGIN/COMMIT round trips apart, and
// reports how many delta-active scopes the read reached. Run:
//
//	ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DISPOSABLE=1 \
//	ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DSN='postgres://...:port/postgres?sslmode=disable' \
//	go test -tags perf9991_manifest_delta ./internal/storage/postgres \
//	  -run TestManifestDeltaReadStatementCountsLive -count=1 -v
//
// Results: docs/internal/evidence/9991-manifest-read-delta-union.md.

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

const manifestDeltaPerfSeed = `
INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status, payload)
SELECT 's-'||lpad(i::text,4,'0'), 'repository','git','s-'||i,'git','s-'||i, now(), now(), 'active','{}'::jsonb FROM generate_series(0,1999) i;
INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status, payload)
VALUES ('big','repository','git','big','git','big', now(), now(), 'active','{}'::jsonb);

INSERT INTO scope_generations (scope_id, generation_id, trigger_kind, is_delta, source_commit_sha, observed_at, ingested_at, status, activated_at)
SELECT 's-'||lpad(i::text,4,'0'), 'g-old-'||i, 'sync', false, 'o-'||i, now()-interval '2 day', now()-interval '2 day', 'superseded', now()-interval '2 day' FROM generate_series(0,1999) i;
INSERT INTO scope_generations (scope_id, generation_id, trigger_kind, is_delta, source_commit_sha, observed_at, ingested_at, status, activated_at)
SELECT 's-'||lpad(i::text,4,'0'), 'g-'||i, 'sync', false, 'c-'||i, now()-interval '1 day', now()-interval '1 day',
       CASE WHEN i >= 1800 THEN 'superseded' ELSE 'active' END, now()-interval '1 day' FROM generate_series(0,1999) i;
INSERT INTO scope_generations (scope_id, generation_id, trigger_kind, is_delta, source_commit_sha, delta_baseline_commit_sha, delta_union_base_commit_sha, observed_at, ingested_at, status, activated_at)
SELECT 's-'||lpad(i::text,4,'0'), 'd-'||i, 'sync', true, 'c2-'||i, 'c-'||i, 'c-'||i, now(), now(), 'active', now() FROM generate_series(1800,1999) i;
INSERT INTO scope_generations (scope_id, generation_id, trigger_kind, is_delta, source_commit_sha, observed_at, ingested_at, status, activated_at)
VALUES ('big','f-big','sync',false,'c0', now()-interval '1 day', now()-interval '1 day','superseded', now()-interval '1 day');
INSERT INTO scope_generations (scope_id, generation_id, trigger_kind, is_delta, source_commit_sha, delta_baseline_commit_sha, delta_union_base_commit_sha, observed_at, ingested_at, status, activated_at)
VALUES ('big','d-big','sync',true,'c1','c0','c0', now(), now(),'active', now());

UPDATE ingestion_scopes SET active_generation_id = CASE WHEN substr(scope_id,3)::int >= 1800 THEN 'd-' ELSE 'g-' END || substr(scope_id,3)::int
WHERE scope_id LIKE 's-%';
UPDATE ingestion_scopes SET active_generation_id = 'd-big' WHERE scope_id = 'big';

-- 25 pins per full generation (old and current), 5 manifests per repo.
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
SELECT gen||':'||j, sc, gen, 'content_entity', 'content_entity:'||sc||':'||j, 'git', 'content_entity:'||sc||':'||j,
       ts + (j||' ms')::interval, ts, false,
       jsonb_build_object('repo_id', sc, 'relative_path', 'm'||(j%5)||'/package.json', 'entity_type','Variable',
         'entity_name', 'pkg-'||lpad(((i*7+j)%2000)::text,4,'0'),
         'entity_metadata', jsonb_build_object('config_kind','dependency','package_manager','npm','version','1'))
FROM (SELECT i, 's-'||lpad(i::text,4,'0') AS sc, g.gen, g.ts FROM generate_series(0,1999) i,
      LATERAL (VALUES ('g-old-'||i, now()-interval '2 day' + (i||' s')::interval), ('g-'||i, now()-interval '1 day' + (i||' s')::interval)) AS g(gen, ts)) s,
     generate_series(0,24) j;
-- small deltas: re-emit the 5 pins of m0/package.json.
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
SELECT 'd-'||i||':'||j, sc, 'd-'||i, 'content_entity', 'content_entity:'||sc||':'||j, 'git', 'content_entity:'||sc||':'||j,
       now() + (j||' ms')::interval, now(), false,
       jsonb_build_object('repo_id', sc, 'relative_path', 'm0/package.json', 'entity_type','Variable',
         'entity_name', 'pkg-'||lpad(((i*7+j)%2000)::text,4,'0'),
         'entity_metadata', jsonb_build_object('config_kind','dependency','package_manager','npm','version','2'))
FROM (SELECT i, 's-'||lpad(i::text,4,'0') AS sc FROM generate_series(1800,1999) i) s, generate_series(0,24,5) j;
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
SELECT 'd-'||i||':repository', sc, 'd-'||i, 'repository', 'repository:'||sc, 'git', 'repository:'||sc, now(), now(), false,
       jsonb_build_object('repo_id', sc, 'path', '/repos/'||sc, 'delta_generation', true,
         'delta_relative_paths', jsonb_build_array('m0/package.json'), 'delta_deleted_relative_paths', '[]'::jsonb)
FROM (SELECT i, 's-'||lpad(i::text,4,'0') AS sc FROM generate_series(1800,1999) i) s;

-- big: 50,000-pin base over 500 manifests; 5,000-row delta over 50 of them.
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
SELECT 'f-big:'||j, 'big', 'f-big', 'content_entity', 'content_entity:big:'||j, 'git', 'content_entity:big:'||j,
       now()-interval '1 day' + (j||' ms')::interval, now()-interval '1 day', false,
       jsonb_build_object('repo_id','big','relative_path','m'||(j/100)||'/package.json','entity_type','Variable',
         'entity_name','pkg-'||lpad((j%2000)::text,4,'0'),
         'entity_metadata', jsonb_build_object('config_kind','dependency','package_manager','npm','version','1'))
FROM generate_series(0,49999) j;
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
SELECT 'd-big:'||j, 'big', 'd-big', 'content_entity', 'content_entity:big:'||j, 'git', 'content_entity:big:'||j,
       now() + (j||' ms')::interval, now(), false,
       jsonb_build_object('repo_id','big','relative_path','m'||(j/100)||'/package.json','entity_type','Variable',
         'entity_name','pkg-'||lpad((j%2000)::text,4,'0'),
         'entity_metadata', jsonb_build_object('config_kind','dependency','package_manager','npm','version','2'))
FROM generate_series(0,4999) j;
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
SELECT 'd-big:repository', 'big', 'd-big', 'repository', 'repository:big', 'git', 'repository:big', now(), now(), false,
       jsonb_build_object('repo_id','big','path','/repos/big','delta_generation',true,
         'delta_relative_paths', (SELECT jsonb_agg('m'||k||'/package.json') FROM generate_series(0,49) k),
         'delta_deleted_relative_paths','[]'::jsonb);
ANALYZE;
`

// manifestDeltaPerfNoDelta repoints every delta-active scope at its full.
const manifestDeltaPerfNoDelta = `
UPDATE scope_generations SET status = 'superseded' WHERE is_delta;
UPDATE scope_generations SET status = 'active'
WHERE generation_id IN (SELECT 'g-'||i FROM generate_series(1800,1999) i) OR generation_id = 'f-big';
UPDATE ingestion_scopes SET active_generation_id =
    CASE WHEN scope_id = 'big' THEN 'f-big' ELSE 'g-'||substr(scope_id,3)::int END
WHERE scope_id = 'big' OR scope_id LIKE 's-%';
ANALYZE scope_generations; ANALYZE ingestion_scopes;
`

// manifestDeltaPerfCounts tallies statements by kind.
type manifestDeltaPerfCounts struct {
	mu    sync.Mutex
	kinds map[string]int
}

func (c *manifestDeltaPerfCounts) add(kind string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.kinds[kind]++
}

func (c *manifestDeltaPerfCounts) reset() map[string]int {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := c.kinds
	c.kinds = map[string]int{}
	return out
}

func manifestDeltaPerfKind(query string) string {
	switch {
	case query == manifestDeltaTargetsQuery:
		return "target lookup"
	case query == deltaUnionBaseQuery:
		return "base lookup (per scope)"
	case query == deltaUnionBasePrunedQuery:
		return "prune probe (per scope)"
	case query == manifestDeltaOverlayInputsQuery:
		return "overlay inputs"
	case strings.Contains(query, "JOIN scope_generations AS active_delta"):
		return "base page"
	case strings.Contains(query, "scope.active_generation_id = fact.generation_id"):
		return "active page"
	default:
		return "other"
	}
}

// manifestDeltaPerfDB counts QueryContext and transaction round trips.
type manifestDeltaPerfDB struct {
	SQLDB
	counts *manifestDeltaPerfCounts
}

func (d manifestDeltaPerfDB) QueryContext(ctx context.Context, query string, args ...any) (db.Rows, error) {
	d.counts.add(manifestDeltaPerfKind(query))
	return d.SQLDB.QueryContext(ctx, query, args...)
}

func (d manifestDeltaPerfDB) BeginReadOnlyRepeatableRead(ctx context.Context) (db.Transaction, error) {
	d.counts.add("BEGIN")
	tx, err := d.SQLDB.BeginReadOnlyRepeatableRead(ctx)
	if err != nil {
		return nil, err
	}
	return manifestDeltaPerfTx{Transaction: tx, counts: d.counts}, nil
}

type manifestDeltaPerfTx struct {
	db.Transaction
	counts *manifestDeltaPerfCounts
}

func (t manifestDeltaPerfTx) QueryContext(ctx context.Context, query string, args ...any) (db.Rows, error) {
	t.counts.add(manifestDeltaPerfKind(query))
	return t.Transaction.QueryContext(ctx, query, args...)
}

func (t manifestDeltaPerfTx) Commit() error {
	t.counts.add("COMMIT")
	return t.Transaction.Commit()
}

func manifestDeltaPerfFormat(kinds map[string]int) string {
	names := make([]string, 0, len(kinds))
	total := 0
	for name, n := range kinds {
		names = append(names, name)
		if name != "BEGIN" && name != "COMMIT" {
			total += n
		}
	}
	sort.Strings(names)
	parts := []string{fmt.Sprintf("statements=%d", total)}
	for _, name := range names {
		parts = append(parts, fmt.Sprintf("%s=%d", name, kinds[name]))
	}
	return strings.Join(parts, " ")
}

// TestManifestDeltaReadStatementCountsLive seeds the #9991 corpus and logs,
// per state and ecosystem, the old read (the active pages alone) and the new
// read: rows, statements by kind, delta-active scopes reached, and the
// median wall time of five runs.
func TestManifestDeltaReadStatementCountsLive(t *testing.T) {
	dsn := os.Getenv("ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DSN")
	optIn := os.Getenv("ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DISPOSABLE")
	ctx, database := postgresproof.OpenDisposableDatabase(t, dsn, optIn, 20*time.Minute)
	if err := ApplyBootstrap(ctx, SQLDB{DB: database}); err != nil {
		t.Fatalf("ApplyBootstrap(): %v", err)
	}
	if _, err := database.ExecContext(ctx, manifestDeltaPerfSeed); err != nil {
		t.Fatalf("seed: %v", err)
	}
	counts := &manifestDeltaPerfCounts{kinds: map[string]int{}}
	store := NewFactStore(manifestDeltaPerfDB{SQLDB: SQLDB{DB: database}, counts: counts})
	names := make([]string, 0, 400)
	for i := range 400 {
		names = append(names, fmt.Sprintf("pkg-%04d", i))
	}
	measure := func(state string) {
		for _, ecosystem := range []string{"npm", "maven"} {
			managers, active := activePackageManifestDependencyQuery([]string{ecosystem})
			var oldDur, newDur []time.Duration
			var oldRows, newRows int
			var oldKinds, newKinds map[string]int
			for range 5 {
				counts.reset()
				started := time.Now()
				rows, err := store.listPackageManifestDependencyFactPages(ctx, active, managers, names)
				if err != nil {
					t.Fatal(err)
				}
				oldDur, oldRows, oldKinds = append(oldDur, time.Since(started)), len(rows), counts.reset()
				started = time.Now()
				loaded, err := store.ListActivePackageManifestDependencyFacts(ctx, []string{ecosystem}, names)
				if err != nil {
					t.Fatal(err)
				}
				newDur, newRows, newKinds = append(newDur, time.Since(started)), len(loaded), counts.reset()
			}
			sort.Slice(oldDur, func(i, j int) bool { return oldDur[i] < oldDur[j] })
			sort.Slice(newDur, func(i, j int) bool { return newDur[i] < newDur[j] })
			t.Logf("%s %s old: rows=%d median=%s %s", state, ecosystem, oldRows, oldDur[2], manifestDeltaPerfFormat(oldKinds))
			t.Logf("%s %s new: rows=%d median=%s reached_delta_scopes=%d %s", state, ecosystem, newRows, newDur[2],
				newKinds["base lookup (per scope)"], manifestDeltaPerfFormat(newKinds))
		}
	}
	measure("delta-active")
	if _, err := database.ExecContext(ctx, manifestDeltaPerfNoDelta); err != nil {
		t.Fatalf("repoint to fulls: %v", err)
	}
	measure("no-delta")
}
