# Collector SDK

This module defines the public Go contracts for out-of-tree Eshu collector
extensions. It intentionally does not import `github.com/eshu-hq/eshu/go/internal`
packages. Core Eshu hosts validate these records before mapping them into the
internal durable fact envelope.

## Compatibility

- Go module path: `github.com/eshu-hq/eshu/sdk/go/collector`
- Initial SDK module semver: `v0.1.x`
- Initial wire protocol: `collector-sdk/v1alpha1`
- JSON Schema artifact: `schema/collector-sdk-v1alpha1.schema.json`
- Cassette format JSON Schema (replay fixtures): `schema/cassette-format.v1.schema.json`
  — a generated mirror of the host's cassette envelope contract
  (`go/internal/replay/schema`), so credential-free replay cassettes can be
  validated offline against the same schema the host enforces.

The module version may add helpers without changing the wire protocol. The wire
protocol changes only when host and extension records would serialize
differently.

## Contracts

The public surface includes:

- `Claim`, `Scope`, and `Generation` for core-owned work identity.
- `Fact`, `SourceRef`, and `Redaction` for source evidence.
- `Status` and `Result` for complete, unchanged, partial, retryable, and
  terminal outcomes.
- `Contract`, `FactDeclaration`, `Validator`, and `ValidationReport` for
  fail-closed host validation.

Collectors emit facts only. They do not write graph truth, mutate queues, run
DDL, or import Eshu internals.

## Validation

`NewValidator(contract).ValidateResult(result)` rejects:

- unsupported protocol versions or result states;
- undeclared fact kinds, schema versions, source confidence values, and
  tombstones;
- `source_confidence=unknown`;
- blank or control-character stable keys;
- source references outside the claimed scope/generation/fact key;
- credential-bearing or host-local source URIs;
- payload keys that look like credentials and were not redacted before emission;
- conflicting duplicates for the same fact kind and stable key.

Exact duplicate facts are accepted as idempotent and reported in
`ValidationReport.DuplicateCount`. Conflicting duplicates fail before host
commit.

## Fixtures

`testdata/fixtures` contains golden result fixtures for complete, unchanged,
partial, retryable, terminal, duplicate, conflict, tombstone, and redaction
cases. The package tests validate those fixtures against the public contract and
assert the generated JSON Schema stays in lockstep with the checked-in artifact.

No-Observability-Change: this SDK module has no runtime, network, queue, graph,
or telemetry emission path. Host runtime telemetry is owned by later core
adapter work.
