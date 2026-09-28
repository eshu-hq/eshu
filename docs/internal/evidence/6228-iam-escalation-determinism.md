# #6228 iam_escalation: live determinism drive (No-Regression)

Date: 2026-09-27. Branch: fix/6228-iam-escalation. Refs #6228
(family slice; does not close the issue).

## Claim

Wiring the iam_escalation family (WriteIAMEscalationEdges ->
CAN_ESCALATE_TO) into the live determinism matrix (registry row 17, shared
N={1,2,4} cell + post-delta exact-set assert) changes no production
behavior. The extractor, writer, handler, and families.go registration shape
are byte-untouched except one rename-only export of the writer's two Cypher
consts (statement text identical). New production-surface Go is the
projector intent builder
(`projector/cloud/aws/iam/escalation`, following the sibling perform/trust
precedent) plus its root dispatch line; everything else is fixture + live
wiring.

## Why the projector builder exists

The first live run (N=1) proved the gap: the reducer handler, extractor,
writer, guard, cassette, and registry row were all green, yet zero
`iam_escalation_materialization` intents were ever fanned out — the
projector had per-family intent builders for instance, trust, and perform,
but none for escalation, so the scope generation's identity statements
projected nothing. The builder (decodable inline/attached_managed identity
statements, either effect, earliest-anchor, shared
`aws_resource_materialization:<scope>` entity key) closes exactly that gap;
no other behavior changes.

## Live proof

`scripts/verify-ifa-determinism.sh --keep` (NORNICDB_IMAGE bare tag, same as
the family 1-3 drives; digest verified equal to the pinned
`sha256:74a8ed...`): PASS, GATE_EXIT=0 (/tmp/esc-det.log). Digests
identical across N=1, N=2, and N=4:
1c04f8d81a92b00d2b5f881c1e6135429c69a07b5b022f0b9b4cbab9f7cf3d91
(walls 208s/204s/206s). Per-cell iam_escalation asserts 5/5 edges exact
pre- and post-delta — 6/6 asserts total, 0 failures; every drain shows
residual=0 with dead_letter=0. The 22-fact ingestion commits with 0
duplicates (stable keys pinned unique by the new test).

Like iam_can_perform there is a list-prop caveat on the asserted set: the
writer stores the merged primitive set as the SET LIST property
`rel.primitives`, which the live strings-only assert-edges cannot
round-trip, so the fixture carries no properties and the merged sets are
pinned guard-side in `checkIAMEscalationMergedPrimitives` (five pairs,
three tokens on the exec-role edge, one elsewhere) -- same split as
perform's merged-actions table. The live assert therefore proves endpoint
resolution and restraint; the guard proves the merge.

## Static re-proof (at head)

- `go test ./internal/ifa/materializededges/ -run TestDirectFamilyCassettes`: ok
  (cassette <-> compiled Odu lockstep, 22 facts, own scope
  aws:eshu-fixture-iam-escalation-account).
- Uniqueness test
  `TestIAMEscalationFamilyOduStableKeysAreUnique`: PASS (22 facts, 0 dup
  keys); RED before the Odu existed, and GREEN only after the stable-key
  derivation carried the full collector identity (a duplicate copy of the
  wrong-target fixture was caught and removed during development).
- Guard `resolveIAMEscalationMaterializedEdges` through the production
  dispatcher: GREEN on the full Odu; RED-proven by dropping one
  edge-producing fact (MISSING team-policy edge).
- Builder tests `TestBuildIAMEscalationMaterializationReducerIntent`: PASS
  (RED before the builder existed); parity `fanOutParityExpectations` +1
  domain (44), probe count 45->46, all pinned.
- Roster/waiver tests `TestGuardedDirectFamilies*`: PASS.
- `go vet` on the changed packages: clean.
- `scripts/test-verify-ifa-determinism.sh`: pass (20 families, pins
  totality both directions).
- `scripts/test-verify-ifa-fault-injection.sh`: pass (neutrality: no fault
  cells).
- `scripts/verify-payload-usage-manifest.sh`: EXIT=0 (new decode seam
  discovered).
- `scripts/verify-package-docs.sh`: EXIT=0.

## No-Observability-Change

No new series, spans, or log schemas: the builder emits a standard reducer
intent through the existing fan-out path, and the handler/writer telemetry
is untouched. Telemetry coverage unchanged by construction; the live run
exercised the standard reducer/projector signals only.

No-Regression Evidence: no production query-text, extractor, handler,
trigger-matcher, or dispatch change. The writer const rename
(canonicalIAMEscalationEdgeUpsertCypher / RetractIAMEscalationEdgesCypher)
is identifier-only: the MERGE/MATCH statement text is byte-identical, so
every executed statement is one the pre-change gate already proved. The
only new graph-write statement is none: this family needs no seed (the
fixture's collector facts produce committed CloudResource nodes through
the canonical pipeline, unlike workload_cloud_relationship). The projector
builder adds one probe over the shared immutable fact index (probe count
45->46, parity domains 43->44, both pinned). Baseline: pre-change
origin/main matrix (sibling families' digests). After: this branch's
matrix run -- digests identical across N=1/2/4 for every pre-existing
family; iam_escalation asserts 5/5 edges exact pre- and post-delta in all
three cells. Backend: NornicDB fix-500-e022384c (digest verified equal to
the pinned sha256:74a8ed...) + Postgres (local determinism stack); Neo4j
in the CI differential cell. Input shape: 22-fact Odu (7 nodes + 15
statements: 6 edge-producing Allows converging on 5 edges, 9 deliberate
non-producers), 5-edge expected set; terminal row counts: 6/6 escalation
asserts green, 0 failures, empty dead-letter. The fault-injection half is
explicitly out of scope (no fault cells; row cell_kind=custom rejected by
generic dispatchers).

No-Observability-Change: no new metrics, spans, logs, or status surfaces.
The builder emits a standard reducer intent through the existing fan-out
path with the family's domain label; the handler/writer telemetry is
untouched. Telemetry evidence: determinism cell logs show the standard
drive/assert sections with the iam_escalation domain label; no new log
lines or status keys.
