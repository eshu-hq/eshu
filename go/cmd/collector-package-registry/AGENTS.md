# collector-package-registry Agent Guidance

## Read First

1. `README.md` and `doc.go` for command scope.
2. `config.go` and `config_test.go` for collector-instance selection, target
   parsing, credential env indirection, and claim-mode validation.
3. `service.go` for `collector.ClaimedService`, Postgres, workflow store, and
   provider wiring.
4. `main.go` for hosted runtime setup.
5. `go/internal/collector/packageregistry/packageruntime/README.md` for live
   metadata fetch and claim invariants.

## Local Rules

- Keep this command claim-aware only. It must select one enabled,
  claim-capable `package_registry` collector instance and commit through
  `collector.ClaimedService`.
- Resolve credential env vars at runtime only. Never copy resolved values into
  desired config, workflow rows, facts, logs, status, metric labels, docs, or
  PR text.
- Keep targets explicit and bounded by provider, ecosystem, registry, scope,
  package limit, and version limit. Do not infer full-registry crawling from a
  provider name.
- Keep provider-specific HTTP/auth behavior in `packageruntime`; this command
  maps process config into runtime config.
- Do not add graph writes, ownership inference, or direct fact writes in
  command wiring.

## Change Rules

- Add target JSON fields in `config.go`, validate them through workflow config
  validation, and map them into `packageruntime.TargetConfig`.
- Add command wiring tests whenever selection, credential resolution, claim
  validation, limits, or document-format mapping changes.
- Keep heartbeat interval below lease TTL when changing claim timing.
