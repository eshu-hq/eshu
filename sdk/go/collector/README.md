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
- `SSMHostInventoryLimits`, `AssessSSMHostCapture`,
  `SSMHostInventoryOutcome`, and `SSMHostInventoryAggregates` for bounded
  SSM host inventory collection (eshu-hq/eshu#9556).

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

## SSM host inventory collection contract

`ssm_host_inventory.go` and `ssm_host_capture.go` bound the SSM host
inventory collection from the accepted #9302 record (eshu-hq/eshu#9556).
The satellite collector imports these helpers; core never configures
collection from them.

- **Ceilings** (`SSMHostInventoryMaxEntriesPerHostPerCapture` 5000,
  `SSMHostInventoryMaxPagesPerHostPerTypePerPoll` 100,
  `SSMHostInventoryMaxEntryBytes` 1024, `SSMHostInventoryMaxPageBytes`
  50 KiB): arbiter-set from the 2-node captured cohort. Configured values
  must not exceed them without a new measurement.
- **Limits** (`SSMHostInventoryLimits.Validate`): every field is required —
  host, page, entry, age, skew, polling, and retention limits plus an
  explicit target list (no unlimited default). Missing limits fail closed;
  above-ceiling values fail; freshness must cohere (stale age at least the
  capture interval, skew within the stale age).
- **Freshness**: `CaptureIsStale` (stale-age bound, recommended two capture
  intervals), `CrossTypeSkewOK` (application/platform capture skew, AWS
  promises no equal capture times across types), and
  `PlatformObservationIsFresh` (the separate `DescribeInstanceInformation`
  rule on observation age — that source has no inventory capture time).
- **Outcomes** (`SSMHostInventoryOutcome`): `denied`, `partial`, `stale`,
  and `successful`, mapped onto result states (`ToResultState`:
  terminal, partial, unchanged, complete). Only successful inventory may
  supersede the prior generation; a denial is not a zero inventory result.
- **Capture assessment** (`AssessSSMHostCapture`): a complete snapshot needs
  every page (valid nonrepeating pagination tokens), a consistent instance
  ID, type, schema version, and capture time within the stream, volume
  within the ceilings, and a fresh capture. Anything else preserves the
  prior generation — never truncated-and-superseding, never an empty host.
- **Aggregate-only telemetry** (`SSMHostInventoryAggregates.LogFields`):
  counts plus the outcome only. No package values, hostnames, IPs, URLs, or
  rows; assessment reasons are closed static strings.

### Fixture-intent proof map (#9556)

| Case | Fixture intent | Contract verdict (proved here) |
| --- | --- | --- |
| Positive application | Dated complete `AWS:Application` row | Decodes; round-trips; 27-page coherent capture assesses complete and may supersede |
| Positive platform | Inventory platform observation with capture time | Decodes; round-trips |
| Positive platform (DII) | `DescribeInstanceInformation` observation, no capture time | Decodes with provenance; no invented capture time |
| Negative application | Row without `capture_time` | Dead-letters `input_invalid` naming `capture_time` |
| Negative platform | Observation without `observation_time` | Dead-letters `input_invalid` naming `observation_time` |
| Ambiguous undated | Zero-row response with no capture time | Unknown: emits no fact, cannot supersede |
| Ambiguous partial | Page-1-only read; token repeat/chain break; split capture times; mixed types | Partial: preserves the prior generation |
| Ambiguous ceiling breach | 101st page or 5001st entry | Partial, never superseding |
| Ambiguous stale | Complete capture past the stale-age bound | Stale display; prior generation stands |

Reducer admission, graph nodes/edges, and API/MCP output for these fixtures
belong to the sibling #9295 reducer/read leaves: this contract fixes the
expected verdicts they must preserve (admit the positives, dead-letter the
negatives, hold the prior generation on every ambiguous case).

## Fixtures

`testdata/fixtures` contains golden result fixtures for complete, unchanged,
partial, retryable, terminal, duplicate, conflict, tombstone, and redaction
cases. The package tests validate those fixtures against the public contract and
assert the generated JSON Schema stays in lockstep with the checked-in artifact.

No-Observability-Change: this SDK module has no runtime, network, queue, graph,
or telemetry emission path. Host runtime telemetry is owned by later core
adapter work.
