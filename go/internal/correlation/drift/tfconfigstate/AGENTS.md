# AGENTS — tfconfigstate

Guidance for LLM assistants editing this package.

## Read first (in order)

1. `doc.go` — package contract and DSL-routing rationale.
2. `drift_kind.go` — closed enum that labels the drift metric.
3. `classify.go` — `Classify` dispatcher and the five helper functions.
4. `candidate.go` — `BuildCandidates` cross-scope candidate constructor.
5. `attribute_allowlist.go` — per-resource-type attribute policy.
6. `../../rules/terraform_config_state_drift_rules.go` — the rule-pack
   declaration this package supports (do not edit it from here).
7. `docs/superpowers/plans/2026-05-10-tfstate-config-state-drift-design.md`
   — design contract (§5 drift kinds, §6 fixtures, §10 deferrals).

## Invariants

- `Classify` is exclusive: returns at most one `DriftKind` per call
  (`classify.go:65`). Dispatch order is load-bearing — see the doc
  comment.
- `BuildCandidates` emits address-sorted output for deterministic explain
  traces (`candidate.go:73`).
- Every emitted `Candidate` carries an `EvidenceTypeDriftAddress` atom so
  the rule pack's structural gate admits it
  (`candidate.go:13`).
- Computed/unknown config-side attributes (`ResourceRow.UnknownAttributes`)
  never raise `attribute_drift` against a concrete state value
  (`classify.go:155`). `attribute_drift` is active end-to-end as of #167:
  the HCL parser now emits flat dot-path `attributes` and `unknown_attributes`
  maps; `configRowFromParserEntry` bridges them into `ResourceRow`
  (`go/internal/storage/postgres/tfstate_drift_evidence_config_row.go:22`).
  The state side uses `flattenStateAttributes` with the same dot-path rules
  (`go/internal/storage/postgres/tfstate_drift_evidence_state_row.go:71`).
- `LineageRotation` on a prior row short-circuits the dispatcher
  (`classify.go:73`).
- High-cardinality values (addresses, attribute paths, module paths)
  belong in `Evidence` atom values, NOT in metric labels.

## Common changes scoped by file

- Add a resource type to the attribute allowlist: edit
  `attribute_allowlist.go` only. Run `attribute_allowlist_test.go` and the
  fixture corpus to confirm no regression.
- Add a drift kind: edit `drift_kind.go` (enum), `classify.go`
  (helper + dispatch), `testdata/<new_kind>/{positive,negative,ambiguous}.json`,
  and the rule pack's `RequiredEvidence` if structural shape changes.
- Change candidate evidence shape: edit `candidate.go`; the rule pack's
  `EvidenceFieldEvidenceType` selector must still match
  `EvidenceTypeDriftAddress` or admission breaks.
- Update fixtures: edit JSON under `testdata/<kind>/`. Each subdirectory
  must keep `positive.json`, `negative.json`, and `ambiguous.json`
  (asserted by `fixture_corpus_test.go`).

## Failure modes mapped to metric / log / span

- Drift classified: `eshu_dp_correlation_drift_detected_total{pack, rule, drift_kind}`
  +1 (emitted by the reducer handler that consumes this package).
- Drift suppressed by lineage rotation: no counter; structured log via the
  handler at `failure_class="lineage_rotation"`.
- Allowlist miss for `attribute_drift`: no counter; classifier returns
  empty. Operators must extend the allowlist or accept the gap.

## Anti-patterns specific to this package

- Do NOT do drift comparison inside the rule-pack declaration in
  `../../rules/`. The DSL does not compare values.
- Do NOT embed full attribute maps in `EvidenceAtom.Value` — atoms are
  for join-key carriage and provenance, not full row contents. Use logs
  for high-volume payloads.
- Do NOT call `Classify` in a hot loop against every state row; the
  reducer handler joins on `(backend_kind, locator_hash)` first and only
  passes through addresses that the join surfaced as disagreement
  candidates.
- Do NOT add a backend selection branch (no graph-backend env var
  conditional). Drift correlation is backend-neutral.

## What NOT to change without an ADR

- The dispatch order in `Classify`. Rearranging it changes which drift
  kind wins on ambiguous inputs.
- The closed `DriftKind` enum. Adding a value expands the
  `drift_kind` metric label space and requires a chunk-status row.
- The single-source attribute allowlist. Promotion to a versioned data
  file is design doc §9 Q5 — handle it as an explicit follow-up.
- The cross-scope candidate pattern. Other rule packs may adopt it after
  this one ships, but the engine and `Candidate.Validate` contract must
  remain unchanged.
