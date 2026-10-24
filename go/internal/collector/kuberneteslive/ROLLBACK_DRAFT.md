# Rollback DRAFT (`#8859`, `#4047` cutover)

**DRAFT — rehearsal plan, not an executed runbook.** Authority for a source
scope and generation must rest with exactly one producer at a time.

This collector is poll-driven (no claim dispatcher, no leases: claim-driven
collection is deferred), so rollback stops a deployment rather than draining
a queue. Every step below still applies; the claim-drain step is a
verification, not a wait.

## Trigger

Any cutover failure: parity mismatch, admission errors, projection/graph
divergence, missing telemetry, or expired/revoked grant blocking emission.

## Steps

### 1. Stop new external claims and polls

Halt the external `collector-kubernetes-live` deployment (and any claim
dispatcher targets for `collector_kind = kubernetes_live`, once claim-driven
collection exists) so no new result reaches the core. In-flight state is
poll-atomic per cluster: a killed poll commits nothing partial, because the
in-tree `collector.Service` commits one whole cluster generation or nothing.

### 2. Drain or expire in-flight leases

Confirm zero pending external results for `kubernetes_live` before
proceeding. Today this is a check, not a wait: the poll path holds no lease
(the workflow control store is uninvolved until claim-driven collection
lands). After that landing, wait for in-flight external results to land or
let the lease TTL expire, with the workflow control store
(`postgres.NewWorkflowControlStore`) as the source of truth — not the
producer's logs.

### 3. Revoke the external grant

```bash
for kind in namespace pod_template relationship warning \
    k8s_service_account k8s_service_account_token_posture \
    k8s_rbac_role k8s_rbac_binding k8s_workload_identity_use \
    k8s_gcp_workload_identity_binding eks_irsa_annotation \
    secrets_iam_coverage_warning; do
  if [ "$kind" = "pod_template" ]; then ver=1.3.0; else ver=1.0.0; fi
  case "$kind" in
    namespace|pod_template|relationship|warning) k="kubernetes_live.${kind}" ;;
    *) k="${kind}" ;;
  esac
  eshu component revoke-grant dev.eshu.collector.kubernetes-live \
    --component-home ~/.eshu/components \
    --version 0.1.0 \
    --kind "${k}" \
    --scope kubernetes_live
done
```

Revocation fails the next external emission closed, so a stray retry cannot
re-establish dual authority.

### 4. Restore the in-tree producer

Reactivate the in-tree `collector-kubernetes-live` deployment (live or
`-mode=cassette` for diagnosis) with the last supported configuration.
In-tree needs no grant: it commits through `postgres.NewIngestionStore`
(`service.go:56`) under core admission.

### 5. Replay from the last committed generation

Resume polling from the last generation the core committed — never from the
external producer's last attempt. Poll generations are time-derived per
cluster (`clusterGenerationID`) and every fact carries a deterministic stable
key (`identity.go:63-73`), so replay is idempotent; duplicate delivery is
safe (pinned by `TestKubernetesLive8859ParityDuplicateDeliveryIsStable`).

### 6. Verify no dual authority, no lost facts

- Exactly one producer holds each scope: query recent committed generations
  for `collector_kind IN (kubernetes_live, secrets_iam_posture)` on the
  cluster scopes; every generation's `collector_instance_id` must name the
  restored producer.
- Fact parity: committed kind counts per scope equal the pre-cutover
  baseline; canonical projection and graph truth agree
  (`projector/kubernetes/`, `reducer/kubernetescorrelation/`,
  `reducer/secretsiam/`).
- Read truth: `GET /api/v0/kubernetes/correlations` and
  `list_kubernetes_correlations` return the baseline rows.
- Telemetry: `eshu_dp_kubernetes_*` counters resume from the in-tree
  binary; the grant-denial path is quiet (no external emissions attempted).

## Rehearsal requirement

Rollback is proven only by rehearsal: cut over a non-production scope,
revoke, restore, replay, and verify the checks above before production
cutover. Record the rehearsal evidence on the cutover issue.
