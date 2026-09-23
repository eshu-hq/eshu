# AGENTS.md — internal/storage/postgres/migrations guidance

## Read first

1. `README.md` in this directory -- why the embed moved here.
2. `embed.go` -- `Definition`, `BootstrapDefinitions`, `Checksum`.
3. `embed_invariant_test.go` -- the golden digest this leaf must not drift.
4. `../schema_bootstrap_lock.go` -- the tracker that keys applied migrations
   by `path + variant + checksum_sha256`.

## Invariants

- Never add, remove, rename, or edit a `.sql` file as part of a refactor of
  this package. `Name`, `Path`, `SQL`, and definition order must stay
  byte-identical to what `embed_invariant_test.go` pins.
- Keep the embed pattern `*.sql` only. Do not let `embed.go`, `doc.go`,
  `README.md`, or `AGENTS.md` become a definition.
- `BootstrapDefinitionsWithoutContentSearchIndexes` stays in root's
  `schema.go`: it needs the content store's deferred DDL, which this leaf
  does not own.
- `postgres.Definition` and `postgres.BootstrapDefinitions` in root are a
  type alias and thin forwarder to this package. Do not duplicate the
  embed or the ordering logic in both places.

## Verification

```bash
cd go && go test ./internal/storage/postgres/migrations -count=1
cd go && go test ./internal/storage/postgres/... -count=1
```
