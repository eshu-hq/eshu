# collector-oci-registry Agent Guidance

## Read First

1. `README.md` and `doc.go` for command scope.
2. `config.go` and `config_test.go` for target JSON, claim-aware mode, and
   credential env indirection.
3. `service.go` for provider client factories, Postgres, and service wiring.
4. `go/internal/collector/ociregistry/README.md` for fact identity and
   redaction contracts.
5. `go/internal/collector/ociregistry/ociruntime/README.md` for scan,
   warning, claim, and telemetry behavior.

## Local Rules

- Keep provider credentials as runtime-only config. Do not log them, place them
  in metrics, write them into facts, status, docs, or PR text.
- Emit facts only through `collector.Service` or `collector.ClaimedService` and
  `postgres.NewIngestionStore`.
- Keep command code to SDK/client wiring. Fact identity belongs in
  `ociregistry`; scan behavior belongs in `ociruntime`.
- Keep `/healthz`, `/readyz`, `/metrics`, and `/admin/status` hosted through
  `app.NewHostedWithStatusServer`.
- Keep registry host, repository, tag, digest, URL, and credential values out
  of metric labels.
- Treat missing Referrers API support as warning evidence, not proof that no
  artifacts exist.

## Change Rules

- Add target fields in `config.go`, test them in `config_test.go`, thread them
  into `ociruntime.TargetConfig`, and update package docs.
- Add providers by keeping auth and endpoint logic in provider subpackages that
  return an `ociruntime.RegistryClient`.
- Add or change metrics through `go/internal/telemetry` plus the telemetry
  reference docs and focused runtime tests.
