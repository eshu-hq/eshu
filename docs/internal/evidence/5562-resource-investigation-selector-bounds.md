# #5562 resource-investigation selector bounds

`POST /api/v0/impact/resource-investigation` no longer resolves a selector with
an unlabeled `MATCH (n)` and one mixed exact-or-substring predicate. Resolution
now runs one direct query per reachable infrastructure label, with the seven
exact properties grouped inside that label. It runs the five bounded substring
properties only when the exact phase returns no candidates and the caller used
`query`; `resource_id` never falls back to substring matching.

Authorization is part of every label query before `LIMIT`. The reads use four
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

The mixed OLD/CURRENT predicate behaved differently across backends. On pinned
NornicDB image `eshu-nornicdb-pr261:149245885258`, it returned the three exact
fixtures but incorrectly omitted the prefix match that its `CONTAINS` clause
claimed to include. On Neo4j 2026.05.0 it mixed that prefix row into the three
exact rows, making an exact selector ambiguous. The final exact phase returns
the same three exact fixtures on both backends; scoped reads return only the two
authorized fixtures, and the fuzzy-only resource appears only after an exact
miss. NornicDB does not return a Bolt PROFILE plan, so its canonical evidence is
output truth plus wall time; no db-hit value is invented.

## Performance Evidence:

Performance Evidence: OLD, CURRENT, and CANDIDATE were measured on the same
200,401-node synthetic graph on the same remote reference machine, with the same
selector, `limit=26`, backend data, and request start/terminal boundaries. OLD
and CURRENT are intentionally identical. Cold is the first read; warm is the
median of the next three reads. The candidate uses the accepted four-slot
fanout. The fuzzy candidate row includes the exact-miss phase and the subsequent
fuzzy phase, matching end-to-end request behavior.

| Backend and phase | Rows | Cold | Warm median | Summed db hits | Bounded operator evidence |
|---|---:|---:|---:|---:|---|
| NornicDB OLD | 3 exact rows; prefix omitted | 1.093031s | 0.000242s | unavailable | PROFILE unavailable |
| NornicDB CURRENT | same as OLD | 1.093031s | 0.000242s | unavailable | PROFILE unavailable |
| NornicDB CANDIDATE exact | 3 exact rows | 0.015931s | 0.000904s | unavailable | direct label queries |
| NornicDB CANDIDATE fuzzy fallback | 1 fuzzy row after exact miss | 0.022883s | 0.001207s | unavailable | direct label queries |
| Neo4j OLD | 3 exact rows plus 1 prefix row | 0.513403s | 0.006073s | 4,442 | `UnionNodeByLabelsScan` |
| Neo4j CURRENT | same as OLD | 0.513403s | 0.006073s | 4,442 | `UnionNodeByLabelsScan` |
| Neo4j CANDIDATE exact | 3 exact rows | 0.367004s | 0.005350s | 3,255 | `NodeByLabelScan` |
| Neo4j CANDIDATE fuzzy fallback | 1 fuzzy row after exact miss | 0.262657s | 0.009867s | 5,686 | `NodeByLabelScan` |

The accepted exact phase reduces Neo4j db hits by 1,187 (26.7%) while keeping
the intended exact output. Fuzzy fallback performs both phases and therefore
uses 3,255 exact-miss hits plus 2,431 fuzzy hits, but it runs only after an exact
miss and returns the row OLD/CURRENT could not represent consistently. Both
backends stay below the 2-second cold ceiling.

Two measured alternatives were rejected. A property-oriented `CALL { UNION }`
fanout was correct on NornicDB but exceeded the cold Neo4j ceiling at 2.500s.
Raising that design from four to seven concurrent sessions regressed a fresh
Neo4j run to 2.229s and increased connection amplification. The accepted design
keeps four sessions and changes the query boundary to one direct read per label;
this is bounded concurrency, not serialization.

The remote run used the accepted Linux amd64 reference profile (16 logical CPUs,
132,209,926,144 bytes visible RAM) and immutable images
`sha256:5627b816bb5e...` for NornicDB and `sha256:dba3898f324b...` for Neo4j.
An unrelated retained Eshu stack remained active, so these targeted numbers are
conservative rather than a clean-host full-corpus baseline. The run directory
basename is `5562-resource-selector-slo`; every disposable proof container and
volume was removed, while the clean detached checkout remains for review
follow-up.

No-Regression Evidence: `TestResourceInvestigationSelectorInteractiveSLO`
enforces the 200,000-noise-node corpus, exact preference, fuzzy-on-miss behavior,
scoped filtering, and the two-second ceiling. The query-plan family registers
all 304 reachable combinations: 38 label/type query shapes across two access
shapes, two environment shapes, and exact/fuzzy phases. Live PROFILE checked all
304 variants in 39.104s and rejected `AllNodesScan`, `CartesianProduct`, and
`UnboundedExpand` for every registered variant.

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
ESHU_RESOURCE_SELECTOR_COMPARISON=1 \
go test -tags resource_selector_slo_live ./internal/query \
  -run '^TestResourceInvestigationSelectorInteractiveSLO$' -count=1 -v
```

Connection values and credentials stay operator-local and are intentionally
omitted.
