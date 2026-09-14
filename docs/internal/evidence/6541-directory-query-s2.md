# 6541: directory language query, seek by repo_id instead of walking a chain

What this records: the shape the directory branch of
`POST /api/v0/code/language-query` now runs, the live correctness proof behind
it on two NornicDB builds, and three backend defects measured while choosing
it. Corpus-scale timing was measured on the remote host and is summarised in
[Corpus timing](#corpus-timing-measured-on-the-remote-host); the full run record
is in [6541 corpus timing](6541-directory-query-s2-corpus-timing.md).

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

Because the bound is per group, the statement's own `ORDER BY` is also what
decides WHICH rows each group keeps, and the handler cannot undo that choice —
no re-sort returns a row the backend never sent. Measured on two repositories of
eight directories each, every directory holding exactly one go file so that
every row ties, at `limit` 4, through the production handler:

| statement `ORDER BY` | page on 1.2.1 (`pr290`) | page on v1.3.1 |
| --- | --- | --- |
| `file_count DESC` | `a01,a02,a03,a07` | `a02,a03,a06,a07` |
| `file_count DESC, repo_id ASC, name ASC` | `a01,a02,a03,a04` | `a01,a02,a03,a04` |

Two builds, one statement, one fixture, two different pages. The fix is to give
the statement the same total order the handler sorts on, which is what the
review's F1 asked for and what this branch now ships. With them aligned the page
is a function of the data: the rows ARE the total order's top-L and the order
within the page is that same total order, on both builds and on Neo4j's global
`LIMIT`.

### 3. An UNWIND variable and a RETURN alias of the same name collide

`UNWIND $repo_ids AS id MATCH (r:Repository {id: id}) RETURN r.id AS id, r.name AS name`
returns the id column keyed by the first bound literal on both builds. Renaming
the loop variable to `rid` and the aliases to `repo_id`/`repo_name` returns both
columns correctly on both builds; that is the form the name read uses. Also
documented on the pitfalls page.

### 4. An `ORDER BY` key written as `d.<property>` after an aggregating `WITH` is ignored

Found while proving the fix for defect 2 above, on both builds. Same fixture,
five directories per repository, all tied, `$limit` 4; the rows are what the
first repository's group retained:

| `ORDER BY` clause | 1.2.1 (`pr290`) | v1.3.1 |
| --- | --- | --- |
| `file_count DESC` | `a5,a3,a2,a1` | `a1,a2,a4,a3` |
| `file_count DESC, d.repo_id ASC, d.name ASC` | `a5,a3,a2,a1` | `a5,a1,a2,a4` |
| `file_count DESC, repo_id ASC, name ASC` | `a1,a2,a3,a4` | `a1,a2,a3,a4` |

The property form is accepted without error or warning and served as though the
trailing keys were absent. `d` is still bound after
`WITH d, count(f) AS file_count`, and the clause is valid Cypher on Neo4j, so
nothing about the statement looks wrong — only the rows say so, and only when a
tie exists to expose it. `buildDirectoryCypher` therefore sorts on the `RETURN`
aliases. Documented on the pitfalls page; no upstream fix commit has been
matched to this one, unlike defects 1-3.

## Local correctness proof

`TestLiveNornicDBDirectoryLanguageQuery*`
(`go/internal/query/language/directory_nornicdb_live_test.go`, build tag
`live_nornicdb_language_imports_grant`) drives the production path — statement,
truncation and name read — through the language package's `Handler` with a real
`Neo4jReader`, against the fixture above. Expected counts are hand-computed from
the fixture: `lib` 4, `inner` 3, `src` 2, `cmd` 2, `pkg` 1, `deep` 1, with
`docs` and `cmd/tool` absent because they hold no go file.

```
ESHU_NEO4J_URI=bolt://127.0.0.1:17925 go test ./internal/query/language \
  -tags live_nornicdb_language_imports_grant \
  -run TestLiveNornicDBDirectory -count=1 -v
```

Latest run, on the head that carries the review's F1 fix. It supersedes the
four-case run recorded before that fix (1.3.1 `ok 0.774s` / 1.2.1 `ok 0.728s`,
both 4/4); the fifth case is the tie proof below.

| build | image | port | result |
| --- | --- | --- | --- |
| 1.2.1 | `eshu-nornicdb-pr290:3722b483c02c` | 17925 | 5/5 PASS, `ok ... 0.779s`, exit 0 |
| 1.3.1 | `timothyswt/nornicdb-cpu-bge:v1.3.1@sha256:ac524899…` | 17927 | 5/5 PASS, `ok ... 0.765s`, exit 0 |

What the five cases prove:

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
- **Tie determinism.** On its own fixture — two repositories of eight
  directories, every directory holding exactly one go file, seeded in reverse
  name order — the page at `limit` 4 is `a01,a02,a03,a04` and the same request
  returns the same page twice with a differently-shaped read in between to
  defeat the build's last-result cache. A grant of the second repository alone
  returns `b01,b02,b03,b04`, so the other group's bound is exercised too.
  (`directory_tie_nornicdb_live_test.go`.)

### RED then GREEN for the tie proof

The tie case is a real regression guard, not a shape assertion. With the
statement's `ORDER BY` reverted to `file_count DESC` alone and nothing else
changed, it fails on BOTH builds, and fails differently on each — which is the
defect itself, visible in the failure text:

| build | failure |
| --- | --- |
| 1.2.1 (17925) | `page = [a01 a02 a03 a07], want [a01 a02 a03 a04]`, exit 1 |
| 1.3.1 (17927) | `page = [a02 a03 a06 a07], want [a01 a02 a03 a04]`, exit 1 |

Restoring `ORDER BY file_count DESC, repo_id ASC, name ASC` returns both builds
to 5/5 PASS, exit 0. The unit-level pin is
`TestBuildDirectoryCypherOrdersOnTheHandlersTotalOrder`, which asserts the
statement carries those three keys and that the `d.<property>` spelling — the
one neither build honours — has not come back.

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
- On NornicDB the seek-versus-WHERE contrast is corpus-scale timing, which the
  remote run below now carries.

The schema bump is additive in the sense the compatibility contract means: the
index moves no MERGE or MATCH identity, so a writer on the previous fingerprint
writes the identical graph and that fingerprint stays a compatible predecessor.

It is NOT free on the write side, and this document should not be read as saying
so. `d.repo_id` is written by every Directory MERGE and SET the canonical
projector emits (`canonicalNodeDirectoryNodeCypher`), so each of those now also
maintains an index entry, one per directory per repository on a full projection
— 20,000 entries on the corpus below. The trade was taken because the read it
serves was measured at 15.987s without the index against 8.534s with it, at a
grant of fifty repositories on that corpus. `repo_id` being effectively
immutable per node (a Directory belongs to one repository for its whole life, so
after the first write the entry is re-set to the same value rather than moved)
is a secondary comfort whose cost benefit is unverified: whether this backend
charges a same-value SET less index maintenance than a value-changing one was
never measured. The projection-side delta at corpus scale is NOT measured
either; it is still open, and the corpus section below says so.

## Corpus timing: measured on the remote host

Measured, not pending. The run happened on the remote Linux host per the owner's
rule; this machine is shared and contended, and a local figure would not be
evidence. The full record — identity, protocol, every cell, the cold runs, and
the caveats — is in
[6541 corpus timing](6541-directory-query-s2-corpus-timing.md). What must not be
lost from it:

- **Frame.** Remote Linux x86_64, 16 logical CPUs, 123 GiB RAM, Go 1.26.2;
  NornicDB `timothyswt/nornicdb-cpu-bge:v1.3.1` at manifest digest
  `sha256:ac52489925968e39d18f845bde5fa2fe363ba703443ead7f97ebc2b0c0084962`;
  50 repositories / 20,000 directories / 200,000 files seeded through the
  canonical projector's own write shapes. BEFORE is `origin/main` at the
  merge-base `bb5f00671`, AFTER is `daf9f5222`, both driven through the
  production builder and the production reader.
  **`absolute_target_applicable: false`** — this is NOT the accepted
  896-repository reference profile, so no absolute target applies and every
  figure is a same-machine relative comparison.
- **The win, index present, warm timed run:** 15.722s -> 0.074s at grant-1/50
  (212x), 16.228s -> 0.922s at grant-5/50 (17.6x), 33.964s -> 8.534s at
  grant-50/50 (4.0x), 12.484s -> 7.465s at unscoped/50 (1.7x), with the
  limit-200 row of each cell within a second of its limit-50 row except
  grant-50 with the index present, which is 1.269s FASTER at limit 200 (7.265s
  against 8.534s). Largest gap among the other eleven pairs: 0.618s.
- **The index is a precondition, not an enhancement.** Without
  `directory_repo_id` the new shape is SLOWER than the shipped statement at the
  unscoped cell — 15.384s against 12.484s at limit 50, 15.446s against 12.664s
  at limit 200. The rewrite is not a win on its own at that width.
- **The 10s reader deadline is cleared for this corpus, not in general.**
  Unscoped at limit 50: BEFORE DEADLINE_FAILED at 10.000s / 0 rows, AFTER with
  the index absent DEADLINE_FAILED at 10.010s / 0 rows, AFTER with it present
  INSIDE_10S_DEADLINE at 8.325s / 50 rows. That is ~17% headroom on an idle
  16-CPU box at 50 repositories, and unscoped cost is close to linear in grant
  width, so a 100-repository deployment would not clear it.
- **The tie-break keys are priced, not free.** The tables were measured at
  `daf9f5222`, before `ORDER BY file_count DESC, repo_id ASC, name ASC` landed.
  A paired re-measure at the shipped statement `34a46500a` (same store, one
  session, 2 reps per head per cell) gives unscoped/200 7.451s -> 7.754s
  (+0.303s, +4.1%) and grant-50/200 7.746s -> 7.988s (+0.242s, +3.1%). Both
  deltas are smaller than the 0.614s-0.988s within-head spread, so at n=2 they
  are not resolvable from run-to-run variance — but the newer head was slower in
  4 of 4 paired runs and in both cold pairs. The result is a bound, not a null:
  at these two cells the keys cost at most ~0.5s (~6%), point estimate
  ~0.25-0.30s (~3-4%).
- **Correctness held:** `all_file_counts_10=true`, `within_limit=true`, and
  `repo_name_filled` equal to the row count, in every timed and every cold cell
  of both runs.
- **The per-unwound-id bound, measured:** unscoped at limit 200 returns 10,000
  raw rows (50 ids x limit 200) before `sortAndTruncateDirectoryRows` cuts them
  to 200 — direct evidence that the Go-side re-sort and truncate is load-bearing
  on this build rather than defensive decoration.
- **F4 is closed** by the two side reads the run was asked to report separately:
  `directoryRepositoryNames` at this corpus's worst case of 50 distinct
  repositories on one page costs 0.005s with 50/50 named, and `allRepositoryIDs`
  costs 0.000s for 50 ids. Both are negligible; the unscoped cell's 7.465s is
  essentially all statement. A contrast the recipe would not have surfaced: a
  scoped caller holding no grants at all runs BEFORE 15.730s / 0 rows against
  AFTER 0.000s / 0 rows, because the branch short-circuits before touching the
  backend where the shipped statement burns a full scan.
- **Two caveats travel with the numbers.** The grant-50 raw-row probe's 0.045s
  is a result-cache hit, not a measurement — its row count and count assertion
  are valid, its time is not. And NEITHER of the issue's 2026-09-05 figures for
  the replaced statement reproduced here: 34.510s at grant-1/50 there against
  15.722s here (2.20x), 121.437s (2m01.437s) at grant-50/50 there against
  33.964s here (3.58x). That measurement and this one are not a controlled pair
  — different machine, different ad-hoc seeding — so neither ratio is a result,
  and the grant-50 cell misses by more than the grant-1 cell, not less.
  Everything in the tables above IS a controlled pair: one seeded store, one
  container, one machine, one session.
- **Still unmeasured:** the index's write-side cost at projection scale. This
  run timed reads, not writes.

Performance Evidence: corpus-scale, remote Linux host, NornicDB v1.3.1 at digest
`sha256:ac52489925968e39d18f845bde5fa2fe363ba703443ead7f97ebc2b0c0084962`, 50
repositories / 20,000 directories / 200,000 files, BEFORE `bb5f00671` against
AFTER `daf9f5222`, `absolute_target_applicable: false` — a same-machine relative
comparison, not the 896-repository reference profile. With `directory_repo_id`
present: 15.722s -> 0.074s at grant-1/50 (212x), 33.964s -> 8.534s at
grant-50/50 (4.0x), 12.484s -> 7.465s at unscoped/50 (1.7x), and the unscoped
10s reader deadline moves from DEADLINE_FAILED at 10.000s to INSIDE_10S_DEADLINE
at 8.325s — ~17% headroom that shrinks as repository count grows. Without the
index the new shape is SLOWER than the shipped one unscoped (15.384s against
12.484s), so the index is a precondition of the claim rather than an
optimisation on top of it. The shipped tie-break keys cost at most ~0.5s (~6%)
at unscoped/200 and grant-50/200, point estimate ~0.25-0.30s (~3-4%), slower in
4 of 4 paired runs. Correctness (`all_file_counts_10`, `within_limit`,
`repo_name_filled`) held in every cell, and the index's write-side cost at
projection scale is still unmeasured. Full record:
docs/internal/evidence/6541-directory-query-s2-corpus-timing.md.

Observability Evidence: `language.Handler.logDirectoryRead` records
`repositories`, `rows_returned`, `rows_kept` and `repositories_named` for every
directory read, so an operator can separate a wide grant from a backend
returning far more rows than the page needs (`rows_returned` against
`rows_kept`, which is how the per-unwound-id row bound shows up in production —
10,000 against 200 at the unscoped corpus cell above), and can attribute a
missing `repo_name` to the second read rather than to the statement.

## Noted, not changed here

`go/internal/graph/schema_application.go` stands at 491 lines of the repo's
500-line cap after this change (#6541 review finding F7). It is not split in
this PR: a split would move the schema fingerprint constants and their
compatibility contract into a new file in a change whose subject is a query
shape, and the constants are exactly what a reviewer of this PR needs to read in
one place. The next change that adds a fingerprint to that file has to split it
first.
