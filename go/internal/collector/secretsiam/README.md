# Secrets/IAM Collector Envelopes

## Purpose

`internal/collector/secretsiam` builds source-fact envelopes for the
`secrets_iam_posture` collector family. It records provider-native IAM identity,
policy, attachment, boundary, instance-profile, optional Access Analyzer, and
coverage-warning evidence without writing graph truth.

## Ownership boundary

This package owns source-fact shape, stable identity, collector kind, and
redaction-safe payload construction for secrets/IAM posture facts. It does not
call AWS, Kubernetes, Vault, Jira, PagerDuty, Postgres, or graph backends, and it
does not decide effective permissions or trust-chain posture.

## Exported surface

See `doc.go` for the godoc contract.

- `EnvelopeContext` carries source scope, generation, claim, and observation
  time fields.
- `NewPrincipalEnvelope`, `NewTrustPolicyEnvelope`,
  `NewPermissionPolicyEnvelope`, `NewPolicyAttachmentEnvelope`,
  `NewPermissionBoundaryEnvelope`, `NewInstanceProfileEnvelope`,
  `NewAccessAnalyzerFindingEnvelope`, and `NewCoverageWarningEnvelope` build
  source facts in the `facts.SecretsIAMSchemaVersionV1` schema.
- Observation structs define the metadata-only inputs accepted by those
  builders.

## Dependencies

- `internal/facts` for durable envelope, fact-kind, schema-version, source-ref,
  and stable-id contracts.

This package has no SDK dependency. Provider adapters normalize source data
before calling it.

## Telemetry

This package emits no metrics, spans, or logs. Runtime adapters and hosted
collector sources own API-call metrics, throttling counters, scan duration,
warnings, and status reporting.

## Gotchas / invariants

- `CollectorKind` is always `secrets_iam_posture`; callers must not use the
  AWS cloud collector envelope helpers for these facts.
- Policy payloads carry condition keys only. Raw policy JSON, statement bodies,
  condition values, AWS credentials, and session tokens stay outside the fact
  contract.
- Stable keys exclude generation IDs so repeated source observations can be
  compared across generations. Fact IDs include generation IDs through the
  envelope boundary.
- Builders emit source facts only. Reducers own all graph promotion, trust-chain
  joins, and effective-permission decisions.

## Related docs

- `docs/public/guides/collector-authoring.md`
- `docs/public/reference/fact-schema-versioning.md`
