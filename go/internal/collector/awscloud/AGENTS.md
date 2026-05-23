# AGENTS.md - internal/collector/awscloud

Read `README.md`, `doc.go`, `types.go`, `envelope.go`, `apicall.go`,
`scan_status.go`, `redaction.go`, the relevant service README, and the AWS
collector public docs before editing this subtree.

## Mandatory Rules

- Keep this package runtime-neutral. It defines AWS service constants,
  boundaries, reported-confidence observations, envelope builders, redaction,
  API-call accounting, and scan status only.
- Do not call AWS APIs, load credentials, schedule claims, write graph truth,
  answer queries, or infer workload, environment, owner, repository, or
  deployable-unit truth here.
- Preserve explicit claim boundaries: collector instance, account, region,
  service kind, scope, generation, and fencing token.
- Keep `FactID` generation-specific and `StableFactKey` source-stable.
- Copy `FencingToken` into every AWS fact envelope.
- Keep `APICallEvent` bounded to account, region, service, operation, result,
  and throttle state.
- Redact ECS and Lambda environment values with `RedactString`; keep
  `RedactionPolicyVersion` versioned and fail closed on unknown secret-shaped
  environment inputs.
- Keep service data allowlists in service packages and public scanner docs. Do
  not widen a scanner allowlist without tests and docs.
- Keep SDK pagination, retry classification, throttling, and credential loading
  in `awsruntime` or service `awssdk` adapters.
- Never put secrets, session tokens, presigned URLs, policy JSON, raw payloads,
  raw AWS errors, tags, names, ARNs, paths, credentials, or page tokens in
  metric labels.

## Proof

- Run `cd go && go test ./internal/collector/awscloud -count=1` for shared
  contract edits.
- Also run changed service tests and
  `cd go && go test ./internal/collector/awscloud/awsruntime -count=1` when
  service-kind, scanner registry, or runtime boundaries change.
