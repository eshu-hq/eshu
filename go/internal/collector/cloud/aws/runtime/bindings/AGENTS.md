# AGENTS.md - runtime/bindings guidance

## Read First

1. `README.md` - aggregator purpose and ownership boundary.
2. `bindings.go` - the canonical list of service bind imports.
3. `../README.md` - runtime registry and runtime surface.

## Invariants

- Keep `bindings.go` a pure list of blank imports of
  `internal/collector/cloud/aws/service/<svc>/bind` packages.
- Keep the list alphabetical so reviewers can verify completeness at a
  glance.
- Do not import anything else. Configuration, validation, and selection
  logic belong to the consumer (the collector-aws-cloud command).
- Preserve the `merge=union` attribute on `bindings.go` in `.gitattributes`
  so parallel scanner PRs do not conflict on this file.

## Common Changes

- Add a new scanner: append one blank import line. No other change here.
- Remove a scanner: delete the matching blank import line. Do not change
  the surrounding file.

## What Not To Change Without An ADR

- Do not add per-service init configuration here. The registry contract
  expects every bind to be self-sufficient.
- Do not change the package name; the command imports it by its canonical
  path.
