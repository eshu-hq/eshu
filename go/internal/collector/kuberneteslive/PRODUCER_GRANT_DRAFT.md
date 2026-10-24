# Producer Grant DRAFT (`#8859`, `#6709`)

**DRAFT — not issued, not stored, not enforced.** Records the intended
first-party delegation for the extracted `kubernetes_live` producer so review
can approve the exact kind/version/scope binding before cutover. Core retains
fact admission, canonical identity, projection, persistence, and retraction;
this grant authorizes emission only.

## Proposed grant identity

- Producer: `dev.eshu.collector.kubernetes-live`
- Version: `0.1.0` (collector artifact version at cutover)
- Scope: `kubernetes_live`
- Kinds — `kubernetes_live` family (`cloud/kubernetes_live.go`):
  `kubernetes_live.namespace` (`1.0.0`), `kubernetes_live.pod_template`
  (`1.3.0`), `kubernetes_live.relationship` (`1.0.0`),
  `kubernetes_live.warning` (`1.0.0`)
- Kinds — posture family (`SecretsIAMSchemaVersionV1`, all `1.0.0`):
  `k8s_service_account`, `k8s_service_account_token_posture`,
  `k8s_rbac_role`, `k8s_rbac_binding`, `k8s_workload_identity_use`,
  `k8s_gcp_workload_identity_binding`, `eks_irsa_annotation`,
  `secrets_iam_coverage_warning`
  (`eks_pod_identity_association` excluded: this collector never emits it)
- Lifetime: bounded `--expires-in` (rotation expected; no non-expiring grants)

## Issuance (operator commands at cutover)

One grant per core-owned kind (the CLI binds one kind per grant):

```bash
for kind in namespace relationship warning; do
  eshu component grant dev.eshu.collector.kubernetes-live \
    --component-home ~/.eshu/components \
    --version 0.1.0 \
    --kind "kubernetes_live.${kind}" \
    --schema-version 1.0.0 \
    --scope kubernetes_live \
    --expires-in 2160h
done
eshu component grant dev.eshu.collector.kubernetes-live \
  --component-home ~/.eshu/components \
  --version 0.1.0 \
  --kind "kubernetes_live.pod_template" \
  --schema-version 1.3.0 \
  --scope kubernetes_live \
  --expires-in 2160h
for kind in k8s_service_account k8s_service_account_token_posture \
    k8s_rbac_role k8s_rbac_binding k8s_workload_identity_use \
    k8s_gcp_workload_identity_binding eks_irsa_annotation \
    secrets_iam_coverage_warning; do
  eshu component grant dev.eshu.collector.kubernetes-live \
    --component-home ~/.eshu/components \
    --version 0.1.0 \
    --kind "${kind}" \
    --schema-version 1.0.0 \
    --scope kubernetes_live \
    --expires-in 2160h
done
eshu component grants --component-home ~/.eshu/components \
  --producer dev.eshu.collector.kubernetes-live
```

Grant-first-then-install: authorize before activating the replacement, which
is the order cutover needs.

## Enforcement (already core-owned, `#6709`)

- Admit only when producer identity, version, kind, schema version, and scope
  all match a live, unexpired, unrevoked stored grant; anything else fails
  closed at install, activation, and every emission.
- Registry read failures deny core-owned kinds rather than emitting under
  unknown authorization.
- Revocation (`component revoke-grant …`) fails the next emission terminal;
  absent-grant revocation returns `grant_not_found`, never silent success.

## First-party note

First-party status changes packaging trust, not authority: the extracted
producer still emits through the grant like any external producer. Delegation
does not transfer canonical ownership — no graph edges, no identity choice,
no queue or storage access.

## Pre-issuance checklist

- [ ] `#6710` template repo exists; manifest version equals grant version.
- [ ] Parity proof green on the fixture pack (`#4047` gate input).
- [ ] Posture `source_uri` hardening decided (sanitize-or-drop in the
      producer, or shared-seam fix landed) — see EXTRACTION_PREP §10.
- [ ] Rollback rehearsed ([ROLLBACK_DRAFT](ROLLBACK_DRAFT.md)).
- [ ] Expiry/rotation owner and alert recorded before issuance.
