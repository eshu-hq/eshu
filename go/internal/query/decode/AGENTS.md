# Agent instructions: decode

Read `doc.go` and `README.md` first. This package is one type, one
constructor, the shared default schema version and a `*string` deref; nearly
any change here is a contract change.

## Invariants

- `Error()` and `Unwrap()` MUST stay exported under those names: the `error`
  interface and `errors.Is`/`errors.As` find them by name.
- `DefaultSchemaMajorVersion` and `DerefString` have one copy, here. Do not
  re-fork either into a handler family; the per-family copies they replaced
  existed only because those families could not import root.
- `Unwrap` MUST return the underlying `*factschema.DecodeError`. Callers reach
  its `ErrUnsupportedSchemaMajor` sentinel via `errors.Is`/`errors.As`; wrapping
  or dropping it compiles fine and stops the sentinel matching.
- `New` MUST classify an unrecognised error as `input_invalid` rather than
  returning nil. A nil here reads to the caller as a successful decode, which
  turns a malformed fact into a row of empty strings in a user-facing response.

## Common changes

Adding a field: add it to `Error`, populate it in `New`, and check whether any
caller formats the error into an operator-facing log, since `Error()` is what
they see.

## Failure modes

- A caller builds `Error{FactKind: ..., FactID: ...}` from the exported fields
  and never sets the wrapped error. `Error()` and `Unwrap()` are nil-guarded for
  exactly this, because the type's fields are exported. A panic
  here would surface as a 500 on a read path whose job is to degrade one bad
  fact gracefully.
- `Unwrap` returning a typed nil rather than an untyped one. A nil
  `*factschema.DecodeError` inside an `error` interface is not nil, so
  `errors.Is` keeps walking and callers testing `Unwrap() != nil` get the wrong
  answer. The guard returns a bare nil.
- A caller treats a returned `nil` from a decode helper as success. `New` never
  returns nil; if it ever did, a malformed fact would read as a decoded one and
  reach the response as a row of empty strings.

## Anti-patterns

- Do not unexport `Error()` or `Unwrap()`; the type stops being an `error`
  and the wrapped sentinel stops matching.
- Do not add decoding logic here. This package holds the failure shape. The
  `factschema` Decode* seam does the decoding and the query layer decides what
  to do with a drop.
- Do not add telemetry here. Whether a decode drop is logged, and at what level,
  is the calling read path's decision.

## Changes needing ADR review

None specific to this package. It carries no wire contract, no persisted shape,
and no runtime behaviour of its own. A change to the `Classification` values it
records, however, follows the fact-kind contract rules in
`.agents/skills/eshu-contract-rigor`, because those values travel with the
payload contract rather than belonging to this package.

## Verification

From `go/`: `go test ./internal/query/... -count=1`. The decode paths that
exercise this live in the root package's `factschema_decode_*_test.go` files.
