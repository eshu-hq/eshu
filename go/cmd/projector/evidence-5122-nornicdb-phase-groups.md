# Evidence: projector NornicDB phase-group parity (#5122)

## Decision and change shape

The shipped standalone projector sends the complete canonical materialization
through one `GroupExecutor` transaction. On the retained worst source-local
scope, NornicDB cannot complete that transaction within either the accepted
remote-E2E 120-second profile or a bounded 600-second diagnostic budget.

The candidate gives the projector the same NornicDB `PhaseGroupExecutor` used
by the ingester. It changes transaction boundaries, not materialization
construction: repository, directory, directory-edge, file, and entity phases
commit independently, while entity chunks retain their configured fanout. The
inner process-wide graph gate remains below that fanout. The measured profile
keeps two projector workers, two graph permits, the accepted remote-E2E
120-second timeout, and the default entity-phase concurrency. No worker
reduction, batch-size-one fallback, or whole-route serialization is introduced.

## Proof environment and retained data shape

- Eshu old route: commit
  `340045d8f3f23dc2e87a91ff30ed537ee7026f7a`, binary SHA-256
  `ace6c12f926c1b96f58c63781706cb81eaafe60013e739a0430a5f5825c64b67`.
- Eshu measured candidate code commit
  `6aa32cba1517c4015740b3d54400dc08c43cad57`, binary SHA-256
  `b6510d21fe7efab12e58ac7f0491924c74fe9d508f590aca63398d1e2e0500b1`.
- After the measured candidate, production Go commit
  `92b62e930374e64e8a2eb7fc353930a64f8f1603` adds the drain-timeout retry
  classification and its focused tests. The only production Go delta changes
  the error returned after a drain timeout; successful phase execution does
  not enter that branch, so the measured candidate timing remains
  representative. Commit `4ff7ba3ec173700e3f0282cbca318f51bd85dbfe`
  then adds the exact-source Compose validation, a file-content regression
  test, docs, and evidence. Final-head focused writer and queue lifecycle tests
  prove the timeout still reaches `projection_retryable`.
- Representative retained-scope NornicDB: v1.1.11 plus the exact-key
  transaction-lock patch from public PR orneryd/NornicDB#261. That patch is a
  rollout dependency until it is merged, tagged, pinned by Eshu, and the
  retained proof is rerun on the immutable production artifact.
- Correctness oracle: `neo4j:2026-community`, image digest
  `sha256:6c162e2432f861f2c4e3da77a6ba478e7f10e2160b870541f85294532bc6ff5f`.
- Same local machine, process resource envelope, copied Postgres facts, and
  projector profile for old and new NornicDB runs: arm64 Apple M5 Max, 18
  physical/logical CPUs, 128 GiB host memory, macOS 26.5.1 (25F80), OrbStack
  kernel 7.0.11 with overlayfs, 18-CPU/62.75-GiB shared container pool. The
  proof Postgres and graph containers had no per-container CPU, cpuset, or
  memory limit. Projector processes used `GOMAXPROCS=16` and
  `GOMEMLIMIT=48GiB`.
- Patched NornicDB image SHA-256
  `e0056d66e69887bfacf3714489da2a60d4d813a7c5f17ccba099393fb32e6031`;
  stock v1.1.11 image SHA-256
  `51b6174ae65e4ce54a158ac2f9eace7d36a1971545824d22add0fe06d94c1090`;
  Neo4j oracle image SHA-256
  `6c162e2432f861f2c4e3da77a6ba478e7f10e2160b870541f85294532bc6ff5f`.
- One anonymized retained worst-case source-local scope: 106,432 fact records,
  3,592 content files, 98,242 content entities, 18,599 built reducer intents,
  1 repository, 522 directories, 3,592 canonical files, and 68,166 canonical
  entities. The ordered retained fact-content hash matched its source before
  the proof.
- Every admissible fresh-graph run started with zero nodes and the complete
  production NornicDB schema (174 online indexes). An intentionally discarded
  fixture launch without those indexes demonstrated why schema readiness is a
  prerequisite; it is not included in the timing table.

The retained database and every graph used below were left running after the
measurements.

## Commands

The runtime commands below are sanitized. `${PROOF_DSN}`, `${BOLT_URI}`,
`${OLD_PROJECTOR}`, and `${NEW_PROJECTOR}` refer to isolated local proof
resources; no source repository path or raw payload is required.

