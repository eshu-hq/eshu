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

## Pilot boundary

| Family | Production execution | Required coverage |
|---|---|---|
| Cloud-resource pages | `PostgresCloudResourceListStore.ListCloudResourceIdentities` | 64 emitted SQL variants, both access classes, all filter/cursor masks |
| Import dependencies | `codequery/imports` row readers and handler | 488 valid request shapes, 280 emitted Cypher variants, both access classes |

The manifest independently declares pilot membership. Removing a contract or
required case must fail coverage. The generated matrix reports legacy and
excluded families separately; pilot success is not universal query coverage.

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
engine and Cypher version. The existing profile runner's different historical
pin must not be treated as the same environment. Read the
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
