# Kotlin Parser

## Purpose

This package owns Kotlin source extraction for the parser engine. It turns one
Kotlin file into parser payload buckets for declarations, imports, variables,
function calls, receiver type inference, smart casts, parser-backed dead-code
roots, and package-bounded function return lookups.

## Ownership boundary

The package owns Kotlin parsing only. Parent engine dispatch, repository path
resolution, registry lookup, and runtime selection stay in go/internal/parser.
The child package must stay independent of the parent package and use shared
parser helpers for common payload and source behavior.

## Exported surface

See doc.go for the godoc contract.

- `Parse` reads one Kotlin file and returns the payload consumed by the
  collector path. The entry point starts in parser.go:12.
- `PreScan` returns function, class, and interface names through the same
  extraction path used by `Parse`. The entry point starts in prescan.go:6.

## Dependencies

The package imports go/internal/parser/shared for `shared.Options`, source
reading, base payload construction, bucket appends, sorting, and name
deduplication. Standard-library dependencies cover regular expressions,
filesystem walking through bounded directories, path normalization, and string
processing.

## Telemetry

This package emits no metrics, spans, or structured logs. Parser runtime
telemetry is owned by the collector and runtime layers that call the parser.

## Gotchas / invariants

`Parse` must preserve the parent payload keys and keep deterministic bucket
ordering before returning. `kotlinFunctionDeadCodeRootKinds` lives in
dead_code_roots.go:68 and only emits bounded parser-backed roots for Kotlin
entrypoints, interfaces, overrides, Gradle, Spring, lifecycle, and JUnit
callbacks. `kotlinInferReceiverType` lives in
receiver_inference.go:5 with method-return helpers because receiver inference
depends on local variables, class properties, sibling function returns, and type
parameter resolution. `kotlinCollectSiblingFunctionReturnTypes` is bounded by
the repository root and nearby Kotlin directories so return-type inference does
not scan the whole workspace. `scopedContext` in scope.go:5 tracks only the
brace-scoped context needed by this regex parser.

## Related docs

- docs/docs/architecture.md
- docs/docs/reference/local-testing.md