```bash
GOMAXPROCS=16 GOMEMLIMIT=48GiB \
ESHU_GRAPH_BACKEND=nornicdb NEO4J_URI="${BOLT_URI}" \
ESHU_POSTGRES_DSN="${PROOF_DSN}" ESHU_CONTENT_STORE_DSN="${PROOF_DSN}" \
ESHU_PROJECTOR_WORKERS=2 ESHU_GRAPH_WRITE_MAX_IN_FLIGHT=2 \
ESHU_CANONICAL_WRITE_TIMEOUT=120s "${OLD_PROJECTOR}"

GOMAXPROCS=16 GOMEMLIMIT=48GiB \
ESHU_GRAPH_BACKEND=nornicdb NEO4J_URI="${BOLT_URI}" \
ESHU_POSTGRES_DSN="${PROOF_DSN}" ESHU_CONTENT_STORE_DSN="${PROOF_DSN}" \
ESHU_PROJECTOR_WORKERS=2 ESHU_GRAPH_WRITE_MAX_IN_FLIGHT=2 \
ESHU_CANONICAL_WRITE_TIMEOUT=120s "${NEW_PROJECTOR}"
```

The old-route diagnostic changed only the timeout to `600s`; it did not change
workers or graph permits. The partial-phase recovery proof used a fresh graph,
ran the candidate once with `ESHU_CANONICAL_WRITE_TIMEOUT=10ms`, waited for the
backend to return to idle, then reset only the projector lifecycle row and ran
the same candidate at `120s` on that partially populated graph. That manual
reset proves graph-write idempotence after a late partial commit; it does not
prove the production queue automatically classified the timeout as retryable.
Review caught that the original drain-timeout wrapper returned a plain error.
The final candidate returns `GraphWriteTimeoutError`, and focused writer and
queue regressions prove the error reaches the existing `projection_retryable`
lifecycle path instead of terminal-failing attempt one.

The final B-7 controls used fresh Compose projects and volumes. The commands
differed only in isolated project/port values and `NORNICDB_IMAGE`:

```bash
/usr/bin/time -p env COMPOSE_PROJECT_NAME=eshu5122goldenstockfresh \
NORNICDB_IMAGE='timothyswt/nornicdb-cpu-bge:v1.1.11@sha256:51b6174ae65e4ce54a158ac2f9eace7d36a1971545824d22add0fe06d94c1090' \
ESHU_POSTGRES_PORT=25232 NEO4J_HTTP_PORT=27274 NEO4J_BOLT_PORT=27287 \
GATE_API_PORT=28280 GATE_MCP_PORT=28291 \
bash scripts/verify-golden-corpus-gate.sh --keep

/usr/bin/time -p env COMPOSE_PROJECT_NAME=eshu5122goldenpatched \
NORNICDB_IMAGE='nornicdb:5122-exact-lock-local' \
ESHU_POSTGRES_PORT=25332 NEO4J_HTTP_PORT=27374 NEO4J_BOLT_PORT=27387 \
GATE_API_PORT=28380 GATE_MCP_PORT=28391 \
bash scripts/verify-golden-corpus-gate.sh --keep
```

Continued validation also built orneryd/NornicDB#261 directly from its full
source commit through `docker-compose.nornicdb-pr261.yaml`. The built image
reported OCI revision `1492458852588c884c32f70d27ea2ee07086769c`; Compose
resolved the same full source commit, `docker/Dockerfile.cpu-bge`, local tag,
and `pull_policy: never`. A fresh isolated B-7 run used that cached image:

```bash
docker compose -f docker-compose.yaml \
  -f docker-compose.nornicdb-pr261.yaml build nornicdb

COMPOSE_PROJECT_NAME=eshu5122pr261proof3 \
ESHU_POSTGRES_PORT=26434 NEO4J_HTTP_PORT=30474 NEO4J_BOLT_PORT=30687 \
docker compose -f docker-compose.yaml \
  -f docker-compose.nornicdb-pr261.yaml \
  up -d --no-build nornicdb postgres

# --no-compose requires psql. This proof routed it through the retained
# Postgres container because the host had no psql client.
psql() {
  local args=("$@")
  if [[ "${args[0]:-}" == postgresql:* ]]; then
    args=("${args[@]:1}")
  fi
  docker exec -i eshu5122pr261proof3-postgres-1 \
    psql -U eshu -d eshu "${args[@]}"
}
export -f psql

ESHU_POSTGRES_PORT=26434 NEO4J_HTTP_PORT=30474 NEO4J_BOLT_PORT=30687 \
GATE_API_PORT=31080 GATE_MCP_PORT=31091 \
GATE_COLLECTOR_SETTLE_SECONDS=60 \
scripts/verify-golden-corpus-gate.sh --no-compose --keep
```

