# Collector Entrypoints Agent Notes

Read these before changing this package:

- `AGENTS.md`
- `docs/internal/agent-guide.md`
- `docs/public/guides/collector-authoring.md`
- `docs/public/deployment/service-runtimes-collectors.md`

Keep the manifest as the source of truth for generated collector command
boilerplate. Do not move provider target decoding, credential handling, or
source fact construction into the generator without a reviewed design.

Common changes:

- Add a collector entry to `collector_entrypoints.yaml`.
- Update templates when shared hosted collector startup behavior changes.
- Regenerate with `scripts/generate-collector-entrypoints.sh`.
- Verify with `scripts/verify-collector-entrypoints-generated.sh`.

Failure modes:

- A stale generated file means the manifest and command package are no longer
  byte-for-byte aligned.
- A provider-specific target schema change belongs in that command package's
  `source_config.go`, not in the shared generator.

Do not record secret values, private target IDs, or customer-specific material
in manifests, generated files, tests, docs, or failure output.
