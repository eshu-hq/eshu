# AGENTS.md - internal/collector/awscloud/awsruntime

Use `README.md` and `doc.go` for runtime ownership and exported contracts. This
file is the scoped checklist for claim, credential, registry, and scan-status
changes.

## Read First

1. `README.md`, `types.go`, `credentials.go`, `registry.go`, `source.go`, and
   `scan_status.go`.
2. `../README.md` for the shared AWS fact-envelope contract.
3. `../checkpoint/README.md` for pagination checkpoint semantics.
4. Service and `awssdk` READMEs under `../services/` before changing scanner
   allowlists or SDK calls.
5. `docs/public/services/collector-aws-cloud.md` and
   `docs/public/services/collector-aws-cloud-security.md`.

## Mandatory Invariants

- Authorize `(account_id, region, service_kind)` before acquiring credentials.
- Keep static AWS credentials out of this package and production config.
- Preserve `aws.RetryModeAdaptive` on loaded AWS SDK configs.
- Central AssumeRole targets require same-account role routing and external ID.
  Local workload-identity targets must not carry AssumeRole fields.
- Copy `CurrentFencingToken` into every AWS boundary and warning fact.
- Expire prior-generation pagination checkpoints before scanner construction.
- Release credential leases even when scanner construction or scanning fails.
- Scanner status is not durable fact commit status; keep scanner-side status
  and commit-side status separate.
- Keep scanner allowlists metadata-only unless the service README and public
  scanner doc explicitly approve a sanitized exception.
- ARNs, names, tags, policy JSON, payloads, page tokens, raw AWS errors, and
  credential material stay out of metric labels and status rows.

## Change Routing

- New credential mode: extend `CredentialMode`, add focused claim/security
  tests, and implement the provider here.
- New service scanner: add the `awscloud` service constant, scanner package,
  `awssdk` adapter, package docs, registry branch, and
  `supportedServiceKinds` entry together.
- Claim shape changes require coordinator/workflow alignment, public collector
  docs, retry/idempotency proof, and status/telemetry proof in the same PR.
- Pagination, lease, fanout, batching, or queue-pressure changes need tracked
  performance and observability evidence.

## Do Not Change Without Owner Approval And Proof

- Do not bypass workflow claims or claim fencing.
- Do not cache cross-account credentials beyond a claim lease.
- Do not infer environment, workload, ownership, or deployable-unit truth in
  the runtime.

## Required Proof

- Run `cd go && go test ./internal/collector/awscloud/awsruntime -count=1`.
- Run `cd go && go test ./cmd/collector-aws-cloud -count=1` when command
  validation or runtime config is affected.
- For docs-only edits, run `go run ./cmd/eshu docs verify ../go/internal/collector/awscloud/awsruntime --fail-on contradicted,missing_evidence` from `go/`.
