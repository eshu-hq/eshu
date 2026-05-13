# AGENTS.md - cmd/collector-oci-registry guidance for LLM assistants

## Read First

1. `go/cmd/collector-oci-registry/README.md` - binary purpose, config,
   telemetry, and invariants
2. `go/cmd/collector-oci-registry/config.go` - target JSON and credential env
   indirection
3. `go/cmd/collector-oci-registry/service.go` - provider client factory and
   `collector.Service` wiring
4. `go/internal/collector/ociregistry/` - fact identity and envelope contracts
5. `go/internal/collector/ociregistry/ociruntime/` - scan orchestration and
   runtime telemetry

## Invariants This Package Enforces

- Provider configs may reference credential env vars, but credentials must not
  be logged, placed in metrics, or written into facts.
- Facts must flow through `collector.Service` or `collector.ClaimedService` and
  `postgres.NewIngestionStore`.
- The command package owns SDK/client wiring only; fact identity belongs in
  `ociregistry`, and scan behavior belongs in `ociruntime`.
- Keep `/healthz`, `/readyz`, `/metrics`, and `/admin/status` wired through
  `app.NewHostedWithStatusServer`.

## Common Changes And How To Scope Them

- Add a target JSON field in `config.go`, add a `config_test.go` case, thread
  it into `ociruntime.TargetConfig`, and update `README.md`.
- Add a provider by keeping provider-specific auth or endpoint logic in a
  provider subpackage and returning an `ociruntime.RegistryClient`.
- Change metrics by updating `go/internal/telemetry`, the telemetry reference
  docs, and this README in the same PR.

## Anti-Patterns

- Writing facts directly from `main.go` or `service.go`.
- Adding registry host, repository, tag, or digest values as metric labels.
- Treating a missing Referrers API as evidence that no artifacts exist.
