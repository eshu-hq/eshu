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

The bound is `limit: 50`, and it is **not load-bearing**. An earlier version of
this note, and of the constant's own comment, claimed it was: "a limit that could
truncate would let a leaking query return zero rows for the wrong reason". That
reasoning is backwards. With any limit >= 1, a leaking query matching k >= 1 rows
returns min(limit, k) >= 1, so `len(rows) != 0` still fails as intended —
truncation cannot manufacture the zero-row pass it warned about.

The limit exists to satisfy the call contract's required-bound rule. The real
vacuous-pass risk is an empty or unreachable fixture, which no choice of limit
affects.

## Status of the underlying defect

**This branch adds the detection, not the fix.** The defect is in the linked
backend, and the cheapest candidate fix is a dependency bump: the same controls
pass on NornicDB **1.2.1** (container `eshu-nornicdb-pr290:3722b483c02c`), which
is why an earlier investigation using that container wrongly concluded the
defect did not reproduce. Confirm the build with `dbms.components()` before
trusting any NornicDB result — the module version (`v1.0.45`) and the reported
product version (`1.0.0`) do not match, so neither number alone identifies it.

These tests are build-tagged `live_nornicdb_language_imports_grant`, exactly like
the four sibling grant tests in this package. CI **compiles** that tag but does
not **run** it. An earlier version of this note said no CI job built the tag at
all; that was wrong, and the distinction matters to anyone judging what this file
actually protects.

### Which backend these tests can actually reach

Raised in review of this change, and it bounds every test in this suite, not just
the two controls added here.

The shared helper `openLiveGrantDriver`
(`go/internal/query/language_query_grant_nornicdb_live_test.go`) connects with
`neo4jdriver.NoAuth()`. The **embedded** NornicDB runtime does not accept that:
`internal/cli/localsupervisor/graph_embedded_nornicdb.go` loads or generates
credentials (`:57`) and starts Bolt with `boltConfig.RequireAuth = true`
(`:179`).

So `ESHU_NEO4J_URI` must point at a backend that accepts unauthenticated Bolt —
in practice the pinned NornicDB container — and these tests **cannot** exercise
the embedded runtime as configured. This is pre-existing: the helper predates
this branch (last touched 2026-09-05) and is shared by all five tagged files in
this package; the new controls simply inherit it. It is recorded here rather than
fixed, because changing the helper's auth would alter what every sibling test
connects to.

It also reinforces the version point below: the embedded 1.0.0 path is not what
this suite runs against.

`scripts/verify-tagged-builds.sh` sweeps this file automatically: it "reads the
constraints out of the files rather than from a hand-maintained list", and its
`--all` mode walks every directory under `go/` holding a `//go:build` file, so a
new tag needs no registration. Verified — the constraint sweep matches five files
in `internal/query` including this one, and
`go vet -tags live_nornicdb_language_imports_grant ./internal/query` exits 0.

That gate is deliberately credential-free ("it compiles, it does not run
anything"), so these assertions never pass or fail in a pipeline. Executing them
needs a human with a live backend and `ESHU_NEO4J_URI` set. The compile sweep is
still load-bearing: it exists because #5167's `live_nornicdb_complexity_grant`
suite lost `ptrToCodeGrantAuthContext` to an unrelated refactor and sat
uncompilable through a `make pre-pr` run, a push, a full CI run and eight review
rounds.

No-Regression Evidence: test-only change. No production file is touched, so
there is no runtime path to measure; `verify-performance-evidence.sh` excludes
`*_test.go` before its hot-path match. The added statements reuse the shipped
builders with an explicit limit of 50, so they add no unbounded read.

No-Observability-Change: this change adds no runtime signal and removes none. A
failure surfaces as a test failure naming the shape, the impossible value, the
row count returned, and the exact Cypher executed.
