# Impossible-value controls for the language-query grant statements

## Why this exists

Every live assertion guarding these statements is positive, so an entire class
of predicate failure is invisible to the suite. That gap is worth closing on its
own merits, and a real defect on one NornicDB build is what exposed it.

**Which build, stated precisely, because an earlier version of this note got it
wrong.** The defect is on the **embedded** library
(`go.mod: github.com/orneryd/nornicdb v1.0.45`, which reports product version
**1.0.0**). That library is linked ONLY under the `nolocalllm` build tag --
`internal/cli/localsupervisor/graph.go:93` errors with "embedded NornicDB is not
available in this Eshu build" otherwise -- so it is reachable in local developer
profiles, **not** in the deployed topology.

What the deployed topology actually runs is the 1.2.x line:
`scripts/verify-replay-tier.sh:24` pins
`timothyswt/nornicdb-cpu-bge:v1.2.3`, and `docker-compose.yaml` defaults to
`eshu-nornicdb-pr290:3722b483c02c` (1.2.1). **Both filter correctly** -- measured,
see below.

So this is a local-profile correctness bug, **not a production tenancy
exposure**. An earlier draft called 1.0.0 "the shipped build" and implied the
latter; that was wrong.

On 1.0.0, `IN` is ignored the moment a second node joins the MATCH. `=` filters correctly, and a
single-node MATCH filters correctly, so the failure is invisible to any test
that asks a question with more than one acceptable answer.

That matters most for tenancy. `RepositoryAccessFilter.GraphConditionOnProperty`
(`querycontract/repository_authz.go:164`) builds the entire grant as:

    (alias.prop IN $allowed_repository_ids OR alias.prop IN $allowed_scope_ids)

and `GraphCondition`, `GraphPredicate`, `GraphWhereClause`,
`GraphPredicateOnProperty` and `GraphWhereClauseOnProperty` all funnel through
it. Measured against a fresh 21-node store with both grant lists naming ids that
exist nowhere: the single-node form returned 0 rows, the two-node form returned
**every row**.

**The 1.2.x control, measured on this box.** The same impossible-grant probe
against the 1.2.1 container (`dbms.components()` -> `["NornicDB",["1.2.1"]]`)
returned **0 rows at one, two and three nodes**, with an `=` control and the
`any(...)` form all clean. That is the version the gates and Compose run.

## Why the existing suite could not fail

`seedLiveGrantGraph` writes every `File` with one constant, `liveGrantLanguage =
"python"` (`language_query_grant_nornicdb_live_test.go:70`, applied at :349). In
a graph where every node is python, "filter to python" and "return everything"
are the same output. The suite's grant cases have the same shape: they assert a
**granted** caller sees the granted rows, which stays true whether or not the
predicate is enforced.

A repo-wide search for an impossible-value assertion in `internal/query`
returned nothing before this change.

## What these controls assert

Two questions with exactly one correct answer, across all four builder branches
that reach the graph (`Repository`, `Directory`, `File`, `Function`):

| control | predicate under test | correct answer |
|---|---|---|
| language `zzz-not-a-language`, unscoped | the language filter, in isolation | 0 rows |
| real language, grant naming only a non-existent repository | the tenancy grant, in isolation | 0 rows |

Each isolates one predicate. The language control runs unscoped so no grant
clause can mask the result; the grant control uses the real language so every
seeded row satisfies it and only the grant can exclude them.

The bound is `limit: 50`, larger than the whole fixture on purpose — a limit
that could truncate would let a leaking query return zero rows for the wrong
reason.

## Status of the underlying defect

**This branch adds the detection, not the fix.** The defect is in the linked
backend, and the cheapest candidate fix is a dependency bump: the same controls
pass on NornicDB **1.2.1** (container `eshu-nornicdb-pr290:3722b483c02c`), which
is why an earlier investigation using that container wrongly concluded the
defect did not reproduce. Confirm the build with `dbms.components()` before
trusting any NornicDB result — the module version (`v1.0.45`) and the reported
product version (`1.0.0`) do not match, so neither number alone identifies it.

These tests are build-tagged `live_nornicdb_language_imports_grant` and no CI
job builds that tag, exactly like the sibling grant tests. They are a tool for a
human with a live backend, not a gate.

No-Regression Evidence: test-only change. No production file is touched, so
there is no runtime path to measure; `verify-performance-evidence.sh` excludes
`*_test.go` before its hot-path match. The added statements reuse the shipped
builders with an explicit limit of 50, so they add no unbounded read.

No-Observability-Change: this change adds no runtime signal and removes none. A
failure surfaces as a test failure naming the shape, the impossible value, the
row count returned, and the exact Cypher executed.
