# collector-oci-registry

## Purpose

`cmd/collector-oci-registry` wires the OCI registry collector binary. It reads
configured OCI Distribution-compatible registries, maps provider endpoint/auth
settings onto the shared client/runtime layer, and commits digest-addressed
facts through the shared ingestion store.

## Ownership boundary

The command owns runtime config loading, claim-aware mode selection,
telemetry/bootstrap wiring, Postgres ingestion wiring, provider target wiring,
and hosted service startup.

It does not normalize OCI evidence, implement provider clients, write graph
truth, or answer queries. Those stay in `internal/collector/ociregistry`,
provider subpackages, projector/reducer code, and `internal/query`.

## Exported surface

This is a `main` package. Use `go doc -cmd ./cmd/collector-oci-registry` for
the package contract. Maintainer-facing entry points are `main`, runtime config
helpers, and service construction helpers used by command tests.

## Dependencies

- `internal/collector/ociregistry` supplies fact contracts.
- OCI registry provider/runtime packages supply registry access.
- `internal/collector` supplies `Service` and `ClaimedService`.
- Postgres ingestion and workflow stores are wired by the binary.
- `internal/telemetry` supplies bootstrap, logger, tracer, meter, and
  instruments.

## Telemetry

The command initializes the `collector-oci-registry` service identity and
passes telemetry providers into the collector runtime. Provider/runtime code
owns collection, request, status, warning, and fact-count signals.

## Gotchas / invariants

- When `ESHU_COLLECTOR_INSTANCES_JSON` is present, the command must select a
  claim-enabled `oci_registry` instance and use workflow claim fencing.
- Digest identity is canonical; mutable tags remain weak observations.
- Provider config must not leak credentials into logs, facts, metric labels, or
  status rows.
- Do not add direct graph writes here. Projection and reducer paths own graph
  materialization.

## Focused tests

```bash
go test ./cmd/collector-oci-registry -count=1
go test ./internal/collector/ociregistry/... -count=1
go doc -cmd ./cmd/collector-oci-registry
```

## Related docs

- `go/internal/collector/ociregistry/README.md`
- `docs/public/reference/collector-reducer-readiness.md`
- `docs/public/reference/telemetry/index.md`
- `docs/public/deployment/service-runtimes.md`
