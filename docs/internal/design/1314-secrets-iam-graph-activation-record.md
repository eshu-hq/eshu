# Secrets/IAM Graph Projection — Activation Record

Issue [#2430](https://github.com/eshu-hq/eshu/issues/2430). Current gate
closeout: [#2406](https://github.com/eshu-hq/eshu/issues/2406). Historical
gates: [#1381](https://github.com/eshu-hq/eshu/issues/1381) /
[#1347](https://github.com/eshu-hq/eshu/issues/1347). Gate: ADR #1314 §11 /
§12 / §14.

**Status: REMOTE-VALIDATION PROOF CAPTURED — repository, chart, and operator
defaults remain OFF; rollback owner still requires operator confirmation.**
`risk:schema` approval in principle was recorded 2026-06-11 (§1) and bound on
2026-06-16 to the target named in §2 for proof collection only. The flag was
enabled only for the transient remote-validation reducer proof in §4. This
record does not authorize any other deployment or any default flip.

This record does not grant approval for any other target. It is the form a
principal/security reviewer and operator complete. The procedure they follow is
the [activation runbook](1314-secrets-iam-graph-activation-runbook.md); the
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
| Target deployment identifier | `remote-amd64-validation/issue-2430-secrets-iam-proof` |
| Backend (NornicDB or Neo4j) | NornicDB |

The repo owner gave `risk:schema` activation approval in principle on 2026-06-11.
This approval bound on 2026-06-16 to the single remote-validation target named
in §2. Approval remains target-bound and does **not** enable repository,
chart, operator, or any second deployment default.

Attestations:

- [x] The preconditions above are present and current for **this** deployment.
- [x] The §7 metadata-only redaction contract is the one enforced by
      `TestExtractRowsCarryNoForbiddenProperties`, and no change has widened the
      property allowlist since the §14 sign-off.
- [x] Data-plane schema bootstrap ran against the **target** backend before live
      writer proof and focused reducer/cypher package gates. Direct target
      constraint/index readback was not captured in this record.
- [x] Schema-level activation is approved in principle, to bind to the single
      target deployment named in §2.

**Signature: Allen Sanabria (repo owner), 2026-06-11 — approval in principle;
bound on 2026-06-16 to `remote-amd64-validation/issue-2430-secrets-iam-proof`
only.**

## 2. Target-deployment decision — PROOF CAPTURED; OWNER PENDING

| Field | Value |
| --- | --- |
| Deployment (exactly one) | `remote-amd64-validation/issue-2430-secrets-iam-proof` |
| Backend | NornicDB default canonical backend |
| Reducer scope(s) projected | Ephemeral live-proof scope only: `scope:test:secrets-iam-live:<nonce>`; production evidence source remains `reducer/secrets-iam-graph` |
| Rollback owner | _TBD — operator confirmation required before #2430 closeout_ |
| Observation window after flip | 2026-06-16 19:14:49Z remote-validation proof window for transient flag-on reducer startup, live writer readback, focused reducer/cypher package gates, and cleanup verification |

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

## 4. Flag-on activation proof — CAPTURED

Attach the real output; a skipped live test is not evidence. Commands are in the
runbook §5.

| Evidence | Captured? | Reference / paste |
| --- | --- | --- |
| Live writer conformance `PASS` against the target backend | [x] | 2026-06-16 remote target, NornicDB with schema bootstrapped: `cd go && ESHU_SECRETS_IAM_GRAPH_LIVE=1 ESHU_GRAPH_BACKEND=nornicdb go test ./internal/storage/cypher -run '^TestSecretsIAMGraphWriterLiveConformance$' -count=1 -v` passed. |
| §7 redaction-allowlist end-to-end no-regression `ok` | [x] | `cd go && go test ./internal/reducer ./internal/storage/cypher ./cmd/reducer -run 'SecretsIAMGraph|SecretsIAM|TestExtractRowsCarryNoForbiddenProperties' -count=1` passed. |
| Reducer flag-on `Warn` log line | [x] | Transient reducer startup emitted `secrets/IAM graph projection ENABLED: live exact-only graph writes are active` with `domain=secrets_iam_graph_projection` and `env_var=ESHU_REDUCER_SECRETS_IAM_GRAPH_PROJECTION_ENABLED`; the reducer reached its normal worker startup log and stayed up until the 8s validation timeout after #2653's truth-contract fix. |
| Live readback: `SecretsIAM*` rows under the reducer `scope_id` + `evidence_source`, allowlisted properties only | [x] | Sanitized live counts before retract: `SecretsIAMServiceAccount=1`, `SecretsIAMVaultAuthRole=1`, `SecretsIAMVaultPolicy=1`, `SecretsIAMSecretMetadataPath=1`; relationships `SECRETS_IAM_USES_SERVICE_ACCOUNT=1`, `SECRETS_IAM_ASSUMES_IAM_ROLE=1`, `SECRETS_IAM_AUTHENTICATES_TO_VAULT_ROLE=1`, `SECRETS_IAM_USES_VAULT_POLICY=1`, `SECRETS_IAM_GRANTS_SECRET_READ=1`. |
| Spot-check: no raw ARN, secret value, or Vault path on any node/edge property | [x] | The redaction gate above passed; the live proof inspected backend node and relationship properties and logged `suspicious_values=0` without printing values. |

No-Regression Evidence: the live NornicDB writer conformance originally failed
on scoped retract with `SecretsIAMServiceAccount survived retract: count = 1`.
After changing retracts from list/`UNWIND` mutation predicates to scalar
`scope_id` statements executed sequentially, the same live target passed in
under 0.1s with all four node counts and all five relationship counts equal to one
before retract and zero reducer-owned nodes after retract. The same branch also
fixes #2653 by registering the Secrets/IAM graph projection truth contract with
`observed_resource` source evidence instead of the output-only
`canonical_asset` layer, allowing the flag-on reducer startup to remain running
until the validation timeout. Local gates also passed:
`go test ./internal/storage/cypher -count=1`, `go test ./internal/reducer
./internal/storage/cypher ./cmd/reducer -run
'SecretsIAMGraph|SecretsIAM|TestExtractRowsCarryNoForbiddenProperties'
-count=1`, and the reducer flag wiring tests.

No-Observability-Change: secrets/IAM graph writes and retracts still flow
through `SecretsIAMGraphWriter` statement metadata with
`phase=secrets_iam_graph`, entity labels, existing executor error wrapping,
existing reducer flag-on warning, and existing graph-write spans/metrics. This
change adds no metric name, metric label, worker, queue domain, runtime knob,
backend branch, or new graph-write route.

## 5. Sign-off to close #2430

Closeable only when §1, §2, §3, and §4 are complete and the evidence is attached
here.

- [ ] Rollback owner confirmed for
      `remote-amd64-validation/issue-2430-secrets-iam-proof`.
- [x] No redaction regressions (property allowlist §7 enforced end to end).
- [ ] #2430 closed with this record linked.

## What NOT to do

- Do not enable before §1 + §2 are signed.
- Do not widen or bypass the §7 property allowlist.
- Do not reach for a worker-count serialization workaround. Upserts remain
  UNWIND-batched, uid-anchored, and idempotent under concurrent delivery; scoped
  retracts intentionally execute one bounded scalar-scope cleanup statement at
  a time because the target NornicDB mutation route did not delete correctly
  through list/`UNWIND` predicates.
- Do not fabricate the live proof, and do not enable more than one deployment per
  record.
