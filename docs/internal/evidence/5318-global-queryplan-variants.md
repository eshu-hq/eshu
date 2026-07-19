# Global Entity Query-Plan Evidence (#5318)

## Decision

Global entity-name reads no longer execute the unsafe graph builder variants.
Repository-selected entity resolution and code search retain their indexed
repository anchors. Global typed entity resolution and global code-name search
use the current `content_entities` snapshot with authorization and optional
language filters applied before the bounded limit.

## Rejected graph rewrite

Performance Evidence: a synthetic 12,000-entity fixture on pinned Neo4j
2026.05.0 confirmed the baseline global entity shape used `AllNodesScan` and the
baseline global code shape used `DirectedRelationshipTypeScan`. A static
per-label `CALL { ... UNION ALL ... }` candidate preserved the exact row set,
but global code substring search regressed from 51.4 ms and 525,601 database
hits to 137.4 ms and 527,203 hits. The candidate was rejected.

The historical capture command was the following one-off test invocation; it
is evidence of the measured theory step, not a current reusable gate:

```bash
cd go
go test ./internal/query -run TestIssue5318GlobalQueryPlanTheory -count=1 -v
```

The throwaway theory test and its isolated backend fixture were not retained in
production code.

## Same-logical-corpus route differential

The retained build-tagged proof
`TestIssue5318SameLogicalCorpusOldGraphAndNewContentRoute` loads the same 12,000
logical entities into a disposable Neo4j 2026 Community graph and a disposable
Postgres 16 content store. It binds the baseline graph bytes to
`origin/main@1e697fd22c3904930bb0e1fa2b20371fa3580f20` and source-file SHA-256
`4cba27767b92a7846c946f5440a156caec9442389215c14076488e5fea183bc7`.
The accepted side executes the shipped `buildEntityNameSearchQuery` bytes.

Both routes returned the same 12 `Target` identities; the symmetric identity
difference was `0/0`. The baseline graph call took 0.431584 seconds and its
profile traversed 12,000 entity rows. The accepted Postgres call took 0.003900
seconds and its `EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON)` execution time was
1.668 ms. These cross-store latencies are recorded independently and are
explicitly **not a speedup comparison**; the acceptance claim is exact output
equivalence on the replicated corpus, not a ratio between storage engines.

The retained companion proof
`TestIssue5318SameLogicalCorpusOldResolveEntityGraphAndNewContentIndex`
exercises the baseline `buildResolveEntityGraphQuery` bytes from
`origin/main@1e697fd22c3904930bb0e1fa2b20371fa3580f20`. The baseline source file is
bound to SHA-256
`10705d5d42dfef252a410938099f0eca94826b7cbbabf8d70aecf4eafb814ef7`.
Both sides query the same typed semantic `guard` rows in the replicated
12,000-entity corpus. The all-scope case returned six identical IDs with a
`0/0` symmetric difference; the scoped case allowed three repository IDs and
returned three identical IDs with a `0/0` symmetric difference.

The all-scope baseline PROFILE used `AllNodesScan`, took 0.177713 seconds, and
the accepted production SQL took 0.005746 seconds with 1.468 ms reported by
`EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON)`. The scoped baseline also used
`AllNodesScan`, took 0.185862 seconds, and the accepted production SQL took
0.002163 seconds with a 0.727 ms plan execution time. These measurements prove
the old shape is broad and the accepted shape preserves the intended typed and
authorized identities. They remain cross-store, cold-client measurements and
are explicitly non-comparable as a speedup claim.

## Production variant guardrail

The #5270 production-family guard now registers all 16 reachable SQL shapes.
Eight typed entity-resolution shapes cover label-only, semantic guard, module
kind, and attribute kind under all-repository and repository-set scopes. Eight
code-search shapes cover exact or literal-substring matching, optional
language, and both scope modes. A seventeenth entry records the empty-grants
fail-closed path, which returns before SQL execution. This split deliberately
avoids registering route combinations that production cannot emit. All
non-empty variants execute `normalizeEntityNameSearch` and the shipped
`buildEntityNameSearchQuery` rather than a copied query.