The exact-source run finished in `81s` pipeline wall time with `416/0/1`
(pass/required-fail/advisory-warn), zero residual or dead-letter work, and zero
nonterminal shared projection intents. The sole warning was the intentional
60-second collector settle versus the 20-second timing baseline; graph, API,
MCP, correlation, drain, and required timing gates all passed. The proof
Compose project and work directory remain retained.

The backend-oracle comparison queried complete node labels/properties
and relationship endpoint labels/properties, relationship types, and
relationship properties. Integral numeric representations were normalized
because Neo4j returns selected numeric properties as `1.0` while NornicDB
returns `1`. Sorted streams were compared in both directions with `comm`.

```bash
ORACLE_HTTP="${ORACLE_HTTP_BASE}/db/neo4j/tx/commit"
CANDIDATE_HTTP="${CANDIDATE_HTTP_BASE}/db/nornic/tx/commit"
NODE_QUERY='MATCH (n) RETURN labels(n) AS labels, properties(n) AS props'
EDGE_QUERY='MATCH (a)-[r]->(b) RETURN labels(a) AS source_labels, properties(a) AS source_props, type(r) AS rel_type, properties(r) AS rel_props, labels(b) AS target_labels, properties(b) AS target_props'

curl -fsS -u "${ORACLE_AUTH}" -H 'Content-Type: application/json' \
  -d "$(jq -nc --arg query "${NODE_QUERY}" '{statements:[{statement:$query}]}')" \
  "${ORACLE_HTTP}" > oracle-nodes.json
curl -fsS -H 'Content-Type: application/json' \
  -d "$(jq -nc --arg query "${NODE_QUERY}" '{statements:[{statement:$query}]}')" \
  "${CANDIDATE_HTTP}" > candidate-nodes.json
curl -fsS -u "${ORACLE_AUTH}" -H 'Content-Type: application/json' \
  -d "$(jq -nc --arg query "${EDGE_QUERY}" '{statements:[{statement:$query}]}')" \
  "${ORACLE_HTTP}" > oracle-edges.json
curl -fsS -H 'Content-Type: application/json' \
  -d "$(jq -nc --arg query "${EDGE_QUERY}" '{statements:[{statement:$query}]}')" \
  "${CANDIDATE_HTTP}" > candidate-edges.json

jq -S -c '.results[0].data[].row
  | walk(if type == "number" and . == floor then floor else . end)
  | [(.[0] | sort), .[1]]' oracle-nodes.json | sort > oracle-nodes.jsonl
jq -S -c '.results[0].data[].row
  | walk(if type == "number" and . == floor then floor else . end)
  | [(.[0] | sort), .[1]]' candidate-nodes.json | sort > candidate-nodes.jsonl
jq -S -c '.results[0].data[].row
  | walk(if type == "number" and . == floor then floor else . end)
  | [(.[0] | sort), .[1], .[2], .[3], (.[4] | sort), .[5]]' \
  oracle-edges.json | sort > oracle-edges.jsonl
jq -S -c '.results[0].data[].row
  | walk(if type == "number" and . == floor then floor else . end)
  | [(.[0] | sort), .[1], .[2], .[3], (.[4] | sort), .[5]]' \
  candidate-edges.json | sort > candidate-edges.jsonl

comm -23 oracle-nodes.jsonl candidate-nodes.jsonl | wc -l
comm -13 oracle-nodes.jsonl candidate-nodes.jsonl | wc -l
comm -23 oracle-edges.jsonl candidate-edges.jsonl | wc -l
comm -13 oracle-edges.jsonl candidate-edges.jsonl | wc -l
wc -l oracle-nodes.jsonl candidate-nodes.jsonl \
  oracle-edges.jsonl candidate-edges.jsonl
shasum -a 256 oracle-nodes.jsonl candidate-nodes.jsonl \
  oracle-edges.jsonl candidate-edges.jsonl
```

## Performance Evidence:

