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

| Backend | Dataset | Old two-query p50/p95 | New three-query p50/p95 | p95 cost |
| --- | --- | --- | --- | --- |
| NornicDB | representative | 0.302/0.423 ms | 0.466/0.635 ms | +0.212 ms |
| NornicDB | saturated | 0.369/0.505 ms | 0.616/0.819 ms | +0.314 ms |
| Neo4j | representative | 1.646/3.697 ms | 2.762/5.135 ms | +1.438 ms |
| Neo4j | saturated | 1.029/1.491 ms | 1.881/2.647 ms | +1.156 ms |

The implemented binding read receives only the access-filtered, sentinel-capped
source repository IDs already returned by the repository deployment-source
query. Its sentinel is applied after the source repository to evidence-artifact
first hop and before target expansion. The honest remaining work is therefore
the outgoing deployment-evidence degree of at most 51 authorized source
repositories; reaching 51 causes all Flux target attribution to skip rather
than select from a partial set.

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
