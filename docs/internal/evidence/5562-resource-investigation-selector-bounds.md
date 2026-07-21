# #5562 resource-investigation selector bounds

`POST /api/v0/impact/resource-investigation` no longer resolves a selector with
an unlabeled `MATCH (n)` and one mixed exact-or-substring predicate. Resolution
now runs seven label-anchored exact property reads. It runs the five bounded
substring reads only when those exact reads return no candidates and the caller
used `query`; `resource_id` never falls back to substring matching.

Authorization is part of every label branch before `LIMIT`. The reads use four
concurrent, independent read sessions and merge only after fan-in. There are no
writes, retries, locks, or shared result slices. Duplicate label/property hits
collapse to one stable candidate, while distinct entities with the same exact
selector remain honestly ambiguous.

## Theory and accuracy evidence

The isolated corpus contained 200,000 unrelated `Function` nodes, 36 resources
for each of the 11 supported infrastructure labels, and five collision fixtures.
The fixtures covered two authorized exact-name matches, one unauthorized exact
match, one prefix-only name, and one fuzzy-only name. OLD and CURRENT are the
same query bytes because #5302 changed the traversal reads, not selector
resolution.

On pinned NornicDB image `eshu-nornicdb-pr261:149245885258`, CURRENT filled its
26-row page with unrelated rows, including 21 unrelated
`ArgoCDApplication` nodes and the fuzzy-only resource. The candidate exact phase
returned only the three exact fixtures. Scoped candidate reads returned only the
two authorized exact fixtures, and the fuzzy-only resource appeared only after
an exact miss. NornicDB did not return a Bolt PROFILE plan, so its canonical
evidence is output truth plus wall time; no db-hit value is invented.

The checked-in live gate rebuilds the same synthetic shape and drives the
production resolver. Its fresh NornicDB run completed exact resolution in
`0.018228s` (18.228 ms) and exact-miss plus fuzzy fallback in `0.029801s`
(29.801 ms), both below the `2s` interactive ceiling. It also rechecks scoped
authorization on the full candidate set.

## Performance Evidence:

Performance Evidence: OLD, CURRENT, and CANDIDATE were measured on the same
200,401-node synthetic graph, with the same selector, `limit=26`, backend data,
and single request start/terminal boundaries. OLD and CURRENT are intentionally
identical. Cold is the first read; warm is the median of the next three reads.
The candidate figures use the accepted four-slot fanout.

| Backend and phase | Rows | Cold | Warm median | Summed db hits | Bounded operator evidence |
|---|---:|---:|---:|---:|---|
| NornicDB OLD | polluted 26-row page | 0.687259s | 0.000670s | unavailable | PROFILE unavailable |
| NornicDB CURRENT | polluted 26-row page | 0.687259s | 0.000670s | unavailable | PROFILE unavailable |
| NornicDB CANDIDATE exact | 3 exact rows | 0.031760s | 0.005239s | unavailable | per-label branches |
| NornicDB CANDIDATE fuzzy | 1 fuzzy row after exact miss | 0.016772s | 0.001998s | unavailable | per-label branches |
| Neo4j OLD | exact, prefix, fuzzy, and unrelated matches | 0.020847s | 0.005752s | 4,442 | `UnionNodeByLabelsScan` |
| Neo4j CURRENT | same as OLD | 0.020847s | 0.005752s | 4,442 | `UnionNodeByLabelsScan` |
| Neo4j CANDIDATE exact | 3 exact rows | 1.074118s | 0.009131s | 5,757 | `NodeByLabelScan`, `Union` |
| Neo4j CANDIDATE fuzzy | 1 fuzzy row after exact miss | 0.947973s | 0.007314s | 4,087 | `NodeByLabelScan`, `Union` |

The candidate does more summed compatibility-backend db hits because it proves
seven independent exact properties instead of accepting one mixed predicate's
incorrect page. Accuracy is the required delta: the old answer is not a valid
performance baseline. The candidate remains below the 2-second compatibility
ceiling in the same-data cold PROFILE proof and is substantially faster on the
canonical NornicDB target.

The cap-7 alternative was rejected. On a warm same-data Neo4j sweep it improved
the median from 24.058 ms to 15.315 ms, but a fresh run regressed to 2.229s and
amplified each request to seven simultaneous connections. The accepted cap stays
at four; this is bounded concurrency, not serialization.

No-Regression Evidence: `TestResourceInvestigationSelectorInteractiveSLO`
enforces the 200,000-noise-node corpus, exact preference, fuzzy-on-miss behavior,
scoped filtering, and the two-second ceiling. The query-plan family registers
all 336 reachable combinations: seven label/type shapes, two access shapes, two
environment shapes, and all seven exact plus five fuzzy properties. Live PROFILE
policy rejects `AllNodesScan`, `CartesianProduct`, and `UnboundedExpand` for
every registered variant.

## Observability Evidence:

Observability Evidence: the existing `query.resource_investigation` handler span
now records bounded, low-cardinality selector attributes:

- `eshu.resource_investigation.selector_seconds`
- `eshu.resource_investigation.selector_phase` (`exact` or `fuzzy`)
- `eshu.resource_investigation.selector_candidate_count`
- `eshu.resource_investigation.selector_ambiguous`
- `eshu.resource_investigation.selector_truncated`

The attributes contain no selector value, resource identity, repository ID, or
credential. Each bounded graph read still emits the existing `neo4j.query` span.
The telemetry regression test records and checks latency, phase, count,
ambiguity, and truncation on an in-memory span recorder.

## Verification

```bash
cd go
go test ./internal/query -count=1
go test ./internal/queryplan -run '^TestHotCypherManifestCoversEveryProductionQueryCall$' -count=1
go test -tags resource_selector_slo_live ./internal/query -run '^$' -count=1
```

Target-backend live gate (isolated disposable graph):

```bash
ESHU_RESOURCE_SELECTOR_SLO_LIVE=1 \
ESHU_RESOURCE_SELECTOR_ISOLATED=1 \
ESHU_RESOURCE_SELECTOR_FRESH=1 \
go test -tags resource_selector_slo_live ./internal/query \
  -run '^TestResourceInvestigationSelectorInteractiveSLO$' -count=1 -v
```

Connection values and credentials stay operator-local and are intentionally
omitted.
