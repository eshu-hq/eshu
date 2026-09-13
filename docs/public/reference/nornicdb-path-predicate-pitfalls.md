# NornicDB Path-Predicate Pitfalls

Measured behaviours of pinned NornicDB builds that decide how a read can be
bounded and filtered. It began as the two entries that decide how a
variable-length traversal is bounded, split out of
[NornicDB Query-Shape Pitfalls](nornicdb-query-pitfalls.md) because both are
long enough to read on their own and both are load-bearing for the code-family
routes that traverse `CALLS` and `INHERITS`. New entries land here rather than
on that page because it is pinned at its current length in
`scripts/lib/markdown-line-cap-grandfather.tsv` and may not grow.

Unless an entry says otherwise, it was measured against the then-pinned
`timothyswt/nornicdb-cpu-bge:v1.2.3@sha256:4dfa887d…`. A different build may
behave differently, and the live tests named in each section are what would say
so.

The first two entries (the #6541 pair) are measured on **two** builds and name
both. Read every entry here as "observed on the builds it names", not as a
permanent property of NornicDB: the author closed a batch of Cypher defect
issues in September 2026 and released v1.3.2 with v1.3.3 following, so a shape
recorded here may already behave differently on a build newer than the one the
entry names. Re-run the entry's live test against the digest you actually
deploy before relying on either answer, and add a "fixed in" line here when a
newer build is measured.

## Pitfall: `ORDER BY` And `LIMIT` After `UNWIND` Apply Once Per Unwound Row

### Observed shape

Measured on BOTH `eshu-nornicdb-pr290:3722b483c02c` (self-reports 1.2.1) and
`timothyswt/nornicdb-cpu-bge:v1.3.1@sha256:ac524899…` (self-reports 1.3.1), over
Bolt and over HTTP, on a two-repository fixture holding eight directories:

```cypher
UNWIND $repo_ids AS rid
MATCH (d:Directory {repo_id: rid})-[:CONTAINS]->(f:File)
WHERE f.language IN $languages
WITH d, count(f) AS file_count
RETURN d.name AS name, d.repo_id AS repo_id, file_count
ORDER BY file_count DESC
LIMIT $limit
```

With `$repo_ids` naming two repositories and `$limit` 2, this returns **four**
rows, not two: the two largest directories of the first repository followed by
the two largest of the second. Each group is correctly ordered and correctly
cut to the limit; what never happens is the ordering and cut across the whole
result. A single-element `$repo_ids` returns 2, which is why the defect hides
on a one-repository fixture.

The counts and the grouping are correct. Only the row bound is wrong, and it is
wrong in the safe direction — too many rows, never too few.

### Consequence

A statement that UNWINDs a list cannot rely on its own `ORDER BY ... LIMIT` to
produce a page. A caller that trusts it serves up to `limit x len(list)` rows.

### Rule

Re-sort and truncate in the caller. That is correct on both backends rather than
a workaround for one: the global top-N is always contained in the union of the
per-group top-Ns, so a backend with a global `LIMIT` (Neo4j, and any NornicDB
build where this is fixed) makes the re-sort a no-op, while a build with the
per-id bound makes it the step that produces the page. Keep the
`ORDER BY ... LIMIT` in the statement too, since it is what bounds each group.

Because the caller-side half is a no-op on a build that bounds globally, this
rule needs no revisiting if a newer build fixes the behaviour: it stays correct
either way. The measured status on builds after v1.3.1 is open — that is what
the "observed on the builds it names" note at the top of this page means.

`buildDirectoryCypher` (`go/internal/query/language/cypher.go`) is shaped
this way, and `sortAndTruncateDirectoryRows` is the caller's half. Live pin:
`TestLiveNornicDBDirectoryLanguageQueryTruncatesToTheGlobalTopN`
(`go/internal/query/language/directory_nornicdb_live_test.go`, build tag
`live_nornicdb_language_imports_grant`). Measurements:
`docs/internal/evidence/6541-directory-query-s2.md`.

## Pitfall: An `UNWIND` Variable And A `RETURN` Alias Of The Same Name Collide

### Observed shape

On both builds named above:

```cypher
UNWIND $repo_ids AS id
MATCH (r:Repository {id: id})
RETURN r.id AS id, r.name AS name
```

The `name` column is correct. The `id` column comes back keyed by the FIRST
bound literal instead of by `id` — a driver reading the row by the alias it
asked for finds nothing under it. Renaming the loop variable fixes it:

```cypher
UNWIND $repo_ids AS rid
MATCH (r:Repository {id: rid})
RETURN r.id AS repo_id, r.name AS repo_name
```

That form returns both columns correctly on both builds, and an empty
`$repo_ids` returns zero rows rather than every repository.

### Rule

Do not reuse an `UNWIND` variable name as a `RETURN` alias. The failure is a
wrongly-named column rather than an error, so it reaches the caller as a missing
value rather than as a failure. `directoryRepositoryNames`
(`go/internal/query/language/directory.go`) uses the second form.

Distinct names cost nothing and are clearer anyway, so keep this convention even
on a build where the collision is fixed. Whether it still reproduces after
v1.3.1 is unmeasured; the reproducer above is what settles it on any build.

## Correction: The Pre-Bound-Endpoint `shortestPath` Shape Does Not Parse

[NornicDB Query-Shape Pitfalls](nornicdb-query-pitfalls.md) records, under its
variable-length anchoring entry, that a path whose BOTH endpoints are pre-bound
in their own `MATCH` clauses works without a label on the path pattern. That was
measured on v1.1.11 and was not true on the v1.2.3 pin, where the exact
`buildNornicDBCallChainCypher` statement does not parse at all:

```text
Neo4jError: Neo.ClientError.Statement.SyntaxError
(shortestPath: could not resolve start variable "start" from preceding MATCH clause)
```

The same traversal with a labelled inline-property anchor on each endpoint —
`MATCH path = (start:Function {uid:$s})-[:CALLS*1..5]->(end:Function {uid:$e})` —
runs correctly on that build. `buildNornicDBCallChainCypher` is not reachable
from `handleCallChain` (a NornicDB backend goes to `nornicDBCallChainRows`
instead), so nothing in production hits the parse error; treat the entry above
as "check before relying on it", not as a safe shape. Live pin:
`TestLiveNornicDBCallChainShippedNornicDBBuilderDoesNotParse`
(`go/internal/query/code_call_chain_path_bound_live_test.go`, build tag
`live_nornicdb_call_chain`).

## Pitfall: A List-Membership Test Inside `all(... IN nodes(path) ...)` Is Not Evaluated

### Observed shape

Measured on the then-pinned `timothyswt/nornicdb-cpu-bge:v1.2.3@sha256:4dfa887d…`
(#5167 batch 2b), against one three-node `CALLS` chain whose middle hop is in a
different repository from its two endpoints. A bound that works returns 0 rows;
an inert one returns the chain.

```cypher
MATCH path = (start:Function {uid: $s})-[:CALLS*1..5]->(end:Function {uid: $e})
WHERE <predicate>
RETURN nodes(path) AS chain, length(path) AS depth
```

| `<predicate>` | rows | correct? |
| --- | ---: | --- |
| none (control) | 1 | — |
| `all(node IN nodes(path) WHERE coalesce(node.repo_id,'') = $repo_id)` | 0 | **wrong, over-filters** |
| the same, on a chain where every node carries `$repo_id` | **0** | wrong, over-filters |
| `all(node IN nodes(path) WHERE node.repo_id = $repo_id)`, same chain | **0** | wrong, over-filters |
| `all(node IN nodes(path) WHERE coalesce(node.repo_id,'') IN $ids)` | **1** | wrong, over-returns |
| `all(node IN nodes(path) WHERE node.repo_id IN $ids)` | **1** | wrong, over-returns |
| the same with a satisfied conjunct ahead of it | **1** | wrong, over-returns |
| `none(node IN nodes(path) WHERE NOT (… IN $ids))` | **1** | wrong, over-returns |
| `size([node IN nodes(path) WHERE NOT (… IN $ids)]) = 0` | **1** | wrong, over-returns |
| `all(… IN ["repo-a"])` (inline literal list) | **1** | wrong, over-returns |
| `all(… = $g0 OR … = $g1)`, both values granted | **0** | wrong, over-filters |
| `coalesce(end.repo_id,'') = $repo_id` (endpoint control) | 0 | right |

Three conclusions follow. The inline literal list fails exactly like the bound
parameter, so parameter binding is not the offender. The obvious escape hatch —
one scalar equality per allowed value, OR-ed — fails in the OTHER direction: it
drops the chain even when every node on it is allowed.

And the single scalar equality fails that way too, which this page got wrong
until #6548. It was graded "right" on two cases whose correct answer was 0
either way — an unsatisfiable value, and a chain that genuinely crosses an
out-of-grant hop — so a predicate that returns nothing passed both. Measured
against a chain on which every node carries the value, it still returns 0, while
the same comparison on an endpoint returns the row. The offender is therefore
`all(...)` over `nodes(path)` itself, not the `IN` operator and not the
comparison: on this build that construct returns everything or nothing,
depending on the form, and never actually filters. All three rows are pinned by
`TestLiveNornicDBPathListPredicateBehaviour`, including the wholly-granted
control chain the earlier table lacked.

### Eshu implications

A path-wide "every hop is in this set" bound cannot be written in Cypher on this
build, in any form. Do NOT reach for the single scalar equality even when the
allowed set has exactly one member: it drops every row, including chains it must
admit. So:

1. Project the raw `nodes(path)` and filter application-side. Compute
   the truncation signal from the RAW row count, before the Go filter, so a page
   thinned by it reports as truncated rather than complete — the same caveat the
   `WITH`-attached `WHERE` entry carries.
2. Or avoid the path predicate entirely by bounding each hop as the traversal
   expands. `POST /api/v0/code/call-chain` takes this route on NornicDB: its
   response path is a Go-side breadth-first search over
   `nornicDBCallChainOneHopRows`, which carries the bound in its own anchoring
   `MATCH`, so bounding every hop needs no `nodes(path)` predicate at all.

The relationship story's inheritance walk takes route 1. Its statement
(`nornicDBRelationshipStoryInheritanceDepthCypher`) binds the two path endpoints
and projects `nodes(path)`; `nornicDBInheritanceRowsInGrant` drops any row whose
path crosses a repository the caller was not granted, and strips the projection
before the row is returned. That closed #6548, where an out-of-grant class
between two granted ones still yielded a depth number.

### Validation

`go test ./internal/query -tags live_nornicdb_call_chain -run
TestLiveNornicDBPathListPredicateBehaviour -count=1` against a standalone pinned
container pins every row of the table above as a MEASURED value, so a later
build that changes any of them is seen rather than silently absorbed.

## Pitfall: `STARTS WITH` And `ENDS WITH` Are True For Every Row Of A Multi-Node `MATCH`

### Observed shape

Measured on the then-pinned `timothyswt/nornicdb-cpu-bge:v1.2.3@sha256:4dfa887d…`
(#6546) against a 180-file corpus, 105 of them `.go`. A `STARTS WITH` or
`ENDS WITH` term is evaluated correctly when the `MATCH` binds one node and
evaluates as `true` for every row when it binds two or more. `CONTAINS` and a
plain equality are correct in both. An AND-ed second condition still filters,
so it is the string operator that becomes `true`, not the whole `WHERE` that is
dropped.

| `WHERE` | `MATCH (f:File)` | `MATCH (f:File)<-[:CONTAINS]-(d:Directory)` |
| --- | ---: | ---: |
| `f.name ENDS WITH '.go'` | 105 | **180** |
| `f.name STARTS WITH 'f00000'` | 30 | **180** |
| `f.name CONTAINS '.go'` | 105 | 105 |
| `f.name ENDS WITH '.go' AND f.language = 'python'` | 0 | **60** |

The consequence for a predicate written as `<right test> OR f.name ENDS WITH
'<ext>'` is that it reads as `<right test> OR true` and admits every row. That
is what the language-query builders did: `buildFileCypher` asked for `go` at a
row bound of 200 answered with 200 rows, 200 of them not Go, on a synthetic
graph of 50 repositories, 20,000 directories and 200,000 files seeded through
the projector's own shapes.

### Eshu implications

Do not put `STARTS WITH` or `ENDS WITH` in the `WHERE` of a multi-node `MATCH`
on this build. Filter on a property the projector writes and compare it with
`=` or `IN`. All four language-query builders (`buildRepositoryCypher`,
`buildDirectoryCypher`, `buildFileCypher`,
`buildEntityCypherWithSemanticFilter`) now carry `f.language IN $languages`
and no extension fallback; the projector stamps `language` on every `File` and
semantic entity it writes, so nothing is lost by dropping the file-name test,
and the bound list carries the parser spellings (`tsx`, `jsx`, `c_sharp`) the
fallback used to reach by extension.

Two shapes were measured and rejected. `CONTAINS '<ext>'` is honoured but is
not anchored, so `.go` also matches `x.gov`. A single-node pre-filter carried
through `WITH` (`MATCH (f:File) WHERE … WITH f MATCH (f)<-[:REPO_CONTAINS]-(r)`)
is honoured but exceeded the route's 10 s graph-read deadline on a 4,000-file
store where the one-clause form answered in 2.8 ms. The measurement table is in
`docs/internal/evidence/6546-language-query-extension-filter.md`.

### Validation

`go test ./internal/query -tags live_nornicdb_language_imports_grant -run
TestLiveNornicDBLanguageQueryAdmitsOnlyTheRequestedLanguage -count=1` against a
standalone pinned container seeds one polyglot repository and asks each builder
for one language at a time; before the change every case returned all seven
files, after it each returns exactly the files of the language asked for. The
entity builder's fallback is measured too: the fixture holds one Function with
no `language` property under the Python file, so `e.language IN $languages`
is evaluated against a missing property on this build. A python entity query
returns that Function through `f.language IN $languages` and reports `python`
for it, and a go entity query leaves it out, so a missing-property `IN` is
neither true nor an error here, unlike the string operators above.
