# Secrets/IAM Graph Projection — Activation Record

Issue [#2430](https://github.com/eshu-hq/eshu/issues/2430). Current gate
closeout: [#2406](https://github.com/eshu-hq/eshu/issues/2406). Historical
gates: [#1381](https://github.com/eshu-hq/eshu/issues/1381) /
[#1347](https://github.com/eshu-hq/eshu/issues/1347). Gate: ADR #1314 §11 /
§12 / §14.

**Status: NOT ACTIVATED — `risk:schema` approval in principle recorded
2026-06-11 (§1); awaiting §2 target-deployment decision and §4 flag-on live
proof.** This is the durable governance + evidence record that closes #2430.
The flag is enabled in exactly one target deployment **only after** §2 names it
and §4 proof is captured. An empty section means that gate is not yet closed; do
not enable the flag.

This record does not grant approval. It is the form a principal/security
reviewer and an operator complete. The procedure they follow is the
[activation runbook](1314-secrets-iam-graph-activation-runbook.md); the
preconditions are recorded in the
[proof snapshot](1314-secrets-iam-graph-promotion-proof-2026-06-07.md); the gate
contract is [ADR #1314](1314-secrets-iam-graph-promotion-gate.md).

## Preconditions (already satisfied — see the proof snapshot)

- [x] ADR §14 principal/security sign-off recorded (2026-06-07).
- [x] §11 fixture truth proven (`secrets_iam_graph_projection_fixture_truth_test.go`).
- [x] §12 writer benchmark recorded (`BenchmarkSecretsIAMGraphWriter`).
- [x] NornicDB + Neo4j live writer conformance + shared backend conformance.
- [x] Schema readback: four `SecretsIAM*` uid constraints + scope indexes.
- [x] §7 redaction allowlist enforced (`TestExtractRowsCarryNoForbiddenProperties`).

The projection stays behind the default-off flag
`ESHU_REDUCER_SECRETS_IAM_GRAPH_PROJECTION_ENABLED` (read in
`go/cmd/reducer/secrets_iam_graph_wiring.go`); a nil writer leaves
`DomainSecretsIAMGraphProjection` unregistered, so no live graph write happens
until the flag is enabled in a target deployment.

## 1. `risk:schema` activation approval — APPROVED IN PRINCIPLE; BINDS ON §2

The principal/security reviewer responsible for the schema/risk surface
completes and signs this. By signing, they attest all four points.

| Field | Value |
| --- | --- |
| Approver (name / role) | Allen Sanabria (repo owner / principal) |
| Date | 2026-06-11 |
| Target deployment identifier | _TBD — required before enablement (see §2)_ |
| Backend (NornicDB or Neo4j) | _TBD — required before enablement (see §2)_ |

The repo owner gave `risk:schema` activation approval in principle on 2026-06-11.
This approval **binds to a specific deployment once §2 names one**; the two
deployment-specific attestations below stay unchecked until the target backend
is named and verified. Approval in principle does **not** enable the flag and
does **not** close #2430 — §2 (target decision), §3 (enable in that
deployment only), and §4 (flag-on live proof) still gate enablement.

Attestations:

- [ ] The preconditions above are present and current for **this** deployment.
      _(Pending: no target deployment named.)_
- [x] The §7 metadata-only redaction contract is the one enforced by
      `TestExtractRowsCarryNoForbiddenProperties`, and no change has widened the
      property allowlist since the §14 sign-off.
- [ ] The four `SecretsIAM*` uid constraints and scope indexes exist in the
      **target** backend, not only in the proof stacks. _(Pending: no target
      backend named.)_
- [x] Schema-level activation is approved in principle, to bind to the single
      target deployment named in §2.

**Signature: Allen Sanabria (repo owner), 2026-06-11 — approval in principle,
binds on §2.**

## 2. Target-deployment decision — TO BE COMPLETED

| Field | Value |
| --- | --- |
| Deployment (exactly one) | _TBD_ |
| Backend | _TBD_ (NornicDB default canonical / Neo4j compatibility) |
| Reducer scope(s) projected | _TBD_ |
| Rollback owner | _TBD_ |
| Observation window after flip | _TBD_ |

Confirm no other writer owns the `SecretsIAM*` labels in this deployment (the
writer scopes every retract by its own `scope_id` + `evidence_source`).

## 3. Enable procedure (per-deployment override — default stays OFF)

Set the flag on the `resolutionEngine` deployment values for the **named target
only**. Do not edit the chart default (`resolutionEngine.env: {}` in
`deploy/helm/eshu/values.yaml`) — that would enable the projection for every
deployment, contradicting "one deployment at a time".

```yaml
# values override for the single approved target deployment ONLY
resolutionEngine:
  enabled: true
  env:
    # Enable only after §1 approval and §2 decision above are signed.
    ESHU_REDUCER_SECRETS_IAM_GRAPH_PROJECTION_ENABLED: "true"
```

Apply via the normal upgrade path; confirm the reducer pod restarted and emitted
the flag-on `Warn` line (`secrets/IAM graph projection ENABLED: live exact-only
graph writes are active`). Treat that line as the operator's flag-on
confirmation.

## 4. Flag-on activation proof — TO BE CAPTURED (operator)

Attach the real output; a skipped live test is not evidence. Commands are in the
runbook §5.

| Evidence | Captured? | Reference / paste |
| --- | --- | --- |
| Live writer conformance `PASS` against the target backend | [ ] | _TBD_ |
| §7 redaction-allowlist end-to-end no-regression `ok` | [ ] | _TBD_ |
| Reducer flag-on `Warn` log line | [ ] | _TBD_ |
| Live readback: `SecretsIAM*` rows under the reducer `scope_id` + `evidence_source`, allowlisted properties only | [ ] | _TBD_ |
| Spot-check: no raw ARN, secret value, or Vault path on any node/edge property | [ ] | _TBD_ |

## 5. Sign-off to close #2430

Closeable only when §1, §2, §3, and §4 are complete and the evidence is attached
here.

- [ ] All four sections complete and signed.
- [ ] No redaction regressions (property allowlist §7 enforced end to end).
- [ ] #2430 closed with this record linked.

## What NOT to do

- Do not enable before §1 + §2 are signed.
- Do not widen or bypass the §7 property allowlist.
- Do not reach for a serialization workaround (the writer is UNWIND-batched,
  uid-anchored, idempotent under concurrent delivery).
- Do not fabricate the live proof, and do not enable more than one deployment per
  record.
