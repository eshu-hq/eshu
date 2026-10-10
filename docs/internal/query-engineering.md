# Prove production query contracts (#7881)

The query engineering pilot extends `internal/queryplan`'s existing manifest
and production binding. Its guarantee is correctness on declared fixtures and
measured workload budgets. Static checks cannot certify arbitrary semantics or
global optimality.

## Design before coding

Record the result contract and design rationale in the executable registry
before changing a covered family. Name the selected pattern, expected work,
indexes, alternatives, and justified exceptions. Define:

- Authorization boundaries, current versus historical data, null handling,
  duplicates, ordering, pagination, and truthful partial responses.
- Input and output cardinalities, selectivity, skew, traversal depth, fanout,
  payload bytes, number of queries, and workload budgets.
- Execution owner, backend and version, emitted variants, parameter cases,
  schema, migrations, indexes, dataset, independent oracle, and proof runner.

An exception needs its affected family, reason, measured cost and proof. A
sequential scan can be appropriate on small tables or low-selectivity reads.
Do not require one exact plan when several satisfy the contract and budget.
The pilot's `max_result_payload_bytes` bounds canonical JSON for the full
database row array returned by one measured statement execution. It does not
include an HTTP or MCP response envelope. The validator checks every captured
normal sample against this ceiling; the independent oracle separately checks
result identity. Changing the fixture or page shape requires new measurement
and a contract update. `selectivity` describes the declared fixture's filters
and output, not a production-wide estimate. Index-cost notes distinguish
measured read work from unchanged-schema incremental write/storage cost; they
do not claim an unmeasured absolute write or storage benefit. Any accepted
planner exception names its exact emitted variant, cost and evidence artifact.

## Pilot boundary

| Family | Production execution | Required coverage |
|---|---|---|
| Cloud-resource pages | `PostgresCloudResourceListStore.ListCloudResourceIdentities` | 64 emitted SQL variants, 96 parameter cases, both access classes, all filter/cursor masks |
| Import dependencies | `codequery/imports` row readers and handler | 488 valid request shapes, 280 emitted Cypher variants, 282 statement cases, both access classes |

The manifest independently declares pilot membership. Removing a contract or
required case must fail coverage. The generated `coverage.json` reports legacy
and excluded families separately; pilot success is not universal query coverage.
The executable schemas are `go/internal/queryplan/pilot_contract.go` and
`pilot_evidence.go`; manifest entries link YAML contracts under the queryplan package’s testdata directory.

| Manifest family | Backend | Variants | Parameter cases |
|---|---|---:|---:|
| `QP-CLOUD-RESOURCE-IDENTITY-PAGE` | PostgreSQL | 64 | 96 |
| `QP-CODE-IMPORT-CROSS-MODULE-CALLS` | Neo4j | 124 | 124 |
| `QP-CODE-IMPORT-CYCLE-EDGES` | Neo4j | 4 | 4 |
| `QP-CODE-IMPORT-PACKAGES` | Neo4j | 60 | 60 |
| `QP-CODE-IMPORT-ROWS-REPOSITORY` | Neo4j | 28 | 30 |
| `QP-CODE-IMPORT-SOURCE-MODULE-FILES` | Neo4j | 16 | 16 |
| `QP-CODE-IMPORT-SOURCE-MODULE-ROWS` | Neo4j | 32 | 32 |
| `QP-CODE-IMPORT-TARGET-MODULE-FILES` | Neo4j | 16 | 16 |

The initial matrix also reports 19 legacy handler families and 108 excluded
PostgreSQL candidate files. One PostgreSQL file has pilot execution-site proof;
its registered adapter and two other reads have explicit exclusions. These
counts describe the pilot boundary, not validated repository-wide semantics.

## PostgreSQL patterns