| Route and profile | Canonical write | Project generation | Total | Result |
| --- | ---: | ---: | ---: | --- |
| Old NornicDB, 120s, attempt 1 | `>120s` | n/a | `121.744308125s` | timeout |
| Old NornicDB, 120s, attempt 2 | `>120s` | n/a | `121.763630834s` | timeout |
| Old NornicDB, 600s diagnostic | `>600s` | n/a | `602.880474083s` | timeout |
| Final NornicDB candidate, fresh graph, 120s | `11.650580875s` | `17.249290125s` | `18.6759285s` | success, attempt 1 |
| Final rebased immutable candidate, clean fresh indexed graph, 120s | `16.027548458s` | `23.503147708s` | `25.273791334s` | success, attempt 1 |
| Final NornicDB candidate, populated graph, 120s | `28.680403666s` | `41.506617625s` | `42.940914458s` | idempotent success |
| New NornicDB, partial graph recovery, 120s | `10.149351333s` | `15.450381125s` | `16.835510834s` | recovery success |
| Old Neo4j correctness oracle, 120s | `10.340724375s` | `16.329392625s` | `17.875696875s` | success |

The immutable rebased fresh run saves at least `96.470516791s` against one
shipped 120-second failed attempt. The accepted retained failure consumed three
such attempts, so replacing that terminal straggler plausibly recovers about
`334.726s` (`360s - 25.274s`), or 5 minutes 35 seconds. That is about
`176.726s` of margin over the current 158-second gap. The realistic expected
saving is therefore about 96 seconds for a one-attempt failure and 335 seconds
for the observed three-attempt failure, excluding retry backoff.

This is same-machine relative proof. The fresh and recovery runs are under 30
seconds locally, but the final populated-graph idempotence run is 42.94 seconds and no
reference-machine full-corpus candidate has run yet. Do not present this alone
as the absolute under-30 end-to-end path.

## Exactness and idempotence

| Check | Old oracle | Candidate | Difference |
| --- | ---: | ---: | ---: |
| Loaded facts | 106,432 | 106,432 | 0 |
| Canonical nodes | 72,281 | 72,281 | 0 |
| Canonical relationships | 75,869 | 75,869 | 0 |
| Normalized node old-only / new-only | - | - | `0 / 0` |
| Normalized relationship old-only / new-only | - | - | `0 / 0` |
| Duplicate node identity groups / extra nodes | - | `0 / 0` | 0 |
| Duplicate relationship groups / extra relationships | - | `0 / 0` | 0 |
| Self relationships | - | 0 | 0 |
| Content files / entities | 3,592 / 98,242 | 3,592 / 98,242 | `0 / 0` |

The rebased candidate graph, the earlier candidate graph, its idempotent rerun,
and the Neo4j oracle all produced identical sorted full-graph streams. The
schema-only clean graph reported 72,281 nodes and 75,869 relationships through
both aggregate counts and complete row enumeration, and matched the oracle
bidirectionally and by hash. With labels sorted and property maps key-sorted,
the streams produced:

- node snapshot SHA-256
  `42f100106bf80f27d26c15f9dbf8634536d3ce238f8c5743e26538b447042e56`;
- relationship snapshot SHA-256
  `20de62c930ee5c06576a0b04a2972321e870714f5427a2091439d4b6b557d660`.

The final fresh-versus-prior candidate diff was nodes `0/0`, relationships
`0/0`; the final fresh-versus-idempotent diff was also `0/0` for both surfaces.
The earlier forced-timeout recovery graph converged exactly to that prior
candidate. Full normalized graph equality also preserves de-duplication,
self-exclusion, and prefix-collision outcomes because the candidate changes
only execution boundaries; it does not change `internal/projector`
materialization construction or canonical Cypher construction.

The shipped NornicDB transaction remained invisible and returned zero nodes and
relationships after its 600-second client timeout. It continued consuming one
CPU until its server-side rollback finished, then returned to idle with the
graph still empty. The candidate's forced directories-phase timeout returned in
10.8 ms, after the repository phase had committed. NornicDB subsequently
completed that small directories transaction, leaving 1 repository and 522
directories but no relationships. The manually resumed run converged exactly,
with the scope and generation active, projector status succeeded, no
dead-letter row, and zero repair-queue rows. This is explicit partial-phase
replay and graph-idempotence behavior, not an atomic-cancellation or automatic
queue-retry claim. Automatic classification is covered by
`TestProjectorCanonicalWriterDrainTimeoutRemainsQueueRetryable` and
`TestProjectorQueueFailLifecycleRetriesGraphWriteTimeoutWithinAttemptBudget`.

## Concurrency contract

- Projector workers remained 2.
- The process-wide canonical graph permit pool remained 2.
- Entity-phase fanout remained enabled at its production default; the inner
  graph gate bounds actual transactions without removing dispatcher concurrency.
- The outer executor does not expose `GroupExecutor`, preventing the canonical
  writer from collapsing all phases back into one transaction.
