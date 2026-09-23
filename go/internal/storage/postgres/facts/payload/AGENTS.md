# AGENTS.md — Postgres fact payload JSON codec guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../../AGENTS.md` for Postgres storage conventions.
3. `json.go` for the codec and helpers; `json_test.go` for their contract.

## Invariants

- Keep `MarshalPayload`, `UnmarshalPayload`, `EmptyToNil`, and
  `EmptyToDefault` byte-identical in behavior when moving or refactoring
  code; this is a move-only package, not a place to change JSON or SQL
  semantics.
- Preserve the literal-backslash-vs-null-escape distinction in
  `stripUnescapedJSONNulls`: it counts preceding backslashes to tell a
  genuine `\u0000` escape from source text that merely spells the six
  characters `\u0000`. Getting this wrong silently corrupts or drops fact
  content.
- Keep the package clause as `package payloadstore`; callers import the
  `storage/postgres/facts/payload` path without an alias.
- Never import the parent `postgres` package or any sibling
  `storage/postgres` family package from here — this package is a leaf so
  every family can depend on it without a cycle.

## Common changes

- A new caller family should import this package directly rather than
  reintroducing a local copy of the codec or the empty-string helpers.
- Changing the JSONB sanitization rules (e.g., a new Postgres JSONB
  rejection case) needs its own issue with a regression test in
  `json_test.go` before the implementation change, per `eshu-postgres-rigor`.

## Failure modes

- Importing the parent `postgres` package (or another family package that
  imports it) creates an import cycle.
- Treating a decoded empty JSON object (`{}`) as a non-nil empty map
  instead of nil breaks callers that branch on "no payload".
- Losing the backslash-parity check in `stripUnescapedJSONNulls` corrupts
  JSON or drops legitimate source text that contains the literal string
  `\u0000`.

## Verification

From `go/`, run:

```bash
go test ./internal/storage/postgres/... -count=1
go vet ./internal/storage/postgres/...
```

Run `scripts/verify-package-docs.sh` from the repository root.
