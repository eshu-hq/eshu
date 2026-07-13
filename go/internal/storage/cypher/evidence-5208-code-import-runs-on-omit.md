# Evidence: omit impossible code-import RUNS_ON retracts (#5208)

## Decision

Implement the source-capability omission for `projection/code-imports`. Defer
the indexed `WorkloadInstance.repo_id` rewrite.

The accepted 896-repository run spent `362.94s` (`6m02.94s`) executing a
`RUNS_ON` retract for a producer that only emits repo-to-repo `DEPENDS_ON`.
That measured critical-path time exceeds the `158s` worthwhile-work threshold
by `204.94s` (`3m24.94s`). A conservative expected saving of `300s` still
clears the threshold by `142s`.

The indexed rewrite did not clear the same gate. On the quiet retained graph,
ten alternating reads averaged `1.030ms` for the shipped traversal and
`0.471ms` for the indexed candidate.

## Retained data

The proof used the terminal graph and Postgres state from the same merged-code
896-repository run that established the #5208 tail.

| Data surface | Cardinality |
| --- | ---: |
| Repositories | 896 |
| Completed `repo_dependency` intents | 2,414 |
| `projection/code-imports` intents | 838 across 838 repositories |
| `resolver/cross-repo` intents | 1,576 across 896 repositories |
| Retained `RUNS_ON` relationships | 277 |
| `RUNS_ON` with `evidence_source=projection/code-imports` | 0 |

The cleanest worst-scope proxy was one repository with exactly two completed
retract intents, one for each evidence source. Its cycle-start gap was
`37.305406s`. The successful resolution-engine container log was not retained
after a later controlled service recreate, so this gap selects the scope but
does not attribute seconds to either individual statement. The accepted
`362.94s` source aggregate remains the timing baseline.

## Query shapes

The shipped single-repository `RUNS_ON` retract is:

```cypher
MATCH (repo:Repository {id: $repo_id})-[:DEFINES]->(w:Workload)
MATCH (i:WorkloadInstance)-[:INSTANCE_OF]->(w)
MATCH (i)-[rel:RUNS_ON]->(:Platform)
WHERE rel.evidence_source = $evidence_source
DELETE rel
```

The indexed candidate was tested with the delete replaced by relationship-ID
readback:

```cypher
MATCH (i:WorkloadInstance {repo_id: $repo_id})-[rel:RUNS_ON]->(:Platform)
WHERE rel.evidence_source = $evidence_source
RETURN id(rel) AS rel_id
```

The implemented code-import shape is narrower: no `RUNS_ON` query or graph
transaction is opened for the exact source `projection/code-imports`.
Repository relationship and evidence-artifact cleanup still execute in their
existing sequential order. Every other evidence source keeps the shipped
three-role retract.

## Timing

Performance Evidence: on the accepted 896-repository run, the exact
`projection/code-imports` `RUNS_ON` arm consumed `362.94s`; the new exact-source
shape omits all `838` impossible calls. The retained graph contained zero
code-import `RUNS_ON` relationships, and the production-writer proof and B-7
gate preserved graph output.

| Measurement | Old | Candidate | Delta |
| --- | ---: | ---: | ---: |
| Accepted code-import `RUNS_ON` aggregate | 362.94s | 0s | 362.94s maximum recoverable |
| Conservative end-to-end estimate | 362.94s measured | omitted | 300-360s expected |
| Quiet retained graph, 10 alternating reads | 1.030ms average | 0.471ms average | 0.559ms |
| Worst-scope read-only `PROFILE` | 29.971ms | 8.939ms | 21.032ms |

NornicDB accepted `EXPLAIN` and `PROFILE`, but its transactional HTTP response
did not return plan/profile metadata. The table therefore reports client wall
time and returned row counts. The millisecond result is a no-go for the indexed
rewrite. It does not invalidate source omission: omission removes the entire
write transaction and any backend wait it would incur during the active tail.

The repo-dependency runner holds one global lease at partition `0/1`, and its
cycles are serialized. The measured statement-duration sum is critical-path
time, not parallel work counted more than once. Worker count, partitioning,
lease behavior, retries, and remaining statement transaction boundaries do not
change.

Observability Evidence: each omission emits the bounded structured log
`shared edge retract role omitted` with the projection domain, exact evidence
source, omitted statement role, repository count, and
`reason=source_capability`. It also increments
`eshu_dp_shared_edge_runs_on_retract_omissions_total` with bounded `domain` and
`reason` labels. Existing duration logs remain unchanged for every statement
that still executes.

## Exactness

| Check | Old | Candidate | Diff/result |
| --- | ---: | ---: | --- |
| All retained relationship identities | 277 | 277 | old-new `0`, new-old `0` |
| Raw relationship rows | 277 | 277 | equal |
| Unique relationship rows | 277 | 277 | equal |
| Duplicate rows | 0 | 0 | equal |
| Worst code-import scope | 0 | 0 | exact empty case |
| Global code-import `RUNS_ON` | 0 | 0 | omission is exact |
| Prefix source `projection/code-imports-extra` | 3 retract roles | 3 retract roles | no prefix collision |

