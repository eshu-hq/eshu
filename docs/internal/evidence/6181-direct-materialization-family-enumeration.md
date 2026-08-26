# 6181 — enumerating the direct-materialization edge families

## What changed

`go/internal/reducer/materialized_edge_families.go` gains a package-level map,
`directMaterializedEdgeFamilyByPort` (28 entries), and one pure function,
`DirectMaterializedEdgeFamilies()`, which returns its deduplicated values
sorted. The rest of the diff in that file is documentation.

The Ifá `materialized_edges:<family>` exhaustiveness gate previously inventoried
only the SHARED half of the reducer's materialized-edge surface — the domains
reaching the graph through the shared-projection intent path. The reducer also
writes edges DIRECTLY, one family per port straight to a
`go/internal/storage/cypher` writer with no intent row in between, and the gate
was blind to all of them. This enumeration is the missing half.

## No-Regression Evidence:

This change adds no production code path, so there is nothing to regress.

VERIFIED — every caller of the new function is a test:

```
rg -n 'DirectMaterializedEdgeFamilies' --glob '*.go'
```

returns `go/internal/ifa/materializededges/*_test.go` (five files) plus one
comment reference in `materialized_edges.go:38`. No non-test production caller
exists. The gate that consumes it runs under `go test`, not under the reducer.

VERIFIED — the existing exported behaviour is untouched. Restricting the diff
of that file to non-comment lines yields **zero removed lines** and no change to
`MaterializedEdgeFamilies()`:

```
git diff origin/main...HEAD -- go/internal/reducer/materialized_edge_families.go \
  | rg '^-' | rg -v '^-\s*//' | rg -v '^---' | rg -v '^-\s*$'
```

REASONED, not measured — the one cost production does pay is building a
28-entry `map[string]string` literal once at package initialisation of
`go/internal/reducer`. It is not on any request, query, projection, or write
path, is not rebuilt per batch, and is bounded by a constant that only changes
when a direct-materialization port is added. No benchmark is cited because
there is no per-operation cost to measure; a package-init map literal of fixed
size is not a throughput or latency surface.

Backend/version: not applicable — no Cypher, no query plan, no schema, and no
graph or Postgres statement changes in this diff. Terminal row and queue counts
are unchanged because no statement is issued.

## No-Observability-Change:

Nothing operator-facing moves, and that is the honest answer rather than a
metric invented to fill this section.

The new function emits no metric, span, or log, and is never reached by a
running service — only by the gate's tests. No counter, dashboard, alert, or
runbook reads a value that changes, because no production code calls it.

Accepting no new telemetry here is reasonable because the change adds no new
runtime failure mode to observe. What it adds is a build-time completeness
check, and the thing worth watching is whether the ledger and the code disagree
— which is exactly what the gate asserts, loudly, at test time.

## Why the change is safe

The enumeration is a declaration, not a behaviour. If it is wrong, the failure
is a red gate, not a wrong graph: a family listed here with no ledger row fails
the coverage test, and a ledger row naming a family neither enumeration knows
fails as a stale row. Both directions are pinned by tests in
`go/internal/ifa/materializededges/`.

The waiver rows it introduces are held to the issue that retires them
(`#6228`). Seeding a stale pointer is rejected — repointing a waiver at the
closed `#5543` turns
`TestMaterializedEdgeWaiversNameTheIssueThatRetiresThem` red with
"closing #5543 would remove nothing here" — so the ledger cannot quietly
accumulate waivers pointing at work that will never retire them.
