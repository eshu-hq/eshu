# Issue 5540 Flux target-attribution evidence

Theory baseline: commit `85e031612`. Measurements used isolated, empty
containers sequentially on macOS 26.5.2, Docker 29.4.0, 12 logical CPUs and
64 GiB host RAM. Containers had no explicit CPU or memory limits. Each case
used 20 warmups and 250 measured iterations over the same seeded graph.

Backends:

- NornicDB v1.1.11, image digest
  `sha256:36e0eb79ddf8`, source revision
  `1492458852588c884c32f70d27ea2ee07086769c`.
- Neo4j 2026.05.0, image digest `sha256:6c162e2432f8`.

The representative graph had one canonical row, 20 repository rows and 20
binding artifacts. The saturated graph had one canonical row, 60 repository
rows and 180 binding artifacts: each source repository had two namespaces
sharing one GitRepository name plus a duplicate. That gives 120 qualified
`(source,target,namespace,name)` identities but only 60 name-only identities,
proving that name-only projection loses 60 valid identities.

| Backend | Dataset | Old two-query p50/p95 | Reviewed predecessor three-query p50/p95 | p95 cost |
| --- | --- | --- | --- | --- |
| NornicDB | representative | 0.302/0.423 ms | 0.466/0.635 ms | +0.212 ms |
| NornicDB | saturated | 0.369/0.505 ms | 0.616/0.819 ms | +0.314 ms |
| Neo4j | representative | 1.646/3.697 ms | 2.762/5.135 ms | +1.438 ms |
| Neo4j | saturated | 1.029/1.491 ms | 1.881/2.647 ms | +1.156 ms |

These timings proved the cost envelope of adding a binding read, but predate the
final split-query saturation fix and are not presented as a measurement of that
final shape. The final implementation receives only the access-filtered,
sentinel-capped source repository IDs already returned by the repository
deployment-source query. Its first query returns at most 51 qualified
source-repository-to-EvidenceArtifact rows. Reaching row 51 returns saturation
without target expansion, drops the sentinel candidate from attribution, and
skips all Flux target binding. Below the sentinel, a second bounded query
expands only those artifact IDs to the requested target. The honest remaining
first-hop work is the outgoing deployment-evidence artifact degree of the
access-filtered source repository page, not merely the count of repositories.

## Gated live backend proof

The opt-in `TestLiveFluxDeploymentSourceTargetBindings` seeds six namespace,
duplicate, and ambiguity artifacts, then a separate saturated set of 51
first-hop artifacts where 50 match target A and zero match target B. It executes
the production query builder and asserts explicit/defaulted namespace behavior,
duplicate collapse, multi-target ambiguity, and saturation with zero partial
attribution for both 50-match and zero-match target expansion cases.

Run only after the external graph-backend preflight clears. The NornicDB command
uses the repository Compose pin built from exact revision
`1492458852588c884c32f70d27ea2ee07086769c` (v1.1.11 base):

```bash
COMPOSE_PROJECT_NAME=eshu5540nornic NEO4J_HTTP_PORT=27474 NEO4J_BOLT_PORT=27687 \
  docker compose -f docker-compose.yaml up --wait -d --build nornicdb
cd go && ESHU_5540_FLUX_BINDINGS_LIVE=1 ESHU_5540_BACKEND=nornicdb \
  ESHU_NEO4J_URI=bolt://localhost:27687 ESHU_NEO4J_DATABASE=nornic \
  GOCACHE=/tmp/eshu-5540-live-nornic go test ./internal/query \
  -run '^TestLiveFluxDeploymentSourceTargetBindings$' -count=1 -v
COMPOSE_PROJECT_NAME=eshu5540nornic NEO4J_HTTP_PORT=27474 NEO4J_BOLT_PORT=27687 \
  docker compose -f docker-compose.yaml down -v
```

The Neo4j command uses the repository-pinned `neo4j:2026-community` service:

```bash
COMPOSE_PROJECT_NAME=eshu5540neo4j NEO4J_HTTP_PORT=28474 NEO4J_BOLT_PORT=28687 \
  ESHU_NEO4J_PASSWORD=change-me docker compose -f docker-compose.neo4j.yml \
  up --wait -d neo4j
cd go && ESHU_5540_FLUX_BINDINGS_LIVE=1 ESHU_5540_BACKEND=neo4j \
  ESHU_NEO4J_URI=bolt://localhost:28687 ESHU_NEO4J_DATABASE=neo4j \
  ESHU_NEO4J_USERNAME=neo4j ESHU_NEO4J_PASSWORD=change-me \
  GOCACHE=/tmp/eshu-5540-live-neo4j go test ./internal/query \
  -run '^TestLiveFluxDeploymentSourceTargetBindings$' -count=1 -v
COMPOSE_PROJECT_NAME=eshu5540neo4j NEO4J_HTTP_PORT=28474 NEO4J_BOLT_PORT=28687 \
  ESHU_NEO4J_PASSWORD=change-me docker compose -f docker-compose.neo4j.yml down -v
```

These live commands are intentionally pending; no backend was launched while
the external preflight owned that shared resource.

No-Regression Evidence: qualified namespace/name identity preserves distinct
GitRepository resources in one file and target, exact matching rejects missing
or mismatched namespace, duplicate identical bindings deduplicate, multiple
targets remain ambiguous, and both backend-compatible query shape and sentinel
placement are covered by focused tests.

No-Observability-Change: existing `neo4j.query` spans retain database system,
database name, statement and error signals; existing deployment-trace limits
retain observed-count, lower-bound and truncation signals; controller binding
outcomes remain surfaced as linked, missing, ambiguous, saturated and
unsupported tallies. No metric, span or log contract changes.
