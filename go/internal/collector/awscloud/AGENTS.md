# AGENTS.md - internal/collector/awscloud

Use `README.md` and `doc.go` for the shared AWS fact contract. This file keeps
the agent-only guardrails for edits under `internal/collector/awscloud`.

## Read First

1. `README.md`, `types.go`, `envelope.go`, `apicall.go`, `scan_status.go`,
   and `redaction.go`.
2. `awsruntime/README.md` before changing claim execution or scanner registry
   behavior.
3. Service and `awssdk` READMEs under `services/` before changing a scanner or
   SDK adapter allowlist.
4. `docs/public/services/collector-aws-cloud.md`,
   `docs/public/services/collector-aws-cloud-security.md`, and
   `docs/public/services/collector-aws-cloud-scanners.md`.
5. `docs/public/guides/collector-authoring.md` for the generic fact contract.

## Mandatory Invariants

- AWS observations are reported source evidence. Do not add graph writes,
  reducer admission, query behavior, or workload/environment inference here.
- The claim boundary stays explicit: account, region, service kind, scope,
  generation, collector instance, and fencing token.
- `FactID` is generation-specific; `StableFactKey` is source-stable.
- Copy `FencingToken` into every AWS fact envelope.
- `APICallEvent` must stay bounded to account, region, service, operation,
  result, and throttle state.
- ECS and Lambda environment values must pass through `RedactString`; keep
  `RedactionPolicyVersion` versioned and fail closed on unknown environment
  keys.
- Service-specific forbidden data classes live in the service READMEs and the
  AWS scanner public doc. Do not widen a scanner allowlist without tests,
  public/package docs, and architecture-owner approval.
- AWS SDK pagination, retries, throttling, and credential loading belong in
  runtime/adapters, not this shared fact package.
- Secrets, session tokens, presigned URLs, policy JSON, raw payloads, raw AWS
  errors, tags, names, ARNs, paths, and credentials MUST NOT become metric
  labels.

## Change Routing

- New service kind: add constants here, scanner and `awssdk` packages, package
  docs, scanner tests, and a `DefaultScannerFactory` branch; keep command-side
  validation aligned with `awsruntime.SupportedServiceKinds`.
- New fact envelope: add the fact kind/schema in `internal/facts` first, then
  add envelope tests here.
- Redaction or credential behavior usually belongs at the runtime boundary
  unless the value is part of the durable envelope contract.
- Any pagination fanout, claim concurrency, batching, queue pressure, or
  downstream graph/materialization pressure needs tracked performance and
  observability evidence.

## Do Not Change Without Architecture-Owner Approval

- Do not make this package call AWS APIs directly.
- Do not infer environment, workload, ownership, or deployable-unit truth from
  names, tags, folders, ARNs, accounts, or aliases.
- Do not move scanner-specific payload allowlists out of scanner package docs.

## Required Proof

- Run `cd go && go test ./internal/collector/awscloud -count=1`.
- Run changed service package tests and `./internal/collector/awscloud/awsruntime`
  tests when scanner registry or runtime boundaries change.
- For docs-only edits, run `go run ./cmd/eshu docs verify ../go/internal/collector/awscloud --fail-on contradicted,missing_evidence` from `go/`.