The producer contract is also closed in code. Code-import correlation builds
deduplicated repo-to-repo `DEPENDS_ON` intents, drops self-references, and uses
deterministic intent IDs for replay. The graph writer now rejects any exact
`projection/code-imports` write whose relationship type is neither empty
(legacy `DEPENDS_ON`) nor `DEPENDS_ON`. A malformed `RUNS_ON` row fails before
backend execution.

A disposable local NornicDB proof used the production `EdgeWriter` to seed:

- one code-import `DEPENDS_ON`;
- one `resolver/cross-repo` `RUNS_ON` on the same repository workload;
- one code-import evidence artifact.

Two consecutive code-import retracts produced the same terminal state:

| Graph assertion | After retract 1 | After retract 2 |
| --- | ---: | ---: |
| Code-import `DEPENDS_ON` | 0 | 0 |
| Code-import evidence artifact relationship | 0 | 0 |
| Other-source `RUNS_ON` | 1 | 1 |

This proves source isolation and idempotent replay on the real backend. Endpoint
nodes survive because the existing relationship retract and artifact cleanup
semantics are unchanged.

The full B-7 golden-corpus gate also passed on an isolated local stack in
`32s`: `417` checks passed, with `0` required failures and `0` advisory
warnings. Its graph assertions retained both expected `RUNS_ON` relationships
and all five expected `DEPENDS_ON` relationships, and its query, API, and MCP
surfaces remained green.

## Commands

Representative retained-data commands, with operator-local connection details
removed:

```bash
gh issue view 5208 --repo eshu-hq/eshu \
  --json number,title,state,body,comments,updatedAt,url

# Read-only NornicDB transactional HTTP queries.
curl -fsS -H 'Content-Type: application/json' \
  -d "$payload" "$NORNIC_HTTP/db/nornic/tx/commit"

# Retained intent cardinality and source capability.
psql "$POSTGRES_DSN" -c "
SELECT payload->>'evidence_source', count(*),
       count(DISTINCT repository_id),
       count(*) FILTER (WHERE completed_at IS NOT NULL)
FROM shared_projection_intents
WHERE projection_domain='repo_dependency'
GROUP BY 1 ORDER BY 1;"

psql "$POSTGRES_DSN" -c "
SELECT payload->>'evidence_source',
       COALESCE(payload->>'relationship_type','<none>'),
       COALESCE(payload->>'action','<upsert>'), count(*)
FROM shared_projection_intents
WHERE projection_domain='repo_dependency'
GROUP BY 1,2,3 ORDER BY 1,2,3;"

# TDD and package proof.
go test ./internal/storage/cypher -run \
  'TestEdgeWriterRetractEdgesRepoDependency|TestEdgeWriterWriteEdgesRejectsCodeImportRunsOn|TestRepoDependencyRetractSummariesShareRelationshipEdgeTypes' \
  -count=1
go test ./internal/storage/cypher ./internal/reducer -count=1
go test -race ./internal/storage/cypher -run \
  'TestEdgeWriterCodeImportRetractRecordsOnlyExactSourceOmissionTelemetry|TestEdgeWriterRetractEdgesRepoDependency|TestEdgeWriterWriteEdgesRejectsCodeImportRunsOn|TestRepoDependencyRetractSummariesShareRelationshipEdgeTypes' \
  -count=5

# Telemetry contract and documentation proof.
go test ./internal/telemetry -count=1
bash scripts/test-verify-telemetry-coverage.sh
ESHU_TELEMETRY_COVERAGE_BASE=origin/main \
  bash scripts/verify-telemetry-coverage.sh
uv run --with mkdocs --with mkdocs-material --with pymdown-extensions \
  mkdocs build --strict --clean --config-file docs/mkdocs.yml

# Real NornicDB production-writer proof. The DSN points to a disposable local
# instance; the test cleans its seeded nodes and relationships.
ESHU_CYPHER_BOLT_DSN="$BOLT_DSN" \
ESHU_CYPHER_BOLT_DATABASE=nornic \
go test ./internal/storage/cypher -run \
  '^TestBoltRepoDependencyCodeImportRetractPreservesOtherSourceRunsOn$' \
  -count=3 -v

# Isolated B-7 golden-corpus proof. Ports and the Compose project were unique
# to this run; the gate removed its stack on exit.
COMPOSE_PROJECT_NAME=eshu5208golden \
NORNICDB_IMAGE=eshu-nornicdb-pr261:149245885258 \
NORNICDB_PULL_POLICY=never \
ESHU_POSTGRES_PORT=45732 \
NEO4J_BOLT_PORT=45687 \
NEO4J_HTTP_PORT=45747 \
GATE_API_PORT=45880 \
GATE_MCP_PORT=45891 \
GATE_DRAIN_TIMEOUT=10m \
GATE_BUDGET_SECONDS=900 \
bash scripts/verify-golden-corpus-gate.sh
# Result: 417 pass, 0 required failures, 0 advisory warnings, 32s.
```

## Recommendation and next route

Land the code-import omission. Do not include the indexed rewrite in the same
change. On the next comparable required profile, confirm the remaining
`resolver/cross-repo` `RUNS_ON` time. Reopen the indexed candidate only if that
remaining contribution is still materially above the target gap.

This change should save about five to six minutes on the measured tail. It is
not enough by itself to claim a new full-drain target or a separate under-20
path.
