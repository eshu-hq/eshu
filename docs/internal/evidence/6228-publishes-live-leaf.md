# Publishes Live Leaf Evidence (#6228)

The publishes family ODU carries 13 facts (4 repositories + 6 source hints
+ 2 versions + 1 package) in the single fixture package-registry scope.
The expected live set is the exact 3-edge PUBLISHES set (ownership of the
Package node, ownership of ver-team-api-1.0.0, publication of
ver-team-api-1.0.0 — the same endpoint pair under both evidence sources is
two edges, not one).

Two defects kept the set at graph=0, both proven live and fixed on this
branch:

1. Empty-path UNIQUE collision (nornicdb): every fixture repository
   derived path `""`, so the losers of the `UNIQUE(Repository.path)`
   race never materialized (orders node absent pre-fix, built_from 0/2).
   Fix: fixture-unique `local_path` on all four repository facts in the
   ODU and the committed cassette (the projector falls back to
   `local_path` for `path`). Regression test:
   `TestIfaFamilyRepositoryPathsAreDistinct`.
2. Hash-luck scope-repository election: the fact loader orders by
   `(observed_at, fact_id)` with a scope-stamped `observed_at` tie, and
   the singular `mat.Repository` row crowned the first decodable fact —
   shared-b by fact_id luck — so team-api's node (and all three edges
   hanging off it) never existed. Fix: two-pass election in
   `extractRepositoryWithQuarantine` preferring the first decodable
   active fact whose canonical remote no other active fact shares (the
   same active-only ambiguity rule the package-source correlation
   applies to hints), falling back to first decodable. Census: the only
   other multi-repo fixture scopes (deployable-unit, replaydelta
   tombstone) elect identically under both rules.
   Regression tests: `TestElectRepositorySkipsTombstonedAndAmbiguousRemotes`
   plus both fallback tests.

The version-scoped hint names ver-team-api-1.0.0 (a published version
fact): the edge writer MATCHes the PackageVersion node and never
fabricates an endpoint, so a ghost version silently materializes
nothing. The package-level and version-scoped hints pair with 1.0.0 on
the publication path through separate decisions that project onto one
edge identity, so both row builders dedupe by edge identity (one row per
MERGE identity, matching what the live gate counts).

No-Regression Evidence (#6228): `go test` green on
`internal/projector/canonical`, `internal/reducer/packages/correlation`,
and `internal/ifa/materializededges` (offline guard, cassette drift
lockstep, coverage lockstep); `bash scripts/verify-ifa-determinism.sh`
PASS with the publishes 3-edge assert green in all six cell asserts
(pre/post-delta × N=1/2/4) and the matrix digest green. Telemetry
evidence is the fault-mirror exact-cover proof
(`test-verify-ifa-fault-injection.sh`: 100 cells, 8 shards) and the
shard-balance check; CI waves are the remaining required proof before
merge.

No-Observability-Change: the change adds no metric, span, log line,
queue, lease, worker, route, or graph query. The `submitted` row counter
emits one fewer publication row on scopes pairing both hint kinds with
one version (two decisions coalesce to one write intent).
