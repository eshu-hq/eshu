# Observability Fact Payloads Agent Rules

This directory is part of the public
`github.com/eshu-hq/eshu/sdk/go/factschema` Go module. It holds the
schema-version-1 typed payload structs for the `observability` fact family
(eighteen kinds across a git-declared lane and a live observed/applied lane).
It must remain independent from Eshu internals.

## Required Checks

- Read the root `AGENTS.md`, the module `AGENTS.md`, and
  `docs/internal/agent-guide.md` before edits.
- Do not import `github.com/eshu-hq/eshu/go/internal/...`. Keep the module
  standalone.
- After changing any payload struct's fields, run `go generate ./...` from the
  module root and commit the regenerated schema under `../../schema/`, refresh
  the embedded fixture-pack copy under `../../fixturepack/schema/`, and update
  the fixture-pack payload examples under `../../fixturepack/payloads/`.
- Run `go test ./... -count=1` from the module root (`sdk/go/factschema`),
  `gofmt` on changed Go files, and `git diff --check` from the repo root.

## Contract Rules

- A field is required exactly when its json tag carries no `omitempty`; by the
  flat-struct convention required fields are also non-pointer, and optional
  fields are pointers carrying `omitempty`. The schema generator and the decode
  seam's required-field check derive that set reflectively from the struct's
  tags — do not hand-maintain a per-kind key list.
- `source_instance_id` is required on every kind (the one identity field every
  collector injects in both lanes). `provider_object_uid` is additionally
  required only on `ObservedDashboard`, `ObservedTarget`, `ObservedLogSignal`,
  and `ObservedTraceSignal` (their sole live emitter always writes it). Do NOT
  make `provider_object_uid` required on `ObservedRule` (the Grafana emitter
  uses `alert_rule_uid`) or on any declared kind (generic passthrough).
- Do not narrow, rename, or invent fields. This migration is output-preserving:
  the structs mirror the existing payload shape the reducer's coverage-metadata
  classifier reads. Every struct carries the full candidate-key union that
  classifier consults, so no key it reads is ever dropped.
- Every struct is FLAT (no embedded fields, no `Attributes` pass-through): the
  reducer reads only a bounded named-key union, which a closed struct covers,
  and the parent decoder ignores unknown top-level keys.
- The reducer decodes only the latest struct for each kind. Version shims for an
  older schema major live in the parent `factschema` package's decode seam
  (`decodeLatestMajor` in `decode.go`), never here or in reducer handler code.

## Fact-kind constant values

The fact-kind strings are DOTTED (`observability.declared_dashboard`). The
`FactKind*` constant VALUES in the parent `decode.go` MATCH the wire strings the
collectors emit (`go/internal/facts.Observability*FactKind`) byte-for-byte; the
reducer-side `TestFactSchemaKindsMatchWireFactKinds` drift lock asserts each
stays byte-equal to its `facts.*FactKind` counterpart.