The named family fingerprint is
`4d4f47c1555b8a42caa91d20a5971902fc19b6ef65d3c77440f9be5df4333ef5`.
The builder source binding is
`3057a508e8b5acf4e07b4d5567b00dbf5e900b360eed82b3102f358e2a1e1523`.
The focused test also mutates one registered query and the source digest to
prove both unreviewed query drift and source drift fail the guard.

## Accepted content-name index

Benchmark Evidence: the recorded OLD and NEW measurements used the same
1,096,542-row retained `content_entities` snapshot through an isolated proof
table, with no persistent database mutation. Case-sensitive exact
`entity_name LIKE 'JsonProperty'` improved from a 62.687 ms parallel sequential
scan to a 2.808 ms temporary GIN bitmap scan (about 22.3x). Case-sensitive
substring `entity_name LIKE '%server%'` improved from 84.866 ms to 9.478 ms
(about 9.0x). The temporary full-column trigram index built in 1.665 seconds and
occupied 46 MB.

This excerpt intentionally records only the load-bearing predicate and index
shapes, not the isolated proof-table setup:

```sql
EXPLAIN (ANALYZE, BUFFERS)
SELECT entity_id
FROM proof_content_entities
WHERE entity_name LIKE 'JsonProperty'
ORDER BY repo_id, relative_path, start_line, entity_name, entity_id
LIMIT 50;

CREATE INDEX ON proof_content_entities
USING gin (entity_name gin_trgm_ops);
```

Cold bootstrap keeps this write-amplifying index deferred. Migration 062 makes
the existing substring-index lifecycle require all three exact full GIN
indexes, and the finalizer builds the new name index only after the bulk load.

No-Observability-Change: the accepted read uses the existing `postgres.query`
span with `db.operation=search_entity_names`, HTTP request telemetry, content
index readiness error classification, and truth envelope. It adds no graph
write, queue, worker, metric label, or runtime setting.

## Exactness and predicate proof

The accepted implementation does not use `LIKE` for exact-name reads. On an
isolated 200,000-row PostgreSQL 18 proof table, the exact predicate
`entity_name = $1` executed in 0.034 ms, compared with 0.187 ms for the
equivalent exact `LIKE` form. More importantly, escaping is part of the
substring contract: an unescaped metacharacter input matched 440 rows, while
the escaped literal input matched the intended 40 rows. The implementation
escapes backslash, percent, and underscore before using
`LIKE '%' || $1 || '%' ESCAPE '\\'`.

The same-data semantic-filter proof started with 20 exact-name rows. Applying
the declared `guard` metadata predicate returned the intended 10 rows. A
page-first catalog join plus metadata filter took 0.143 ms, while applying
metadata before the bounded page and hydrating repository names afterward took
0.085 ms. The accepted SQL therefore applies authorization, language, entity
type, exact-or-literal-substring name matching, and supported semantic metadata
inside a materialized bounded candidate query. Repository-name hydration occurs
only after that deterministic page is selected.

## Finished live proof

The regression test used a disposable PostgreSQL 16 container and a unique
programmatically created database dedicated to one invocation. It refuses to
start unless the caller sets the explicit disposable opt-in and the supplied
administrative DSN names the `postgres` database; a retained `/eshu` DSN is
rejected before connection or DDL. It loaded 100,000 noise content rows and
100,000 noise repository-catalog rows plus
representative duplicate-name, metacharacter, language, authorization, and
semantic-metadata rows, then exercised the HTTP handler rather than issuing the
SQL directly:

```bash
cd go
ESHU_TEST_CONTENT_INDEX_POSTGRES_DSN="$PROOF_DSN" \
ESHU_TEST_CONTENT_INDEX_POSTGRES_DISPOSABLE=1 \
  go test ./internal/query \
  -run TestGlobalEntityNameAPIDifferentialAndPerformanceLive \
  -count=1 -v
```

