# Fixture Pack Agent Rules

This directory is the versioned, importable payload-conformance fixture pack for
the `github.com/eshu-hq/eshu/sdk/go/factschema` module (Contract System v1
§3.5). It mirrors the parent module's independence constraints.

## Required Checks

- Read the root `AGENTS.md`, `docs/internal/agent-guide.md`, and the parent
  `sdk/go/factschema/AGENTS.md` before edits.
- Keep this package importable with no third-party dependencies reachable from
  it: use only `embed` and `encoding/json`. Do NOT import
  `github.com/eshu-hq/eshu/go/internal/...` or the schema generator
  (`internal/schemagen`).
- The embedded `schema/*.json` files MUST stay byte-identical to the canonical
  generated artifacts under `sdk/go/factschema/schema/*.json`. When a schema is
  regenerated (`go generate ./...`), refresh the embedded copy
  (`cp schema/<kind>.v1.schema.json fixturepack/schema/<kind>.v1.schema.json`).
  The drift-lock test `TestFixturePackSchemasMatchCanonical` fails the build on
  any divergence.
- Every fact kind with a schema MUST ship both a `payloads/<kind>.valid.json`
  and a `payloads/<kind>.invalid.json`. The invalid payload MUST omit exactly one
  schema-required field so it dead-letters through the typed decode seam.
  `TestFixturePackPayloadsDecodeThroughSeam` enforces both.
- Update `README.md` and `doc.go` when the accessor surface or versioning story
  changes.
- Run `go test ./... -count=1` from `sdk/go/factschema`, `gofmt` on changed Go
  files, and `git diff --check` from the repo root.

## Contract Rules

- The pack version IS the `sdk/go/factschema` module version — one git tag, one
  lockstep release. Do not invent a separate fixture-pack version number.
- Fixtures prove payload SHAPE only. The pack never claims graph truth, hosted
  activation, or production safety.
- The fixtures are keyed by the core fact-kind wire string. An out-of-tree
  collector maps its own namespaced kind to the shipped schema shape; the pack
  does not, and must not, emit bare core kinds as if it were a collector.