- Queue heartbeat, lease, ordering, and activation paths are unchanged. The
  drain deadline now uses the queue's existing retryable graph-timeout contract
  instead of bypassing it with a plain error.
- A focused wiring proof configured entity fanout 7 and a three-permit graph
  gate. The raw grouped executor observed maximum concurrency exactly 3 and
  greater than 1. A simultaneous drain call could not enter while those three
  permits were occupied, proving grouped and drain writes share the same pool.
- An exact chunk-boundary regression proves the streaming dispatcher drains one
  entity label before admitting the next label without serializing chunks
  inside a label. Both contention tests pass ten repetitions and `-race`.
- A timed-out server transaction can finish after the client deadline. The
  exact-key lock in orneryd/NornicDB#261 serializes overlapping same-UID retries;
  disjoint UID work remains concurrent. Eshu must not roll this candidate out
  without that backend patch pinned.

## Observability Evidence:

No new instruments are required. Existing structured projector telemetry
reported `load_facts`, `build_projection`, `canonical_write`, `content_write`,
`intent_enqueue`, and `project_generation` durations and cardinalities. The
shared phase executor logs phase, statement count, duration, concurrency mode,
chunk ordinal/range, and a sanitized first-statement summary on failure. Queue
status exposed attempt, retry, success, activation, dead-letter, and repair
residue. Backend CPU plus live read queries distinguished a client timeout from
server-side rollback or late partial commit.

## Verification status

Passed on the rebased candidate before this evidence note was committed:

- focused concurrency and drain-timeout regressions, ten repetitions;
- the same focused regressions with `-race`;
- the full 23-package graph-write `go test -race` surface;
- a fresh real-NornicDB replay tier, including delta retraction and
  idempotence (`84.35s` wall, retained healthy backend);
- CI registry tests and drift, whole-module selector tests, package-doc tests
  and live verification, changed-file file cap, performance-evidence tests and
  live verification, strict MkDocs, `git diff --check`, and private-data/
  attribution scans;
- the immutable retained indexed-scope projection and full normalized oracle
  differential;
- `make pre-pr`, including whole-module formatting, lint, build, vet, selected
  exactness and telemetry gates, and race lanes (`11m52s` wall);
- fresh B-7 on stock NornicDB v1.1.11: `417/0/0` (pass/fail/warn),
  `37s` pipeline, `179.49s` command wall;
- fresh B-7 on v1.1.11 plus orneryd/NornicDB#261: `417/0/0`
  (pass/fail/warn), `37s` pipeline, `179.94s` command wall.
- fresh B-7 on the exact-source Compose override for commit
  `1492458852588c884c32f70d27ea2ee07086769c`: `416/0/1`, `81s` pipeline;
  the only advisory was the intentionally extended collector settle.

Both fresh B-7 controls retained two projector workers and completed all three
drain phases with `3 pass / 0 fail / 0 warn`; both backend pairs remain healthy
with zero restarts. A prior B-7 run against a reused stock `--keep` volume was
red at `406/9/2`, but the harness does not clear retained volumes. The two
fresh controls therefore supersede that result for causal interpretation: B-7
shows no stock-versus-patched difference on this small corpus. The deterministic
same-UID failing-before/passing-after backend proof and the retained full-corpus
failure, not the reused B-7 run, establish the exact-lock rollout dependency.

Still required on the final diff: the final `eshu-code-review` hostile and
cross-pass review.

## Recommendation and limits

Conditional go. The implementation is locally proven and worth retaining:
output exactness is `0/0`, idempotent replay and partial-phase recovery converge
exactly, and configured concurrency is preserved. Replacing one 120-second
failure saves about `96.471s`, which does not cover the accepted `158s` gap.
Replacing the observed three-attempt terminal failure saves about `334.726s`,
which covers that gap by about `176.726s`; this is the supported contribution
claim.

Production readiness remains blocked on orneryd/NornicDB#261 being merged,
tagged, pinned by immutable Eshu artifact, and the candidate being rerun on that
artifact. The fresh B-7 controls do not prove the absolute under-30 full-corpus
target or make a source-built image equivalent to a future production tag. The
exact-source override does let local and remote validation continue on one
auditable upstream revision without reducing concurrency. After final review,
the branch may be published for dependency-aware review and the reference
remote full-corpus run, but it must not be marked merge-ready while Eshu still
pins stock v1.1.11 by default.

Refs #5122, #4207, orneryd/NornicDB#261.
