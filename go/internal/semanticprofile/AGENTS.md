# AGENTS.md — internal/semanticprofile guidance

This package parses semantic extraction provider profile configuration. It must
never load provider credentials or call provider endpoints.

## Invariants

- Treat credential values as out of scope. Configuration may carry only handles
  such as secret names, Vault paths, workload identity markers, local profile
  names, or environment variable names.
- Keep status projections redacted. Do not expose credential handles through
  `status`, API, MCP, OpenAPI examples, logs, or docs.
- Fail closed on unknown provider kinds, credential source kinds, source
  classes, or malformed environment variable handles.
- Hosted provider traffic belongs behind policy and prompt-safety gates; this
  package only describes configured profiles.

## Verification

Run `cd go && go test ./internal/semanticprofile -count=1` after changes. Run
the package documentation gates when adding or changing this package's docs.
