# 6541: directory language query, seek by repo_id instead of walking a chain

What this records: the shape the directory branch of
`POST /api/v0/code/language-query` now runs, the live correctness proof behind
it on two NornicDB builds, and three backend defects measured while choosing
it. Corpus-scale timing is NOT in this document yet; see
[Corpus timing: pending remote run](#corpus-timing-pending-remote-run).

## The change

The statement walked an unbounded chain from File to Repository:

```cypher
MATCH (f:File)<-[:CONTAINS]-(d:Directory)<-[:REPO_CONTAINS|CONTAINS*]-(r:Repository)
WHERE f.language IN $languages
WITH d, r, count(f) as file_count
RETURN ... r.name as repo_name, file_count ORDER BY file_count DESC LIMIT $limit
```

It now seeks each granted repository's directories by an indexed `repo_id`:

```cypher
UNWIND $repo_ids AS rid
MATCH (d:Directory {repo_id: rid})-[:CONTAINS]->(f:File)
WHERE f.language IN $languages
WITH d, count(f) as file_count
RETURN d.id as entity_id, d.name as name, labels(d) as labels,
       d.relative_path as file_path,
       d.repo_id as repo_id,
       file_count
ORDER BY file_count DESC
LIMIT $limit
```

Two columns of work move to the handler, both because the backend cannot do
them in this statement (measured below):

1. `repo_name` is filled from a second bounded read keyed on the `repo_id` the
   statement projects, over the page's own repositories:
   `UNWIND $repo_ids AS rid MATCH (r:Repository {id: rid}) RETURN r.id as repo_id, r.name as repo_name`.
2. The page is re-sorted on `file_count DESC, repo_id, name` and truncated to
   `limit`.

The projected columns are unchanged. `entity_id` and `file_path` are still
null on every directory row: the canonical projector writes neither `d.id` nor
`d.relative_path`. That predates this change, is asserted as a known gap by the
live test, and is NOT fixed here.

### Where the repository-id list comes from

| caller | list |
| --- | --- |
| scoped, no `repo_id` | `access.RepositorySearchIDs()`, the sorted deduplicated union of granted repository and scope ids |
| scoped or unscoped, `repo_id` given | `[repo_id]`, or empty when the grant disallows it |
| unscoped admin, no `repo_id` | every `Repository.id`, read from the graph |

The scoped list reproduces exactly what the replaced predicate
`r.id IN $allowed_repository_ids OR r.id IN $allowed_scope_ids` admitted.
Deduplication is load-bearing. A repeated id is visited twice by UNWIND, and
what that costs depends on the backend: on both pinned builds it returns every
one of that repository's directories TWICE with `file_count` intact (measured:
8 rows for 4 directories, `inner` 3 in each), because everything after the
UNWIND runs once per id; a backend that aggregates the whole result at once
would instead count each file twice and double the counts. An earlier draft of
this document and of the code comments claimed the doubling happens here — it
does not, and
`TestLiveNornicDBDirectoryLanguageQueryRepeatsRowsForARepeatedID` now pins the
behaviour that was actually measured.

The unscoped list is read from the graph rather than from the Postgres
repository catalogue (`ingestion_scopes`), so a repository present in the graph
but absent from the catalogue cannot silently vanish from an admin's answer. A
Directory-label anchor was rejected instead: expanding
`(d:Directory)-[:CONTAINS]->(f:File)` over the issue's corpus measured 8.658s
for 200,000 rows, and a whole-label scan is what the query-plan gate rejects.

## Backend defects measured while choosing the shape

These are observations of the two builds named below, and they are current
behaviour on every released build. An upstream probe (2026-09-13) established
that the NornicDB fixes for this defect surface landed on `orneryd/NornicDB`
`main` AFTER the v1.3.2 tag (`d2c8a9b4`, 2026-09-11) and after the newest
published image, and that no v1.3.3 tag, release or image exists — the relevant
commits are `4393e7e6416a` ("restore clause pipeline and aggregation semantics")
and `0c2766bfc9e4` ("filter aggregated WITH rows after chained MATCH"). So
v1.3.1, which #6657 pins, and v1.3.2 both still carry them, and nothing here is
marked "fixed in v1.3.2".

