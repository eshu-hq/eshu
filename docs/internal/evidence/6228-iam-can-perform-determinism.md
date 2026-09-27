# #6228 iam_can_perform: live determinism drive (No-Regression)

Date: 2026-09-27. Branch: fix/6228-iam-can-perform. Refs #6228 (family slice;
does not close the issue).

## Claim

Wiring the iam_can_perform family (WriteIAMCanPerformEdges -> CAN_PERFORM)
into the live determinism matrix (registry row 15, shared N={1,2,4} cell +
post-delta exact-set assert) changes no production behavior: the extractor,
writer, and trigger matcher are untouched in what they resolve, write, and
select. The only production-code touches are additive registrations (guard,
families.go entry, exported Split wrapper + consts) that no existing path
calls.

## Live proof

`scripts/verify-ifa-determinism.sh --keep` (NORNICDB_IMAGE bare tag, same as
the iam_can_assume drive): PASS N=1/2/4, digests identical across N, 0
failures. Per-cell iam_can_perform asserts: 3/3 edges exact pre- and
post-delta (endpoint pairs + identity_policy_only scope), 6/6 asserts total
across the matrix. Pre-existing families' cells unchanged (their asserts and
digests identical to the pre-change runs).

Review P1 found the first live run drove 17 facts, not 20: the permission
stable key omitted resources/not-actions, collapsing four statements to one
fact_id at ingest. Fixed by widening the key to the collector identity
inputs (+ uniqueness test); the re-run above is on the widened keys, and its
ingestion log shows 60 perform facts committed = 20 x 3 cells. All eleven
non-producers reach the extractor live in this run.

## Static re-proof (at head)

- `go test ./internal/ifa/materializededges/ -run TestDirectFamilyCassettes`: ok
  (cassette <-> compiled Odu lockstep, 20 facts).
- Guard `TestGuardedDirectFamiliesResolveTheirOduCovered/iam_can_perform`:
  PASS — extractor reproduces the hand-derived 3-edge set exactly (pairs +
  scope), merged action sets pinned per pair in the guard-side table
  (sensitivity proven by deliberate mutation: single-action pin REDs the
  guard, restored GREEN).
- `scripts/test-verify-ifa-determinism.sh`: pass (18 families pins both
  directions, 18 fixture wirings, private-data scan 71 files, 0 findings).
- `scripts/test-verify-ifa-fault-injection.sh`: pass (neutrality: family
  never dispatches to the fault gate).
- `scripts/test-generate-ci-gates-doc.sh`: 16/16 after regen (RED 15/16
  before regen).
- Matcher RED->GREEN: pre-edit registry selects neither live gate for the 3
  new input paths; post-edit selects ifa-determinism only.

No-Regression Evidence: content-hot files touched
(iam_can_perform_edge_writer.go: 2 const renames only, no query-text change;
iam_can_perform_materialization.go: +8-line exported Split wrapper delegating
to the untouched split) keep identical runtime semantics. Baseline:
pre-change origin/main matrix (sibling families' digests). After: this
branch's matrix run — digests identical across N=1/2/4 for every pre-existing
family; iam_can_perform asserts 3/3 edges exact pre- and post-delta in all
three cells. Backend: NornicDB fix-500-e022384c + Postgres (local
determinism stack) and Neo4j (differential cell in CI). Input shape: 20-fact
Odù (6 resources, 14 statements), 3-edge expected set; terminal row counts:
6/6 perform asserts green, 0 failures, empty dead-letter. Writer unit tests
green; extractor output byte-identical (guard reproduces the hand-derived
set); trigger matcher selects a strict superset only on the 3 new input
paths. The fault-injection half is explicitly out of scope (no fault cells;
row cell_kind=custom rejected by generic dispatchers).

No-Observability-Change: no new metrics, spans, logs, or status surfaces.
The drive/assert fns reuse the shared `_ifa_direct_family_drive` and
`assert-edges` paths with the family's domain label, the same
operator-visible shape as the three sibling families. Telemetry evidence:
determinism cell logs show the standard drive/assert sections with the
iam-can-perform domain label; no new log lines or status keys.

## Golden-corpus disposition (B-7/B-12)

No testdata/golden change. The new testdata/cassettes/iamcanperform/ fixture
is an Ifa family-matrix input, not a live-collector cassette the B-7 gate
replays; binding proof is the cassette<->Odu lockstep test, the vacuity
guard, and the CI determinism-matrix + corpus-gate jobs (green at head).
