# AGENTS.md - internal/collector/tempo guidance

## Read first

1. `README.md`
2. `doc.go`
3. `http_client.go`
4. `envelope.go`
5. `source.go`
6. `docs/public/reference/observability-evidence.md`
7. `docs/public/reference/telemetry/metrics-ingestion-collectors.md`

## Invariants this package enforces

- Tempo collection is metadata-only. Do not fetch or persist spans, traces, raw
  trace IDs, request attributes, TraceQL bodies, or trace search payloads.
- Tag values are collected only for explicitly configured tag names. Accepted
  values are represented as counts and fingerprints, never raw strings.
- High-cardinality tag-value reads emit coverage warnings and do not persist
  value hashes.
- Tenant IDs are request headers only. Facts store tenant presence and a
  fingerprint, not the raw tenant.
- Collectors emit source facts only. Reducers own declared/applied/observed
  comparison and graph/read-model truth.

## Common changes and how to scope them

- Add a supported metadata endpoint by proving the official Tempo response shape
  cannot contain spans, trace IDs, request attributes, or query text.
- Add a new fact field by updating the envelope test first, then the public fact
  and observability docs.
- Add telemetry by updating `go/internal/telemetry` tests, contract constants,
  this README, and the public telemetry page in the same PR.

## Failure modes and how to debug

- `tempo provider failure: rate_limited` means Tempo returned HTTP 429 after
  bounded retries. Check `eshu_dp_tempo_rate_limited_total` and the workflow
  failure class.
- `permission_hidden` warnings mean credentials cannot read a metadata endpoint.
  They are evidence gaps, not absence of traces.
- `high_cardinality_rejected` warnings mean a configured tag produced more
  values than the target limit.

## Anti-patterns specific to this package

- Calling `/api/search`, `/api/traces/<id>`, `/api/v2/traces/<id>`,
  `/api/metrics/query`, or `/api/metrics/query_range` from this package.
- Adding raw provider URLs, tenant IDs, tag values, trace IDs, or query bodies to
  facts, logs, metric labels, or status strings.
- Treating observed Tempo tags as proof of service health, ownership, runtime
  coverage, or incident cause.

## What NOT to change without ADR review

- The metadata-only source boundary.
- The rule that live Tempo evidence is fallback, validation, drift, and
  freshness evidence after IaC-first declared/applied sources.
- Moving correlation or graph projection into collector code.
