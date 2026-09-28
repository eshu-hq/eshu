# #6228 ec2_uses_profile Determinism Live-Drive Evidence

Wiring the `ec2_uses_profile` direct-materialization family
(`WriteEC2UsesProfileEdges`) into the live determinism matrix touches two
content-hot Go files — `go/internal/storage/cypher/ec2_uses_profile_edge_writer.go`
carries the family's `MERGE (source)-[rel:USES_PROFILE]->(target)` write
Cypher, and `go/internal/ifa/familyodu/ec2_uses_profile_family_odu.go` builds
its fixture — so this note records the no-regression proof the
performance-evidence gate requires.

## What changed at runtime

- The family's Odù names its own fixture scope
  (`aws:eshu-fixture-ec2-uses-profile-account`) instead of sharing a scope
  with any sibling cassette. Scopes carry one ACTIVE generation, so driving
  a second generation into an occupied scope supersedes the first family's
  generation — its handler never runs and the sibling exact-set assert fails
  with zero edges (diagnosed live 2026-09-27 on the shared scope during the
  iam_can_assume drive). One live generation per scope per cell.
- The write Cypher itself is byte-identical: the two renamed constants
  (`CanonicalEC2UsesProfileEdgeUpsertCypherFormat`,
  `RetractEC2UsesProfileEdgesCypher`, exported so the materialized-edge
  family registry can hold them instead of copied text) carry the same
  template the unexported constants held. The `USES_PROFILE` relationship
  type and both endpoint labels come from the closed single-member
  vocabulary the lockstep cassette test pins. No new MATCH/MERGE shape, no
  new worker, batch, lease, or concurrency knob.
- The shared N={1,2,4} determinism cell drives one more 9-fact cassette and
  the post-delta block asserts its exact three-edge set (6/6 asserts, one
  per N per pre/post-delta phase). No other family's pins changed.

## Proof

No-Regression Evidence: `NORNICDB_IMAGE=ghcr.io/eshu-hq/nornicdb-amd64-cpu:fix-500-e022384c
bash scripts/verify-ifa-determinism.sh --keep` (tag form; the digest form
fails with "cannot overwrite digest", same workaround as the iam_can_assume
slice) drove all three cells to their graph dumps with 0 failed asserts on
2026-09-28. The gate's terminal PASS line scrolled out of the captured
`tail -30` window, so this note states what the retained artifacts prove
instead of quoting it:

- All three canonical dumps are byte-identical:
  `graph-n{1,2,4}.dump` sha256
  `8724c612f391b7027c8dbf76cceec3bf92cf8702baf22b58d3f49a981066ce3a`
  (1,600,721 bytes each) — the determinism comparison itself, measured
  directly rather than via the gate's digest line.
- Every post-delta exact-set assert exited 0 at every N, including the new
  `ifa_ec2_uses_profile_assert` three-edge assert: any nonzero assert
  aborts the gate through `die` before that cell's dump, and all three
  dumps exist. An exact 3-against-3 match cannot pass on an undriven
  family, so the handler materialized exactly the expected set at N=1, 2,
  and 4.
- Every previously-pinned sibling assert still passed in the same run, by
  the same control-flow argument — the added family neither diverged
  across worker counts nor perturbed sibling cells.
- Terminal counts: 9 committed cassette facts under the new scope (3
  `aws_resource` profile nodes + 6 `ec2_instance_posture` facts) with
  `generations_committed=1` at workers=1 (22:35), workers=2 (22:39), and
  workers=4 (22:41); exact three-edge `USES_PROFILE` set per run;
  0 failed asserts. The within-run control is the sibling set: the same
  script on the same stack without this wiring drives everything but ec2,
  and the after measurement adds the driven family while every sibling
  assert stays green. Kept workdir:
  `$TMPDIR/ifa-determinism.XXXXXX.3jmqAko2wf` (dumps, rationale deltas,
  per-cell drive logs).

Static mirrors agree: `bash scripts/test-verify-ifa-determinism.sh` pass
(21 families, pins proved both directions),
`bash scripts/test-verify-ifa-fault-injection.sh` pass (neutrality — the
new row carries `cell_kind=custom` with no fault cells, so the fault gate
cannot dispatch it), `cd go && go test ./internal/ifa/... -count=1` all
ok, `go test ./internal/storage/cypher/...` all ok.

No-Observability-Change: no new metric, span, log, or status signal. The
family is observed through the existing live-gate asserts
(`scripts/verify-ifa-determinism.sh` post-delta block) and the existing
registry-derived pins mirror; nothing new for an operator to watch at 3 AM
beyond the gate that already pages on failure.
