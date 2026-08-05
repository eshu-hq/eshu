# #5827 provenance-edge identity and legacy repair evidence

Issue #5827 makes independent provenance assertions coexist when they use the
same relationship type and endpoints. The identity contract is now:

```text
(start node, end node, relationship type, scope_id, evidence_source)
```

This applies to `PUBLISHES` edges targeting `Package` and `PackageVersion`,
`BUILT_FROM`, and `DERIVED_FROM`.

## Root cause

Eshu's writers included `scope_id` and `evidence_source` in the relationship
property map, but the previous NornicDB default ignored relationship pattern
properties while evaluating `MERGE`. Two same-pair assertions therefore
collapsed into one edge. The later writer overwrote mutable properties, and a
retract by either owner could delete the shared row.

Splitting batches or serializing writers could not fix the behavior: the second
statement still matched the first relationship. Neo4j's relationship `MERGE`
contract includes the pattern properties in the match, so the backend behavior
was a compatibility defect.

## Backend prerequisite

The correction is orneryd/NornicDB#290 at exact source revision
`5d2731ae1b3328708f74f12c21658786abac641a`. Eshu's default Compose tag is
`eshu-nornicdb-pr290:5d2731ae1b33`, built from that full revision and labeled
with it. Until the commit is reachable from upstream `main`, the Git build
context selects `pull/290/head` and verifies that ref against the full revision
checksum.

The backend change covers plain `MERGE`, generic and specialized `UNWIND`,
explicit transactions, deterministic concurrent create, numeric-width
equivalence, signed zero, and non-reflexive NaN semantics. Committed-write cache
invalidation occurs only after a successful authoritative transaction write.

### Backend performance proof

Same-host `GOMAXPROCS=1` benchmarks used the same source, corpus shape, storage
state, `-benchtime=20x`, and `-count=3`. Values below are the median nanoseconds
per operation.

| Path and fanout | Previous main | Corrected revision |
| --- | ---: | ---: |
| plain, 2 | 108,323 | 54,844 |
| plain, 32 | 150,975 | 50,492 |
| plain, 256 | 575,935 | 64,494 |
| UNWIND, 2 | 211,681 | 42,975 |
| UNWIND, 32 | 4,741,627 | 303,417 |
| UNWIND, 256 | 188,007,740 | 2,568,088 |

The corrected point lookup stays bounded as pair fanout grows. The compatibility
scan is reserved for legacy or collision recovery and is bounded to the endpoint
pair.

## Eshu writer proof

`TestProvenanceEdgeWriterLiveLegacyRowSetMigration` creates the exact legacy
state first: one endpoint-only relationship. It then replays two independent
assertions in both orders for all four writer shapes:

- `PUBLISHES` to `Package`
- `PUBLISHES` to `PackageVersion`
- `BUILT_FROM`
- `DERIVED_FROM`

Every case starts at one relationship and finishes at exactly two, preserving
both `scope_id` and `evidence_source` values.

`TestProvenanceEdgeWriterLiveSamePairAssertionIsolation` repeats each shape five
times, replays duplicates, runs eight concurrent writers, and retracts one
assertion. It requires exactly two relationships before retract and exactly the
other owner's relationship afterward. This proves idempotency, convergence,
and retract isolation without reducing worker count or serializing the write
path.

The live tests ran through Eshu's Bolt driver against an isolated fresh
NornicDB container built from the exact revision above:

```text
go test ./internal/storage/cypher \
  -run 'TestProvenanceEdgeWriterLive(LegacyRowSetMigration|SamePairAssertionIsolation)' \
  -count=1 -v
PASS
```

## Legacy-row repair

Migration 096 records a one-time marker named
`provenance_edge_identity_upgrade_096` and reopens current successful work for
the affected `package_source_correlation` and `container_image_identity`
domains. Claimed or running work is marked `cross_scope_replay_required` so the
normal worker lifecycle replays it safely rather than racing an in-flight
generation.

The repair deliberately uses the existing retract-before-write path. It does
not mutate graph rows in SQL, invent graph endpoints, or bypass reducer truth.
The replay removes each legacy owner-scoped row and rematerializes it with the
new identity.

The live Postgres proof applies the real migration to current succeeded,
claimed, running, stale-generation, and unrelated-domain fixtures. It verifies
that only current affected work is reopened or dirtied, then reapplies the
migration and proves the marker makes it idempotent:

```text
go test ./internal/storage/postgres \
  -run TestProvenanceEdgeIdentityUpgradeSeedsCurrentReplayOnceLive \
  -count=1 -v
PASS
```

## Golden truth

The B-12 snapshot requires at least two `PUBLISHES` relationships. rc-164
filters `PACKAGE_PUBLICATION_CORRELATION`, while rc-172 filters
`PACKAGE_OWNERSHIP_CORRELATION` for the same Repository and PackageVersion
endpoints. One surviving edge cannot satisfy both assertions.

`BUILT_FROM` remains narrowed by its canonical OCI evidence kind and
`source_tool=oci`. Package assertions use the canonical explicit
`source_tool=unknown` fallback because their decisions do not carry a truthful
ecosystem token.

The exact-source full-corpus gate materialized 23 `BUILT_FROM` assertions after
scope identity stopped collapsing them. The snapshot ceiling is 40: bounded
above the observed result for fixture growth while still catching an accidental
fanout explosion.

```text
scripts/verify-golden-corpus-gate.sh
527 pass, 0 required-fail, 0 advisory-warn
elapsed 129s; budget ceiling 1800s
```

## Rollout and rollback

Roll out the exact NornicDB #290 source or a released successor containing the
fix before deploying the Eshu writer and migration. The old backend would still
collapse the new identity properties.

Rollback must preserve that order in reverse: stop the Eshu reducer or return to
the old writer before rolling the backend below the fixed revision. Migration
096 is idempotent and leaves its marker in place; a forward deployment can
explicitly replay affected current work again if graph data was restored from
an older backup.

## Observability impact

No new metric is introduced in #5827. Existing provenance-edge counters still
measure submitted materialization rows; issue #5828 separately corrects those
counters to distinguish attempted from durably materialized outcomes. Migration
state and replay-required markers remain visible through the existing reducer
work/status surfaces.
