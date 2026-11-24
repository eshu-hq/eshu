# AGENTS.md - internal/ifa/impact/digest guidance

## Read first

1. `README.md` - purpose and the two readers of the digest.
2. `digest.go` - `DigestSQL`, `Cassettes`, `Read`.

## Invariants

- One statement, two readers. Do not copy `DigestSQL` into a test or a script: import it, or call the
  `ifa impact-digest` verb.
- The digest must exclude every column that names the delivering run (`intent_id` and `cause` in a support
  payload, `materialized_at` on a winner). Adding such a column to the digest makes equal runs hash
  differently.
- `Read` refuses empty tables. Do not relax it: an empty family would pass by comparing empty to empty.
- Keep the package driver-free; callers own the connection.

## Verification

`cd go && go test ./internal/ifa/impact/digest/...`