The live differential proved all duplicate exact matches, literal substring
handling for `%`, `_`, and backslash, language filtering, repository-access
filtering, and semantic metadata filtering against explicitly computed expected
rows. Every request completed below the test's 500 ms bound. The test also
asserts deterministic ordering and the legacy response fields (`name`,
`labels`, and `repo_name`) alongside content-native fields.

The exact production SQL plan over 100,002 repository-catalog rows executed in
14.063 ms. `ingestion_scopes` had `Actual Loops = 1`, 2,941 shared-hit blocks,
and zero shared-read blocks. This proves repository-name hydration is performed
once for the distinct repository IDs in the bounded page, rather than by a
per-row `LATERAL` catalog scan.

The index lifecycle was separately exercised against fresh bootstrap, migration
retry, an upgrade state with the pre-062 two-index validator, and deferred
bootstrap. Normal and upgraded databases created all three substring indexes;
deferred bootstrap did not create the name index until finalization. All proof
tables and schema state lived only in the disposable database; no retained Eshu
database was mutated.

The concurrency proof populated `content_entities` before migration 062, held
an `ACCESS EXCLUSIVE` blocker to prove bounded `lock_timeout`, cancelled a
second attempt to prove interruption cleanup, then ran two concurrent migration
retries and two concurrent full `ApplyBootstrap` calls. The initial red test
reproduced PostgreSQL's same-name `CREATE INDEX IF NOT EXISTS` catalog race;
the transaction-scoped migration lock and bounded whole-bootstrap ownership
boundary made the retry green. Exact GIN method, `entity_name` column,
`gin_trgm_ops`, validity/readiness, and the three-index lifecycle validator were
all asserted. The complete live storage lifecycle suite passed in 19.276
seconds.

The MCP proof sends real JSON-RPC requests through the HTTP transport for
`find_code` and `resolve_entity`. It verifies exactness, type and language
forwarding, bounded limits, and the content-index truth envelope. The CLI proof
also pins explicit `count` / `limit` / `truncated` metadata. The CLI proof keeps
`eshu find name` on its legacy name-only `/entities/resolve` graph domain and
proves that a server's explicit untyped-global 400 is propagated without a
fallback to `/code/search`.

After rebasing onto `origin/main@e8e3928b97c20335bf60cd47ea28a9ce884e9cf7`,
the B-7 golden-corpus gate was rerun at implementation head
`74a530f8a7f6f865f1f5de1a18e2aea258c66473` on isolated alternate host ports
because a retained development stack owned the defaults. The full
20-repository pipeline completed in 35 seconds with 420 passes, zero required
failures, and zero advisory warnings. HTTP and MCP `find_code` both proved
`limit=1`, `count=1`, `truncated=true`, and the deterministic first identity.
Typed `resolve_entity` proved count 2 and both expected same-name identities.

## Exact implementation binding

The implementation, test, contract, and public-documentation patch, excluding
this self-referential evidence file, is bound by SHA-256
`644e3a6ebc2f8f17ea1c08df609ad845f506f070974f659b1102678d589aa826`.
The digest includes the pinned base commit, the binary working-tree diff, and
the path and content hash of every non-ignored untracked file. Run the following
exact command from the repository root; it intentionally excludes only this
self-referential evidence file:

```bash
compute_issue_5318_binding() {
  local base='e8e3928b97c20335bf60cd47ea28a9ce884e9cf7'
  local evidence_path='docs/internal/evidence/5318-global-queryplan-variants.md'
  export LC_ALL=C
  {
    printf 'BASE\0%s\0' "$base"
    git -c core.quotepath=false diff --binary --no-ext-diff \
      "$base" -- . ":(exclude)$evidence_path"
    git ls-files --others --exclude-standard -z |
      sort -z |
      while IFS= read -r -d '' file; do
        [[ "$file" == "$evidence_path" ]] && continue
        printf 'UNTRACKED\0%s\0' "$file"
        shasum -a 256 -- "$file"
      done
  } | shasum -a 256 | cut -d ' ' -f 1
}
compute_issue_5318_binding
```

Two consecutive executions produced the recorded digest. Any later source,
test, contract, or documentation edit outside this evidence file invalidates
the value.
