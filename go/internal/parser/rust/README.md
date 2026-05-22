# Rust Parser Adapter

## Purpose

This package owns Rust-specific tree-sitter payload extraction for functions,
types, modules, traits, impl blocks, imports, macro definitions and invocations,
calls, constants, statics, type aliases, root metadata, attributes, derives, and
generic parameter metadata, conditional derive evidence, nested field and enum
variant attributes, and structured where-clause evidence.

## Ownership boundary

The package receives a caller-owned tree-sitter parser from the parent parser
engine. It owns Rust syntax walking and payload assembly, while the parent
package keeps registry dispatch, runtime parser construction, and compatibility
method signatures.

## Exported surface

See `doc.go` and `go doc ./internal/parser/rust` for the godoc contract.
Callers use `Parse` for full payload extraction, `PreScan` for dependency symbol
discovery, and `ResolveModuleRowFileCandidates` with `ModuleResolution` for
filesystem candidate calculation from parser-emitted module rows.

## Dependencies

This package imports the shared parser helper package and tree-sitter types. It
must not import the parent parser package.

## Telemetry

This package emits no telemetry directly. Parser timing and runtime observability
remain owned by the parent engine.

## Gotchas / invariants

- Brace imports expand into one `imports` row per syntactic import while
  preserving raw `full_import_name`.
- `fn main` only becomes a root on Cargo-shaped entrypoint paths or direct
  runtime macro evidence. Library functions named `main` are not roots by name
  alone.
- Exact `pub` visibility marks public API items; scoped visibility such as
  `pub(crate)` does not.
- Trait implementation methods, including unsafe impl blocks, carry trait
  context so cleanup analysis does not delete runtime-dispatched surfaces by
  local inbound-edge shape alone.
- Module path candidates stay relative to the current file directory. Explicit
  `#[path = "..."]` replaces the candidate list; macro-origin rows stay blocked.
- `parseCargoCfgManifest` is bounded and ignores dynamic or unsupported TOML
  instead of guessing.
- The package preserves macro, cfg, derive, generic, and where-clause evidence
  without inferring arbitrary macro expansion, Cargo feature selection, cfg
  evaluation, workspace feature solving, or cross-crate semantic resolution.

## Focused tests

```bash
cd go
go test ./internal/parser/rust -count=1
go vet ./internal/parser/rust
go doc ./internal/parser/rust
```

## Related docs

- `docs/public/reference/mcp-reference.md`
