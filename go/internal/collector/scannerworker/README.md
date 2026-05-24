# scannerworker

## Purpose

`scannerworker` defines the contract between workflow claims and isolated
security analyzer workers. It is a boundary package, not a scanner runtime.

## Ownership boundary

This package owns analyzer lane routing, scanner-worker claim input, bounded
target scope, resource limits, source-fact output validation, and retry or
dead-letter payload shape.

Reducers remain truth owners. Scanner workers may emit `scanner_worker.*`
source facts, and may eventually emit source SBOM and attestation facts, but
they must not emit reducer finding facts or graph projection phases.

## Exported surface

- `AnalyzerKind`, `ExecutionLane`, `AnalyzerLane` — route heavy analyzer
  profiles to `scanner_worker` and reducer-owned analysis to `reducer`.
- `TargetScope`, `TargetKind` — bounded target identity copied from workflow
  work items. `LocatorHash` must use `sha256:<64 hex>`.
- `ResourceLimits` — CPU, memory, timeout, input-size, file-count, and
  fact-count limits a runtime must enforce.
- `ClaimInput`, `NewClaimInput`, `NewClaimInputAt` — immutable scanner-worker
  input copied from an active workflow claim and fencing token.
- `FactOutput`, `ValidateFactOutput` — validates fenced source facts and
  rejects silent clean output or reducer-owned finding facts.
- `FailureDisposition`, `FailureClass`, `ResourceUsage`, `FailurePayload`,
  `FailurePayloadFor` — bounded retry and dead-letter payloads that avoid raw
  target locators.

## Dependencies

- `internal/facts` — source fact envelope and scanner/SBOM fact registries.
- `internal/scope` — scanner-worker collector identity.
- `internal/workflow` — work item and claim contracts.

## Telemetry

This package does not emit telemetry directly. Runtime implementations must use
the scanner-worker metrics and spans registered in `internal/telemetry`.

## Gotchas / invariants

- Scanner workers emit source facts only. Reducer finding facts are rejected.
- Completed scanner claims must emit at least one source fact or warning; silent
  clean output is not accepted.
- Retry and dead-letter payloads carry safe locator hashes and bounded failure
  classes, not raw repository paths, image names, registry URLs, package
  coordinates, or bucket keys.
- Claim input rejects mismatched claim IDs, fencing tokens, owners, lease
  expirations, expired claims, and non-scanner workflow items.

## Related docs

- `docs/public/reference/security-intelligence.md`
- `docs/public/reference/collector-reducer-readiness.md`
- `docs/public/reference/telemetry/metrics-ingestion-collectors.md`
