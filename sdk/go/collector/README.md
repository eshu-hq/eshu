# Collector SDK

This module defines the public Go contracts for out-of-tree Eshu collector
extensions. It intentionally does not import `github.com/eshu-hq/eshu/go/internal`
packages. Core Eshu hosts validate these records before mapping them into the
internal durable fact envelope.

## Compatibility

- Go module path: `github.com/eshu-hq/eshu/sdk/go/collector`
- Initial SDK module semver: `v0.1.x`
- Initial wire protocol: `collector-sdk/v1alpha1`
- Paged wire protocol: `collector-sdk/v1alpha3` (optional `Result.Page`
  continuation; v1alpha1/v1alpha2 single-shot results unchanged)
- JSON Schema artifact: `schema/collector-sdk-v1alpha1.schema.json`
- JSON Schema artifact (paged): `schema/collector-sdk-v1alpha3.schema.json`
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
- `ResultPage` plus `PageBoundForStdout`, `FactWireBytes`, and `SplitFacts`
  for versioned (`collector-sdk/v1alpha3`) multi-call pagination of results
  that would not fit one bounded stdout write.
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
- duplicate decoded JSON member names in a native payload's canonical
  representation, including identical duplicates and escaped equivalent names;
- conflicting duplicates for the same fact kind and stable key.

Native payloads with custom JSON marshaling are checked against their serialized
JSON, including member-name uniqueness at every object depth. The caller must
provide deterministic serialization and keep the result stable from validation
through subsequent host use. `ValidationReport` does not freeze or copy the
payload; concurrent mutation or stateful marshalers can change what is later
emitted.

For `azure_tag_observation` schema `1.0.0` only, the top-level
`tag_value_fingerprints` object may use sensitive-looking tag keys as taxonomy.
Every value must be a full lowercase `redacted:hmac-sha256:` marker. Null maps,
raw values, and the same key under another fact kind or path are rejected. The
generic `ValidateShareSafeKeys` check keeps its context-free key rule.

Kubernetes posture facts have a separate exact root-field exception at schema
`1.0.0`:

| Fact kind | Root fields | Admitted JSON values |
| --- | --- | --- |
| `k8s_service_account`, `k8s_service_account_token_posture` | `secret_ref_count`, `image_pull_secret_ref_count` | number or null |
| Same two kinds | `automount_token` | `"true"`, `"false"`, `"unknown"`, or null |
| `k8s_workload_identity_use` | `projected_service_account_token` | boolean only |

The key check admits numeric scalars without rounding or imposing a range;
configured payload-schema validation still decides whether a count is an
integer. Omitted fields remain valid at this key boundary. Other kinds,
versions, and nested appearances receive no exception. Native and serialized
facts use the same policy, while `IsSensitiveKeyName` and
`ValidateShareSafeKeys` remain strict for these names.

When a component manifest declares `payloadSchemaRef`, the in-tree host and
`eshu component conform` additionally validate that fact's payload against the
referenced `sdk/go/factschema/fixturepack` schema before publication or hosted
activation. Namespaced component facts without a schema ref remain
provenance-only and are not payload-shape validated.

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
