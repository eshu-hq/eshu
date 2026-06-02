# AGENTS.md - internal/collector/secretsiam guidance

## Read First

1. `README.md` - package purpose, exported surface, and invariants.
2. `doc.go` - package contract.
3. `types.go` - accepted source observation shapes.
4. `envelope.go` - stable identity and payload construction.
5. `docs/public/guides/collector-authoring.md` - collector authoring contract.

## Invariants

- Keep `CollectorKind` set to `secrets_iam_posture`.
- Emit source facts only. Do not add graph writes, reducer logic, or query
  behavior here.
- Keep policy evidence metadata-only: no raw policy JSON, no statement bodies,
  no condition values, no credentials, and no session tokens.
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
