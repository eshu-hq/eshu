# AGENTS.md - internal/collector/secretsiam guidance

## Read First

1. `README.md` - package purpose, exported surface, and invariants.
2. `doc.go` - package contract.
3. `types.go` - accepted source observation shapes.
4. `envelope.go` - stable identity and payload construction.
5. `kubernetes_types.go` - accepted Kubernetes observation shapes.
6. `kubernetes_envelope.go` - Kubernetes stable identity and payload construction.
7. `vault_types.go` - accepted Vault observation shapes.
8. `vault_envelope.go` - Vault stable identity and payload construction.
9. `docs/public/guides/collector-authoring.md` - collector authoring contract.

## Invariants

- Keep `CollectorKind` set to `secrets_iam_posture`.
- Emit source facts only. Do not add graph writes, reducer logic, or query
  behavior here.
- Keep policy evidence metadata-only: no raw policy JSON, no statement bodies,
  no condition values, no credentials, and no session tokens.
- Keep Kubernetes evidence metadata-only: no raw Secret names, projected tokens,
  ServiceAccount names, namespaces, RBAC subject names, RBAC resourceNames, or
  nonResourceURLs in payloads unless a test proves the value is fingerprinted or
  represented by a bounded count.
- Keep Vault evidence metadata-only: no raw KV paths, key names, custom
  metadata values, policy bodies, policy names, auth role names, mount
  accessors, entity IDs, alias names, Vault tokens, AppRole secret IDs, private
  URLs, or warning messages in payloads. Use fingerprints, counts, and bounded
  capability summaries.
- Preserve provider-native identity needed for stable joins, but keep
  user-facing posture and trust-chain conclusions in reducers.

## Common Changes

- Add a source fact by defining the fact kind in `internal/facts`, writing a
  failing envelope test, then adding a narrow observation type and builder.
- Extend a payload only with additive fields and update the package README plus
  public fact docs when the wire contract changes.
- Add scanner integration in the provider-specific scanner after the envelope
  builder test proves normalization and redaction.

## What Not To Change Without An ADR

- Do not promote IAM trust principals into canonical graph identities here.
- Do not persist raw provider documents or secret-bearing values.
- Do not infer workloads, environments, ownership, or deployment truth from IAM
  names, ARNs, paths, or policy text.
