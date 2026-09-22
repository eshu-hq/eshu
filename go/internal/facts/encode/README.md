# Encode

## Purpose

`encode` holds the payload-encoding substrate shared by the facts root and
its nested fact families: `StableID`, the deterministic fact identifier
function, and the `IntPtr`/`StringPtr`/`BoolPtr`/`StringValue`/
`StringPtrFromMap`/`IntPtrFromMap`/`JSONShapeMap`
helpers encoders use to build optional-field payloads and normalize them
for storage comparison.

It is a new package (not a simple move) created specifically to break an
import cycle: `go/internal/facts` imports its nested families (its
`schemaVersionFamilies` table references `cloud` through `compat_cloud.go`;
its semantic and documentation encoders reference the `docs` package), so a
nested family cannot import the facts root back to reach a shared helper.
`encode` depends on neither, so both sides import it.

## Ownership boundary

Owns the stable-ID algorithm and the payload pointer/decode helpers. Does
not own fact-kind vocabulary, schema versions, or any encoder's field
mapping — those stay in the facts root (`semantic_encode.go`) and each
nested family (`docs/encode.go`).

## Exported surface

- `StableID(factType, identity)` — deterministic SHA-256 hex id from a
  fact type and a normalized identity map; moved verbatim from the facts
  root's former `stableid.go`
- `IntPtr`, `StringPtr`, `BoolPtr` — zero-value-to-nil pointer helpers for
  optional payload fields
- `StringValue`, `StringPtrFromMap`, `IntPtrFromMap` — permissive decode
  helpers that read a `map[string]any` payload back
- `JSONShapeMap` — normalizes an encoder's payload map into the shape a
  JSON round-trip produces, so an in-process payload and a payload read
  back from storage compare equal

There is deliberately no exported two-value `(payload, err)` form. Each
owning package keeps its own four-line unexported `jsonShapePayload` adapter
(`docs/encode.go` and the facts root's `semantic_encode.go`) so the error an
encoder returns is returned by same-package code and keeps its original
text — routing it back out through this package would make every encoder
return an unwrapped error from an external package, which `wrapcheck`
rejects, and wrapping it would change the message every existing caller
sees.

See `doc.go` for the full godoc contract.

## Dependencies

Go standard library only (`crypto/sha256`, `encoding/hex`,
`encoding/json`, `fmt`, `reflect`, `time`). No internal package imports —
this is deliberate; see Purpose above.

## Depended on by

Four files, and no others (`rg -l '"github.com/eshu-hq/eshu/go/internal/facts/encode"'`):

- `go/internal/facts/stableid.go` — `facts.StableID` forwards to
  `encode.StableID`.
- `go/internal/facts/docs/documentation.go` — each `*StableID` derivation
  calls `encode.StableID`.
- `go/internal/facts/docs/encode.go` — calls `encode.StringPtr`,
  `encode.BoolPtr`, `encode.StringValue`, `encode.StringPtrFromMap`,
  `encode.IntPtrFromMap` and `encode.JSONShapeMap` to build its payloads.
- `go/internal/facts/semantic_encode.go` — calls `encode.StringPtr`,
  `encode.IntPtr` and `encode.JSONShapeMap` in place of the unexported
  `stringPtr`/`intPtr` helpers it used before the move.

## Telemetry

None. Encoding and hashing run inline with the caller's request and carry
no instrumentation of their own.

## Gotchas / invariants

- `StableID` panics if `json.Marshal` fails on the identity map. Callers
  must not pass identity maps containing non-serializable values — this
  behavior is unchanged from the pre-move `facts.StableID`.
- `StableID`'s normalization (UTC RFC3339Nano for `time.Time`, sorted map
  keys via `json.Marshal`) is a deduplication contract. Changing it
  changes which facts are considered "same as before" across every caller
  that reaches it through `facts.StableID`, not just direct callers of
  this package.
- `IntPtr`/`StringPtr`/`BoolPtr` treat the zero value as absence. A real
  zero, empty string, or false claim an encoder needs to represent as
  "observed and false/zero" (not "unobserved") must not go through these
  helpers.
- `IntPtrFromMap` rejects a non-integral `float64` (returns `nil`) rather
  than truncating it, so a fractional value read back from JSON never
  silently becomes a wrong line or page number.

## Related docs

- `docs/public/reference/fact-schema-versioning.md`
