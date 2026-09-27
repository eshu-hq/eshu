# #6228 iam_can_assume Determinism Live-Drive Evidence

Wiring the `iam_can_assume` direct-materialization family
(`WriteIAMCanAssumeEdges`) into the live determinism matrix touches one
content-hot Go file — `go/internal/ifa/familyodu/iam_can_assume_family_odu.go`
contains the family's `MERGE (principal)-[rel:CAN_ASSUME]->(role)` write Cypher
— so this note records the no-regression proof the performance-evidence gate
requires.

## What changed at runtime

- The family's Odù names its own fixture scope
  (`aws:eshu-fixture-can-assume-account`) instead of sharing
  `aws:eshu-fixture-account` with `iam_instance_profile_role`. Scopes carry
  one ACTIVE generation, so the shared scope superseded the sibling and the
  first live revision asserted zero edges. One live generation per scope.
- The write Cypher itself is unchanged: the `CAN_ASSUME` relationship type
  and both endpoint labels come from the closed single-member vocabulary the
  lockstep cassette test pins. No new MATCH/MERGE shape, no new worker,
  batch, lease, or concurrency knob.
- The shared N={1,2,4} determinism cell drives one more 13-fact cassette and
  the post-delta block asserts its exact two-edge set (6/6 asserts, one per
  N per pre/post-delta phase). No other family's pins changed.

## Proof

No-Regression Evidence: `bash scripts/verify-ifa-determinism.sh --keep`
with backend `NORNICDB_IMAGE=ghcr.io/eshu-hq/nornicdb-amd64-cpu:fix-500-e022384c`
(tag form; the digest form fails with "cannot overwrite digest") reported
PASS for N=1, N=2, and N=4 with 0 failures. All 6/6 `iamcan` exact-set
asserts passed and every previously-pinned family assert still passed, so the
added family neither diverged across worker counts nor perturbed sibling
cells. Terminal counts: 13 committed cassette facts under the new scope,
exact two-edge `CAN_ASSUME` set per run, 0 failed asserts. Baseline for
comparison is the same script on the same stack before this wiring (family
not driven, gate PASS); the after measurement adds the driven family and the
gate stays PASS. Full log: `/tmp/detlive3.log` (~2616 lines, run 2026-09-26).

Static mirrors agree: `bash scripts/test-verify-ifa-determinism.sh` pass,
`bash scripts/test-verify-ifa-fault-injection.sh` pass (neutrality — the new
row carries `cell_kind=custom` with no fault cells, so the fault gate cannot
dispatch it), `cd go && go test ./internal/ifa/... -count=1` all ok.

No-Observability-Change: no new metric, span, log, or status signal. The
family is observed through the existing live-gate asserts
(`scripts/verify-ifa-determinism.sh` post-delta block) and the existing
registry-derived pins mirror; nothing new for an operator to watch at 3 AM
beyond the gate that already pages on failure.
