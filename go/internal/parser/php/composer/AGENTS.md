# AGENTS.md - internal/parser/php/composer guidance

## Read first

1. `README.md` - surface, rules, and known limits
2. `doc.go` - godoc contract
3. `manifest.go` - producer rule and the shared `ManifestName`
4. `lock.go` - consumer rule and the longest-prefix lookup
5. `docs/internal/evidence/7601-php-package-keys.md` - measurements and rulings

## Invariants

- This package is a leaf: it must not import `internal/parser/php`,
  `internal/parser`, or anything under `internal/storage`.
- `ManifestName` is the single name rule for both the parser's `package_id`
  stamp and the Postgres producer anchor
  (`internal/storage/postgres/code/producers`). Change it only together with
  the parity test there.
- Every rule fails closed to `""`. Never fall back to a shorter prefix, an
  outer manifest, or `autoload-dev` to produce a key: an unproven key can mint
  a false cross-repository edge.
- `FixtureDirPattern` is the single fixture-directory rule. The producer anchor
  (`internal/storage/postgres/code/producers`) embeds it in its SQL. Match whole
  segments only: `packages/testing` must stay a producer.
- Keep composer names disjoint from npm names: `ValidPackageName` must keep
  rejecting a leading `@` and requiring exactly one `/`.

## Verification

From `go/`:

```bash
go test ./internal/parser/php/... -count=1
go test ./internal/storage/postgres/code/producers/... -count=1
```
