# AWS Scanner Bindings Aggregator

## Purpose

`internal/collector/cloud/aws/runtime/bindings` imports every AWS service
bind package so the runtime registry is populated by init side
effects. The collector-aws-cloud command and the runtime tests blank-import
this package to obtain the full production scanner set.

## Ownership boundary

This package owns one thing: the canonical list of blank imports that pull
every AWS service `bind` into a binary. It does not own service
selection logic, configuration validation, or any runtime behavior. Each
binding's behavior lives in its own service `bind` package.

## Exported surface

None. The package is imported only for its init side effects. See `doc.go`
for the godoc rendering of that contract.

## Dependencies

One blank import per AWS service bind package. Adding a new scanner
appends one line. Removing a scanner removes one line. No file in runtime
or any other consumer needs to change.

## Telemetry

None of its own. Each registered scanner and its SDK adapter emits the
per-service counters and spans documented in the runtime README.

## Gotchas / invariants

- Keep the blank-import list alphabetical so reviewers can scan it.
- Do not add non-binding imports here. The package must stay a pure
  side-effect aggregator.
- This file is marked `merge=union` in `.gitattributes` so parallel scanner
  PRs can land without textual conflicts on this list.

## Related docs

- `../README.md` for the registry and runtime surface.
- `docs/public/services/collector-aws-cloud-scanners.md` for the user-facing
  coverage table.
- `docs/public/guides/collector-authoring.md` for the new-scanner workflow.
