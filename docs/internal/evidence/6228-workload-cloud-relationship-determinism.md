# #6228 workload_cloud_relationship: live determinism drive (No-Regression)

Date: 2026-09-27. Branch: fix/6228-workload-cloud-relationship. Refs #6228
(family slice; does not close the issue).

## Claim

Wiring the workload_cloud_relationship family
(WriteWorkloadCloudRelationshipEdges -> USES) into the live determinism
matrix (registry row 16, shared N={1,2,4} cell + post-delta exact-set assert)
changes no production behavior. The Odù, guard, extractor, writer, handler,
and families.go registration all landed in #6523 and are byte-untouched
here. New production-surface Go is one narrow fixture-setup subcommand
(`eshu-ifa materialize-workload-endpoints`, following the
materialize-platform-prerequisite precedent) plus one exported derivation
wrapper; everything else is fixture + live wiring.

## Why the seed exists

The fixture Odù carries aws_resource facts only, so the determinism cell has
no workload-domain facts for the workload pipeline to turn into nodes -- yet
the writer MATCHes
`(workload:Workload)<-[:INSTANCE_OF]-(instance:WorkloadInstance)` and the
handler fails `instances_not_ready` without them (observed live 2026-09-27:
1 anchor, orders-api/prod, handler deferring with max_wait 30m, drain
residual 1). The seed creates exactly the Odù's own positive anchor
(workload:orders-api in prod; instance id derived by the single source of
truth the guard mapper shares) and nothing else. The service-only,
ambiguous, and environment-less anchors get no nodes, so the extractor's
drop-never-invent restraint still has something to prove live. Owner
sanctioned creating the endpoints 2026-09-27.

## Live proof

`scripts/verify-ifa-determinism.sh --keep` (NORNICDB_IMAGE bare tag, same as
the iam_can_assume / iam_can_perform drives): PASS, GATE_EXIT=0
(/tmp/workload-live3.log). Digests identical across N=1, N=2, and N=4:
b1d1542cc0568277733c5b1e1e6d6734cc69c5a61b5caeda87c20a24f674e9e8
(walls 206s/206s/200s). Per-cell workload_cloud_relationship
asserts 2/2 edges exact pre- and post-delta (all five string props:
resolution_mode, environment, relationship_basis, service_anchor_source,
service_anchor_reason) — 6/6 asserts total, 0 failures; every drain shows
residual=0 with dead_letter=0. The seed reports
`instance_id=workload-instance:orders-api:prod verified=1` in all three
cells; the 5-fact ingestion commits with 0 duplicates (stable keys are
account:region:type:id, pinned unique by the new test).

Unlike iam_can_perform there is no list-prop caveat on the asserted set: the
expected fixture names only scalar props, so the live strings-only
assert-edges covers the full named property set. (The writer also SETs
evidence_fact_ids as a list; it is pinned guard-side by the extractor test,
not live -- same split as perform's merged-actions table.)

## Static re-proof (at head)

- `go test ./internal/ifa/materializededges/ -run TestDirectFamilyCassettes`: ok
  (cassette <-> compiled Odu lockstep, 5 facts, own scope
  aws:eshu-fixture-workload-relationship-account).
- Uniqueness test
  `TestWorkloadCloudRelationshipFamilyOduStableKeysAreUnique`: PASS, and
  mutation-proven -- narrowing the key derivation to account:region REDs it,
  restored GREEN.
- Seed subcommand `go test ./cmd/ifa/ -run 'MaterializeWorkloadEndpoints'`:
  4/4 PASS (exact nodes+params, verify-miss fails, invalid-input before
  backend, redacted backend errors) plus dispatch routing; RED first (test
  failed to compile), GREEN after.
- Guard `TestGuardedDirectFamiliesResolveTheirOduCovered/workload_cloud_relationship`:
  PASS -- extractor reproduces the hand-derived 2-edge set exactly
  (pre-existing from #6523, untouched).
- `scripts/test-verify-ifa-determinism.sh`: pass (19 families pins both
  directions, 19 fixture wirings, private-data scan 74 files, 0 findings;
  includes the new seed-call needles + seed-vs-cassette drift check).
- `scripts/test-verify-ifa-fault-injection.sh`: pass (neutrality: family
  never dispatches to the fault gate; 49 cells exact cover unchanged).
- `scripts/test-generate-ci-gates-doc.sh`: 16/16 after regen (259 -> 262
  paths).
- Matcher RED->GREEN: pre-trigger registry/workflow/specs edits, the new Odu
  path selected neither live gate (mirror failed naming the file); post-edit
  selects ifa-determinism only.

No-Regression Evidence: no production query-text, extractor, handler,
trigger-matcher, or dispatch change. The only new graph-write statement is
the once-per-cell endpoint seed (see Seed statement below), which touches 3
nodes/edges total and never runs in any deployed path. Baseline: pre-change
origin/main matrix (sibling families' digests). After: this branch's matrix
run -- digests identical across N=1/2/4 for every pre-existing family;
workload_cloud_relationship asserts 2/2 edges exact pre- and post-delta in
all three cells. Backend: NornicDB fix-500-e022384c + Postgres (local
determinism stack) and Neo4j (differential cell in CI). Input shape: 5-fact
Odù (2 edge-producing anchors + 3 deliberate non-producers), 2-edge expected
set; terminal row counts: 6/6 workload asserts green, 0 failures, empty
dead-letter. The fault-injection half is explicitly out of scope (no fault
cells; row cell_kind=custom rejected by generic dispatchers).

Seed statement (exact shape, for the content-based performance-evidence
gate): `MERGE (w:Workload {id: $workload_id})`,
`MERGE (i:WorkloadInstance {id: $instance_id})`,
`SET i.environment = $environment`,
`MERGE (i)-[:INSTANCE_OF]->(w)` -- sequential single-row MERGEs on the true
identity keys the writer MATCHes (no cartesian products, no unanchored
patterns, no variable-length traversals); input cardinality 1 row, output at
most 2 nodes + 1 edge; same MERGE forms as the production canonical node
writers; no new index or constraint (3 nodes in a fresh cell). Backend:
NornicDB fix-500-e022384c (Bolt) locally, Neo4j in the CI differential cell.
No benchmark: one-shot per-cell fixture setup, outside every deployed and
hot path -- explicitly trading the full bench for this no-measurable-regression
statement (3 idempotent MERGEs cannot move any path measurement).

No-Observability-Change: no new metrics, spans, logs, or status surfaces.
The drive seed reuses the `eshu-ifa` subcommand shape with the family's
domain label; the subcommand prints one `instance_id=... verified=1` line
the drive checks exactly. Telemetry evidence: determinism cell logs show the
standard drive/assert sections with the workload-cloud-relationship domain
label plus the seed's verified line; no new log lines or status keys.

## Golden-corpus disposition (B-7/B-12)

No testdata/golden change. The new
testdata/cassettes/workloadcloudrelationship/ fixture is an Ifa
family-matrix input, not a live-collector cassette the B-7 gate replays;
binding proof is the cassette<->Odu lockstep test, the vacuity guard, and
the CI determinism-matrix + corpus-gate jobs (green at head).