Use PostgreSQL 18 semantics. The engine version and image identity in each
artifact identify the actual implementation used. Read
[EXPLAIN](https://www.postgresql.org/docs/18/sql-explain.html) and
[Using EXPLAIN](https://www.postgresql.org/docs/18/using-explain.html) when
interpreting execution plans.

| Pattern | Good | Bad |
|---|---|---|
| Selective access | Typed bound parameters; matching expression or partial index | Concatenate user values into statement text |
| Current truth | Join source facts to the active generation; reject tombstones | Treat every owner-ledger row as current |
| Authorization | Require the allowed scope/repository before the page limit | Limit candidates before applying grants |
| Join and batching | Bound a key batch; declare multiplicity and total query count | Fetch one row per key without a query-count budget |
| Stable page | Order by `(resource_type, uid)` and seek after both keys | Offset an unordered result or omit the unique tie-breaker |
| Aggregate | Filter before grouping; distinguish null and empty values | Count multiplied join rows as distinct entities |
| Index | Match equality, range and ordering; measure the real index | Accept hypothetical cost estimates as execution proof |

For example, `WHERE tenant_id = $1 ORDER BY name, id LIMIT $2` places the
grant before a stable page. `WHERE tenant_id = $1 OR public = true` needs an
explicit public-access contract; it cannot satisfy a private tenant contract.
A matching index speeds reads but consumes storage and adds write work. Record
its size and representative insert/update cost when evaluating an index.

## Cypher patterns

Use the supported Neo4j pin from `docker-compose.neo4j.yml`; record the actual
engine and Cypher version. The profile and methodology runners use the same
supported pin. Read the
[Cypher 25 execution plan reference](https://neo4j.com/docs/cypher-manual/25/planning-and-tuning/execution-plans/)
for plan semantics; verify version-specific features on the selected binary.

| Pattern | Good | Bad |
|---|---|---|
| Selective anchor | Label plus indexed identity and request bounds | Start with an unlabeled whole-graph match |
| Expansion | Directed, bounded traversal from a selective anchor | `[:CALLS*]` with no upper bound |
| Two-endpoint read | Apply the caller grant to both repository aliases | Authorize the source but leak the target repository |
| Stable page | Order by all keys before `SKIP`/`LIMIT` | Apply a limit before an authorization predicate |
| Projection | Return required scalar fields and declared nulls | Return full paths when the response needs only identities |
| Deduplication | Declare entity identity and multiplicity before `DISTINCT` | Hide an accidental Cartesian product with `DISTINCT` |

`MATCH (r:Repository {id: $repo_id}) ... WHERE r.id IN $allowed_repo_ids`
binds the request and caller. A grant mentioned in a comment, projection, or
permissive disjunction does not constrain the read. The parser must consume
the whole accepted pilot grammar and preserve nesting and boolean precedence.
Unsupported covered syntax fails explicitly. Engine parsing is an additional
check; fixture results remain the semantic proof.

## Reproducible evidence

Execute profiles only on disposable databases: `EXPLAIN ANALYZE` and `PROFILE`
execute the statement. Time normal production queries separately from profiles.
PostgreSQL profiling adds overhead and does not measure network transfer.

Each JSON artifact records emitted bytes, safe fixture parameters and hashes;
source, base, candidate and harness commits; engine/configuration;
schema/migration/index identities; dataset version, seed and distribution;
independent expected and actual results; plans, work and repeated timings.

Required parameter cases include nulls, duplicates, empty grants and results,
inaccessible selectors, current/history mixes, tombstones, skewed scopes,
high-degree graphs, large payloads, cursor boundaries and expensive misses.
Check response shaping through HTTP and actual MCP operations. A capped cycle
enumeration can finish with `truncated: true` and `has_more: false`; that is a
partial answer, even though no further page is available.

Capture PostgreSQL query counts, rows, loops, buffers and spills from
`EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON)`. Capture Neo4j operators, database
hits, expansion cardinality and memory where exposed. Record unavailable
metrics with an executable alternate proof. Never turn an unavailable metric
into a fabricated zero.

Pair base and candidate on the same workload, configuration, seed and storage
preparation. Identify each side's schema independently; a deliberate index
change may differ. Alternate repeated samples and retain raw timings. Define
cold preparation explicitly. A first pass after warmups is not a cold-cache
sample. Report warm distributions and noise separately from workload budgets.
Do not require exact equality of nondeterministic work counters.

The initial pilot verifies that base and candidate production query/schema
sources are unchanged, then alternates those same emitted statements on one
disposable fixture. This proves unchanged-query behavior and overhead, with no
speedup claim. A query rewrite needs independently built base and candidate
executables before this runner can supply comparative evidence for that change.
PostgreSQL cold preparation discards plans; it does not evict operating-system
or shared-buffer caches. Neo4j records first-pass and warm samples separately.
The graph runner records actual calls for each of the 488 request shapes and
fails when a request exceeds three graph reads, including repeated identical
statements. Each emitted-statement artifact separately records its one read.

## Enforcement stages

| Stage | Required proof | Activation rule |
|---|---|---|
| Before commit | Deterministic contracts, complete parser, source binding, required cases | Seeded semantic and coverage violations must fail |
| Before push | Targeted disposable PostgreSQL/Neo4j fixture and plan proof | Calibrate runtime and noise, then select on covered query/schema changes |
| Pull request CI | Same targeted proof and archived machine-readable artifacts | Missing, stale or unexercised required evidence fails |
| Merge group CI | Proof on the combined checkout | Demonstrate query-only, schema-only and combined selection |
| Dedicated run | Scale, concurrent HTTP/MCP reads and resource observations | Declare load, topology, budgets and independent result checks |

Preserve current backend support policy and required NornicDB checks. Neo4j
proof is mandatory for supported graph correctness and performance. Each stage
needs observed RED/GREEN and coverage before becoming blocking.

`bash scripts/verify-query-methodology.sh --static` is the pre-commit registry
gate. Its live mode is selected through `query-plan-regression`, which runs
before push and in the `verify-contracts` PR and merge-group job. Both backend
artifacts must validate as a complete set. `scripts/test-verify-query-methodology.sh`
plants absent output and failed producers and exercises the real selector with
query-only, each backend's schema-only, and combined-tree paths.

The dedicated `methodology scale (neo4j)` manual job runs the existing latency
fixture at its default scale with four concurrent workers, twenty requests per
operation and three sequential samples. It archives latency, concurrency,
PostgreSQL work and host/container resource observations. It does not replace
the existing required NornicDB check. Selected parameterized GETs, POSTs and
MCP tool calls validate independent fixture results, including warmups; every
concurrent request contributes its status and result outcome.

## Regression discipline

Every escaped regression adds a missing rule, contract case, fixture or budget.
The #7335 traversal regression escaped because a forbidden operator named
`UnboundedExpand` does not exist in Neo4j. The existing executable checks read
the real expansion details and reject unbounded ranges. This standard extends
that lesson to parsed emitted text and seeded graph evidence.

The #6794 status regression showed that latency alone can miss a large work
increase. Preserve the existing PostgreSQL buffer budgets and their seeded
RED/GREEN tests. New HTTP and MCP operations must reach their seeded query,
validate the response envelope and record per-operation coverage; HTTP 200
does not prove a successful MCP tool result.

Run and retain the planted removed-scope-filter, physically missing index and
unbounded-traversal failures, then their clean passes. Also demonstrate missing
artifact, stale source/query identity and omitted required-case failures.
