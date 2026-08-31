# Agent instructions: querydecode

Read `doc.go` and `README.md` first. This package is one type and one
constructor; nearly any change here is a contract change.

## Invariants

- `Error()` and `Unwrap()` MUST stay exported. Root aliases this type, and a
  type alias cannot reach unexported methods across a package boundary. Making
  either unexported silently forces a 73-site rename in package `query`.
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

## Verification

From `go/`: `go test ./internal/query/... -count=1`. The decode paths that
exercise this live in the root package's `factschema_decode_*_test.go` files.