None of that changes what ships here: the shape below is correct on every build
measured, and its caller-side halves stay correct on a build that fixes the
underlying behaviour (the re-sort becomes a no-op under a global LIMIT). If the
single-statement form proves correct on a build cut after those commits,
simplifying to it is a follow-up once Eshu's pin moves, not a change to this one.

Builds: `eshu-nornicdb-pr290:3722b483c02c` (self-reports 1.2.1, the Compose pin)
and `timothyswt/nornicdb-cpu-bge:v1.3.1@sha256:ac52489925968e39d18f845bde5fa2fe363ba703443ead7f97ebc2b0c0084962`
(self-reports 1.3.1). Identity confirmed with `CALL dbms.components()` and the
Bolt agent string on each container. Fixture: two repositories, eight
directories nested 0-3 levels, 17 files across go/python/markdown, seeded
through the canonical projector's own write shapes. Transport: Bolt (Python
neo4j 6.3.0) and HTTP `/db/nornic/tx/commit`, which agree on every row below.

### 1. A trailing Repository MATCH returns the literal string "r.name"

`UNWIND ... WITH d, count(f) AS c MATCH (r:Repository {id: d.repo_id}) RETURN r.name, d.path, c`
returns correct counts and, in the name column, the literal text `r.name`. This
is why the statement cannot project the repository name at all.

One thing changes it, and neither is available to this route:

| variant | 1.2.1 | 1.3.1 |
| --- | --- | --- |
| the route's form (UNWIND + `WHERE f.language IN $languages`) | `"r.name"` | `"r.name"` |
| one-element `$repo_ids` | `"r.name"` | `"r.name"` |
| literal list `UNWIND ['a','b']` | `"r.name"` | `"r.name"` |
| **no WHERE at all** | `"r.name"` | **correct** |
| **no UNWIND**, `{repo_id: $rid}` with the WHERE | `"r.name"` | **correct** |
| `MATCH (r:Repository) WHERE r.id = d.repo_id` | literals | literals |
| carry `d.repo_id AS rk`, then `{id: rk}` | 0 rows | 0 rows |

On 1.3.1 the defect therefore needs UNWIND and a MATCH-level WHERE together —
and the route needs both. Worse, once the statement carries the route's real
projection (`labels(d)`, `c AS file_count`, `r.id AS repo_id`), even the
no-WHERE and no-UNWIND variants return literals on 1.3.1 as well. Two shapes
that DO name the repository correctly on 1.3.1 — a `d.repo_id IN $ids` filter,
and Repository matched first — drop the `LIMIT` entirely, returning every row.

### 2. `ORDER BY`/`LIMIT` after `UNWIND` applies once per unwound id

With two repositories and `LIMIT 2`, every UNWIND form above returns 4 rows:
each repository's own top 2. Correct within each group, never across the
result. This is why the handler truncates. Documented in
[NornicDB Path-Predicate Pitfalls](../../public/reference/nornicdb-path-predicate-pitfalls.md).

### 3. An UNWIND variable and a RETURN alias of the same name collide

`UNWIND $repo_ids AS id MATCH (r:Repository {id: id}) RETURN r.id AS id, r.name AS name`
returns the id column keyed by the first bound literal on both builds. Renaming
the loop variable to `rid` and the aliases to `repo_id`/`repo_name` returns both
columns correctly on both builds; that is the form the name read uses. Also
documented on the pitfalls page.

## Local correctness proof

`TestLiveNornicDBDirectoryLanguageQuery*`
(`go/internal/query/language/directory_nornicdb_live_test.go`, build tag
`live_nornicdb_language_imports_grant`) drives the production path — statement,
truncation and name read — through the language package's `Handler` with a real
`Neo4jReader`, against the fixture above. Expected counts are hand-computed from
the fixture: `lib` 4, `inner` 3, `src` 2, `cmd` 2, `pkg` 1, `deep` 1, with
`docs` and `cmd/tool` absent because they hold no go file.

```
ESHU_NEO4J_URI=bolt://127.0.0.1:17955 go test ./internal/query/language \
  -tags live_nornicdb_language_imports_grant \
  -run TestLiveNornicDBDirectoryLanguageQuery -count=1 -v
```

| build | result |
| --- | --- |
| 1.3.1 (port 17955) | 4/4 PASS, `ok ... 0.774s`, exit 0 |
| 1.2.1 (port 17957) | 4/4 PASS, `ok ... 0.728s`, exit 0 |

