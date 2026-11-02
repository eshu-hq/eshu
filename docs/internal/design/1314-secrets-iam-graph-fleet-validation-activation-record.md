# Secrets/IAM Graph Projection — Fleet Validation Activation Record (DRAFT)

Issue [#6228](https://github.com/eshu-hq/eshu/issues/6228). Gate: ADR #1314 §11 /
§12 / §14. Prior proof: [#2430](https://github.com/eshu-hq/eshu/issues/2430).
Backend decision: [#7331](https://github.com/eshu-hq/eshu/issues/7331).

**Status: DRAFT — enactment package only. Nothing here is approved, enabled, or
applied. Every `FILL` field needs an owner signature at enactment.**

This record does **not** extend the existing activation record
(`1314-secrets-iam-graph-activation-record.md`), which authorizes **only**
`remote-amd64-validation/issue-2430-secrets-iam-proof`. That proof ran on
NornicDB and is **not** inheritable: NornicDB is secondary and not supported
for deployment per #7331, so this target needs its own §4-shape flag-on writer
proof against Neo4j.

This is the form a principal/security reviewer and operator complete for the
fleet validation deployment on the EKS cluster. The procedure they follow is
the [activation runbook](1314-secrets-iam-graph-activation-runbook.md). The
exact commands are in the
[companion commands document](1314-secrets-iam-graph-fleet-validation-activation-commands.md).
One record covers exactly one deployment: a second fleet target needs its own
record, not an amendment to this one.

## Preconditions

Already satisfied and current on this base (`3e07d26b1f`):

- [x] ADR §14 principal/security sign-off recorded (2026-06-07, see the proof
      snapshot `1314-secrets-iam-graph-promotion-proof-2026-06-07.md`).
- [x] §11 fixture truth proven
      (`secrets_iam_graph_projection_fixture_truth_test.go`).
- [x] §12 writer benchmark recorded (`BenchmarkSecretsIAMGraphWriter`,
      `go/internal/storage/cypher/cloud/aws/iam/secrets_graph_writer_bench_test.go` (`BenchmarkSecretsIAMGraphWriter`).
- [x] NornicDB + Neo4j live writer conformance + shared backend conformance
      (runbook §1).
- [x] §7 redaction allowlist enforced
      (`TestExtractRowsCarryNoForbiddenProperties`,
      `go/internal/reducer/secretsiam/secrets_iam_graph_projection_extract_test.go` (`TestExtractRowsCarryNoForbiddenProperties`).
      Re-run on this base at package time: the runbook §5b gate passes,
      exit 0 across 95 `ok` packages (see the commands document, evidence E3).
- [x] The projection stays behind the default-off flag
      `ESHU_REDUCER_SECRETS_IAM_GRAPH_PROJECTION_ENABLED` (read in
      `go/cmd/reducer/secrets_iam_graph_wiring.go` (`secretsIAMGraphProjectionEnabled`); empty is OFF,
      malformed is a hard error at reducer start). A nil writer leaves
      `DomainSecretsIAMGraphProjection` unregistered.

Still to capture at enactment (all rows are `FILL` until then):

- [ ] Schema readback: the four `SecretsIAM*` uid constraints + scope indexes
      exist in the **target Neo4j backend**, not only in the proof stacks.
- [ ] §4-shape flag-on writer proof against the **target Neo4j backend**
      (live conformance `PASS`, reducer flag-on `Warn` line, live readback
      counts, sensitive-value spot check).

## 1. `risk:schema` activation approval — APPROVED IN PRINCIPLE; BINDS AT ENACTMENT

The owner approved binding `risk:schema` approval-in-principle to this target
on 2026-11-02 (unblockers comment on #6228: "Target: the fleet validation
deployment on the EKS cluster, canonical backend. Owner approves binding
risk:schema approval-in-principle to this target."). The reviewer completes and
signs this table at enactment. By signing, they attest all four points.

| Field | Value |
| --- | --- |
| Approver (name / role) | FILL: name / role (owner decision names the repo owner / principal) |
| Date | FILL: enactment date |
| Target deployment identifier | FILL: exact deployment identifier (functional description: the fleet validation deployment on the EKS cluster) |
| Backend (Neo4j) | Neo4j — recommended per #7331 (supported canonical backend) and the owner unblockers ("canonical backend"); owner signs to confirm |

Attestations (all `FILL` until signed):

- [ ] The preconditions above are present and current for **this** deployment.
- [ ] The §7 metadata-only redaction contract is the one enforced by
      `TestExtractRowsCarryNoForbiddenProperties`, and no change has widened
      the property allowlist since the §14 sign-off.
- [ ] Data-plane schema bootstrap ran against the **target Neo4j backend**
      before live writer proof and focused reducer/cypher package gates, and
      the §4 schema readback was captured.
- [ ] Schema-level activation is approved for the single target deployment
      named in §2.

**Signature: FILL: name, role, date — binds to the §2 target only.**

## 2. Target-deployment decision — DRAFT; OWNER CONFIRMS AT ENACTMENT

| Field | Value |
| --- | --- |
| Deployment (exactly one) | FILL: exact deployment identifier + target overlay values path (functional description: the fleet validation deployment on the EKS cluster) |
| Backend | Neo4j (recommended; see §1 — owner confirms at signing) |
| Reducer scope(s) projected | FILL: the deployment's generation scope at enactment. Production evidence source is fixed: `reducer/secrets-iam-graph` (`SecretsIAMGraphEvidenceSource` in `go/internal/reducer/secretsiam/secrets_iam_graph_projection_extract.go`). The writer scopes every retract by its own `scope_id` + `evidence_source`. |
| Rollback owner | FILL: name / on-call handle holding rollback (§6 of the runbook) |
| Observation window after flip | FILL: window (recommendation: one full projected generation plus readback and spot check, then the §4 evidence review before declaring complete) |

Confirm no other writer owns the `SecretsIAM*` labels in this deployment (the
writer scopes every retract by its own `scope_id` + `evidence_source`).

## 3. Enable procedure (per-deployment override — defaults stay OFF)

Set the flag on the `resolutionEngine` deployment values for the **named target
only**. Do not edit the chart default (`env: {}` under `resolutionEngine` in
the chart `values.yaml`) and do not edit the shared base values — either would
enable the projection beyond the single approved target, contradicting "one
deployment at a time".

```yaml
# values override for the single approved target deployment ONLY
resolutionEngine:
  enabled: true
  env:
    # Enable only after §1 approval and §2 decision above are signed.
    ESHU_REDUCER_SECRETS_IAM_GRAPH_PROJECTION_ENABLED: "true"
```

The override shape is proven to render onto the reducer Deployment by
`helm template` (evidence E1 in the commands document); the base and the
deploy-checkout EKS overlay (`gitops/argocd/overlays/aws/values.yaml` over base) both render with the flag absent (evidence E2). Apply via
the normal upgrade path for the target (ArgoCD sync — exact app command in the
commands document); confirm the reducer pod restarted and emitted the flag-on
`Warn` line (`secrets/IAM graph projection ENABLED: live exact-only graph
writes are active`, emitted by `secretsIAMGraphProjectionWriter` in `go/cmd/reducer/secrets_iam_graph_wiring.go`). Treat
that line as the operator's flag-on confirmation.

## 4. Flag-on activation proof — TO CAPTURE AT ENACTMENT

Attach the real output; a skipped live test is not evidence. Commands are in
the companion commands document (§3 there).

| Evidence | Captured? | Reference / paste |
| --- | --- | --- |
| Live writer conformance `PASS` against the target Neo4j backend | FILL | `cd go && ESHU_SECRETS_IAM_GRAPH_LIVE=1 ESHU_GRAPH_BACKEND=neo4j go test ./internal/storage/cypher/cloud/aws/iam -run '^TestSecretsIAMGraphWriterLiveConformance$' -count=1 -v` against the target backend (Bolt endpoint, credentials, and database are `FILL`). Must show `PASS`, not a skip — and `[no tests to run]` output is not evidence either, since a `-run` pattern matching nothing exits 0. |
| §7 redaction-allowlist end-to-end no-regression `ok` | FILL | `cd go && go test ./internal/reducer/... ./internal/storage/cypher ./cmd/reducer -run 'SecretsIAMGraph\|SecretsIAM\|TestExtractRowsCarryNoForbiddenProperties' -count=1` on the exact deployed commit. |
| Reducer flag-on `Warn` log line | FILL | Target reducer log showing `secrets/IAM graph projection ENABLED: live exact-only graph writes are active` with `domain=secrets_iam_graph_projection`. |
| Live readback: `SecretsIAM*` rows under the reducer `scope_id` + `evidence_source=reducer/secrets-iam-graph`, allowlisted properties only | FILL | Counts per node family (4) and relationship (5), before retract, scoped to the enactment `scope_id`. |
| Spot-check: no raw ARN, secret value, or Vault path on any node/edge property | FILL | Property-name + value-shape inspection reporting `suspicious_values=0` without printing values. |

## 5. Sign-off to enact

Enactable only when §1, §2, and §3 are signed and §4 evidence is attached
here.

- [ ] Rollback owner confirmed for the §2 target.
- [ ] No redaction regressions (property allowlist §7 enforced end to end).
- [ ] §4 proof captured against the target Neo4j backend.

## What NOT to do

- Do not enable before §1 + §2 are signed.
- Do not inherit the #2430 NornicDB proof for this Neo4j target.
- Do not widen or bypass the §7 property allowlist.
- Do not reach for a worker-count serialization workaround. Upserts remain
  UNWIND-batched, uid-anchored, and idempotent under concurrent delivery.
- Do not fabricate the live proof, and do not enable more than one deployment
  per record. A second fleet target gets its own record.
