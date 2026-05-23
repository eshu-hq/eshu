# AWS Cloud Collector Contract

## Purpose

`internal/collector/awscloud` defines the runtime-neutral AWS cloud fact
contract. Service scanners use it to turn AWS API observations into
reported-confidence fact envelopes and bounded scan-status records.

## Ownership Boundary

This package owns AWS service/resource constants, claim boundary metadata,
observation types, envelope builders, redaction helpers, API-call accounting,
and scan-status sanitizers. It does not call AWS APIs, schedule claims, choose
credentials, persist facts, write graph truth, or answer queries.

## Exported Surface

See `doc.go` and `go doc ./internal/collector/awscloud`. Main exports include
service and resource constants, `Boundary`, observation structs,
`New*Envelope` builders, `RedactString`, scan-status helpers, `APICallEvent`,
`APICallRecorder`, and `APICallStatsRecorder`.

## Telemetry

This package emits no spans or metrics directly. `awsruntime` and service SDK
adapters emit claim, credential, API-call, throttle, pagination, duration,
resource, relationship, tag, warning, and partial-run signals.

## Gotchas / Invariants

- Envelope builders must preserve boundary fields, generation identity, and
  reported confidence.
- Redaction must happen before provider payloads are persisted or logged.
- API-call accounting must stay bounded by service, account, region, operation,
  and result-style labels.
- Adding a service kind requires updates in service scanners, `awsruntime`
  registry support, tests, and public collector docs.

## Focused Tests

```bash
cd go
go test ./internal/collector/awscloud -count=1
go run ./cmd/eshu docs verify ../go/internal/collector/awscloud --limit 1000 \
  --fail-on contradicted,missing_evidence
```

## Related Docs

- `go/internal/collector/awscloud/awsruntime/README.md`
- `docs/public/services/collector-aws-cloud.md`
- `docs/public/deployment/service-runtimes.md`
