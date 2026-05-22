# internal/collector/awscloud

## Purpose

`internal/collector/awscloud` owns the runtime-neutral AWS cloud fact contract.
Service scanners convert AWS API observations into reported-confidence facts
through this package before shared collector and reducer paths persist or
materialize them.

## Ownership boundary

This package owns AWS service-kind constants, claim boundary metadata,
reported-confidence envelope builders, scalar redaction helpers, and bounded
scan-status accounting types.

It does not call AWS APIs, schedule workflow claims, choose credentials, write
graph truth, or answer queries. Service packages under `services/*` own source
API mapping; `awsruntime` owns claim execution and scanner selection.

## Exported surface

Use `go doc ./internal/collector/awscloud` for the full contract. The main
surface is:

- Service/resource/relationship constants and collector kind `aws`.
- `Boundary` and observation types for resources, relationships, ECR image
  references, DNS records, warnings, and scan status.
- `New*Envelope` builders for durable fact envelopes.
- `RedactString`, `RedactionPolicyVersion`, and scan-status sanitizers.
- `APICallEvent`, `APICallRecorder`, and `APICallStatsRecorder`.

## Dependencies

- `internal/facts` supplies envelope contracts.
- `internal/redact` supplies deterministic redaction.
- Runtime and service scanner packages import this package; it should not
  import them back.

## Telemetry

This package emits no spans or metrics directly. Runtime and SDK adapter
packages emit claim, credential, service-scan, API-call, throttle, pagination,
duration, resource, relationship, tag, warning, and partial-run signals.

`APICallEvent` must remain bounded to account, region, service, operation,
result, and throttle flag. Do not add ARNs, names, page tokens, raw AWS errors,
policy JSON, or secret material to metric labels or status rows.

## Gotchas / invariants

- Collectors emit evidence, not canonical graph truth.
- Metadata-only services must not read payloads, secrets, policies, object
  bodies, queue messages, log events, database rows, or mutation APIs.
- ECS and Lambda environment values must be redacted before persistence.
- IAM and Route 53 are global services, but claims still carry a region label
  so all AWS claims share one boundary shape.
- EC2 currently emits network topology evidence, not EC2 instance inventory.
- Copy `FencingToken` into every fact envelope so stale workers cannot
  overwrite newer generations.
- New service kinds must stay aligned with `awsruntime.SupportedServiceKinds`
  and command-side target validation.

## Focused tests

```bash
go test ./internal/collector/awscloud -count=1
go test ./internal/collector/awscloud/awsruntime -count=1
go test ./cmd/collector-aws-cloud -count=1
go doc ./internal/collector/awscloud
```

Run service package tests when a scanner or SDK adapter changes.

## Related docs

- `docs/public/services/collector-aws-cloud.md`
- `docs/public/guides/collector-authoring.md`
- `docs/public/reference/environment-variables.md`
- `docs/public/reference/telemetry/index.md`
- `go/internal/collector/awscloud/awsruntime/README.md`