What the four cases prove:

- **Nesting.** `deep` (depth 3) keeps its own file and `inner` (depth 2) keeps
  its three. The replaced walk, and every bounded `*1..N` variant of it, folded
  a nested directory's files into its parent and dropped the nested directory.
- **Language filter.** `docs` (markdown) and `cmd/tool` (python) are absent
  from a go query even though both hold files.
- **The row bound.** Two repositories at `limit` 2 return exactly `lib` (4) and
  `inner` (3) — the global top-N, not the four-row per-repository superset the
  backend produced.
- **The grant.** A caller granted only alpha receives alpha's four directories
  and no beta row. A grant naming a repository that does not exist returns zero
  rows (the impossible-value control).
- **repo_name.** Every row carries the name the statement no longer projects.
- **A repeated id.** Handed `[alpha, alpha]` directly, the statement returns
  alpha's four directories twice, counts intact. The production path, given a
  grant naming alpha in both its repository and scope lists, returns each
  directory once with the right count — the deduplication doing its job.

### Test sensitivity

The builder tests were written against the old statement and failed on it, but
that first RED was recorded before this branch was rebased onto the
`internal/query/language` package split, and the commit carrying it does not sit
on this branch's history. What is reproducible HERE is a mutation control:
putting the old `(f:File)<-[:CONTAINS]-(d:Directory)<-[:REPO_CONTAINS|CONTAINS*]-(r:Repository)`
pattern back inside the current builder, keeping its signature, fails
`TestBuildDirectoryCypherWalksNoVariableLengthChain` on every caller class
("Directory statement still walks a variable-length chain") and
`TestBuildDirectoryCypherProjectsRepoIDNotRepoName`, exit 1. Restoring the
statement returns the package to green.

Restoring the whole pre-change builder file instead produces a BUILD failure,
because the handler calls the new signature. That is not evidence of anything,
and is recorded here so nobody cites it as a RED.

### Two live failures that are NOT this change

Running the whole `live_nornicdb_language_imports_grant` tag against these two
images fails twice, and both failures reproduce identically on the branch's base
commit `514534567` in a separate worktree, against the same containers:

| test | 1.3.1 | 1.2.1 | on base `514534567` |
| --- | --- | --- | --- |
| `TestLiveNornicDBImportDependencyGrantBindsEveryBuilder` | FAIL | FAIL | FAIL on both, identically |
| `TestLiveNornicDBLanguageQueryDirectoryTwoClauseShapeReturnsNothing` | PASS | FAIL | same: passes on 1.3.1, fails on 1.2.1 |

Neither is caused by this change: the imports builders are untouched, and the
failing probe inside the second is hand-written Cypher, not a builder. Both
tests were written against `sha256:4dfa887d…` (self-reports 1.2.2), which is
neither image here. The imports failure is the per-unwound-id row bound
(entry 2 above) reaching a different family: its unscoped control expects a full
page of out-of-grant rows and gets 14 rows spanning both repositories. The
second reports that on 1.2.1 even a two-clause aggregation projecting plain
properties returns nothing, where the page documents that shape as working.
Both belong to whoever re-pins those tests to the build they run against.

### Grant coverage moved for the Directory branch

`TestLiveNornicDBLanguageQueryGrantBindsEveryBuilder` no longer includes
Directory. Its control proves a grant PREDICATE decides row membership before
the row bound, by filling an unscoped page entirely with out-of-grant rows. That
argument does not apply to this branch any more: its grant is the UNWOUND id
list rather than a predicate, so an out-of-grant row cannot be produced at all,
and the per-id row bound means an unscoped two-repository page holds each
repository's own top rows instead of filling with the larger one's. The Directory
grant is proved end to end by
`TestLiveNornicDBDirectoryLanguageQueryHonoursTheGrant` plus its impossible-grant
control. The test passed on the base commit with Directory included and passes
here without it.

Unit and contract coverage, both backends irrelevant:
`go test ./internal/query ./internal/query/language ./internal/queryplan ./internal/graph -count=1` → exit
0 (`ok internal/query 23.198s`, `ok internal/queryplan 0.307s`, `ok
internal/graph 0.231s`). `go vet -tags live_nornicdb_language_imports_grant
./internal/query` → exit 0.

## The directory_repo_id index

`CREATE INDEX directory_repo_id IF NOT EXISTS FOR (d:Directory) ON (d.repo_id)`
is declared for both backends. The issue measured the seek at 53ms against
4.964s for the same aggregation reached through a `WHERE` at a grant of one
repository, and recorded that the index alone buys nothing — the inline-property
seek is what buys the speedup and the index is its prerequisite.

**Index use is not proven on NornicDB, and cannot be from this machine.** The
build reports no query plan at all (`TestLiveNornicDBGrantPlanShapeIsNotReportable`),
and `SHOW INDEXES` reports `readCount: 0` for every index even after a seek
that must use one: a positive control on the long-established
`nornicdb_directory_path_lookup` (a `{path: ...}` seek the issue measured at
13ms) also left the counter at 0 on 1.3.1. The counter is not implemented, so
it is evidence of nothing in either direction. What IS available:

- On Neo4j the plan-profile gate proves the anchor plans as an index seek:
  `QP-LANGUAGE-DIRECTORY` names `directory_repo_id` in `required_schema`, which
  is what makes the gate create it before profiling, and the variant family
  rejects `AllNodesScan`.
- On NornicDB the seek-versus-WHERE contrast is corpus-scale timing, which
  belongs to the remote run below.

The schema bump is additive: the index changes reads only, no MERGE or MATCH
identity moves, so the previous fingerprint stays a compatible predecessor.

## Corpus timing: pending remote run

Not run here. Per the owner's rule, performance measurement happens on the
remote host; this machine is shared and contended, and a local figure would be
invalid. The recipe:

**Corpus.** 50 repositories; 20,000 Directory nodes (400 per repository, nesting
depth 0-8); 200,000 File nodes (10 per directory, so every correct `file_count`
is 10 at a grant of one repository). Seed through the projector's own write
shapes in `go/internal/storage/cypher/canonical_node_cypher.go`:
`canonicalNodeRepositoryUpsertCypher`, then `canonicalNodeDirectoryNodeCypher`,
then the depth-0 and depth-N directory edge phases (committed after the node
phase), then `canonicalNodeFileFirstGenerationMergeCypher`, which writes both
`REPO_CONTAINS` and `CONTAINS`. Batch 500 rows per UNWIND. No generator is
committed; the issue's 2026-09-05 corpus was seeded ad hoc the same way.

**Schema first.** Apply the graph schema before the timed reads
(`eshu-bootstrap-data-plane`, or `graph.SchemaStatementsForBackend`), so
`directory_repo_id` exists.

**Protocol.** The build answers an identical repeated query from a last-result
cache in about 1ms, so vary `$limit` between consecutive timed runs. Its latency
and memory drift upward over a container's life, so restart the container
between cells. Run the production builder through the production reader, with
the deadline lifted for measurement and one run at the real 10s deadline for the
unscoped case.

**Cells.** grant-1, grant-5, grant-50 and unscoped, at limits 50 and 200, one
discarded warm-up then one warm run each. Record rows, wall time, and a
correctness check that every `file_count` is 10.

**Two contrasts worth including.** The same cells with `directory_repo_id`
dropped, which is the only way to show the index is load-bearing on NornicDB
given the missing plan and counters; and the handler's second read, to confirm
the name lookup stays near the 135ms the issue measured.

Baseline to beat, from the issue's measurement of the replaced statement:
34.510s at grant-1/50, 2m01.437s at grant-50/50, and a 10.005s deadline failure
unscoped. S2's own measured figures there were 53ms at grant-1 and 5.728s at
grant-50, before the handler's extra read.

Performance Evidence: pending the remote corpus run described above. What is
measured today is correctness only, on a 2-repository/8-directory/17-file
fixture, on both NornicDB builds (table above). The corpus figures quoted in
this document are the issue's 2026-09-05 measurements of the candidate shapes,
not measurements of this implementation, which adds one bounded read and a
Go-side sort the issue's numbers do not include.

Observability Evidence: `language.Handler.logDirectoryRead` records
`repositories`, `rows_returned`, `rows_kept` and `repositories_named` for every
directory read, so an operator can separate a wide grant from a backend
returning far more rows than the page needs (`rows_returned` against
`rows_kept`, which is how the per-unwound-id row bound shows up in production),
and can attribute a missing `repo_name` to the second read rather than to the
statement.
