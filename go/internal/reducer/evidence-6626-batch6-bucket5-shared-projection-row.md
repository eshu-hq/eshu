# Evidence: retire reducer.SharedProjectionIntentRow (#6626 batch 6, bucket 5)

## What changed

The last coordinated alias slice (bucket 3 explicitly excluded it):
`reducer.SharedProjectionIntentRow` (`= sharedintent.Row`, declared in the
shared-projection stanza of `go/internal/reducer/compat_correlation.go`) is
deleted, and every caller now spells the canonical `sharedintent.Row`:

- 139 external caller files: `reducer.SharedProjectionIntentRow` repointed,
  adding the `sharedintent` import where missing and dropping the now-unused
  reducer-root import in 9 files.
- In-package bare uses across the reducer root (including two
  `package reducer_test` files that qualified the alias) repointed the same
  way; the sibling `sqlrelationship.SharedProjectionIntentRow` alias and its
  5 files are a different symbol and untouched (not `reducer.X`, out of scope).
- Forwarder signatures repointed (`BuildSharedProjectionIntent`,
  `CarriesNoEdge` now take/return `sharedintent.Row`); a stanza retirement
  note records the bucket. Guard test
  `TestBucket5SharedProjectionIntentRowRetired` in
  `compat_alias_retirement_bucket5_test.go` scans the whole reducer package
  directory for any re-declared alias (bucket 3/4 pattern).
- Docs: the two storage READMEs that listed the alias as a reducer-root
  dependency now name `internal/reducer/sharedintent`; the sharedintent
  package doc no longer claims the root keeps a Row alias.
- `scripts/lib/replay_tier_inventory.json`: sha256 re-pinned for the 8
  enrolled `offlinetier` retract tests this rename touches. Each diff is
  spelling-only (alias to identical canonical type); the enrolled `tests`
  declaration lists were asserted unchanged before re-pinning, per the
  `b39d7241bd` precedent for a repointed retract test.

223 `.go` files (222 repointed + the new guard test), 2 READMEs, and the
inventory re-pin — 227 paths total with this note. No golden, spec, or
manifest content changed besides the inventory pins.

## Why no regression

No-Regression Evidence: the alias and the canonical type are the identical
type, so every repoint is spelling-only; no signature, envelope, row, or
ordering change.
Proof on this head: `go build ./...` exit 0; `go vet ./...` exit 0; gofmt
clean; `go test -count=1` green over every touched package tree
(`internal/reducer/...`, `internal/storage/...`,
`internal/replay/costcounting/...`, `internal/replay/offlinetier/...`,
`internal/ifa/materializededges/...`, `internal/query/codequery/...`,
`internal/collector/repo/git/...`, `cmd/reducer/...`), plus `-race` on
`internal/reducer/`, `internal/reducer/sharedintent/`, and
`internal/storage/cypher/edge/writer/`; guard RED (seeded re-declaration
fails) / GREEN (clean tree passes, with the bucket 3/4 guards);
`scripts/test-verify-golden-corpus-gate.sh` pass (B-7 green, B-12 snapshot
byte-identical — no testdata changes); `ci-gates validate --drift` pass;
`test-verify-telemetry-coverage.sh` 49/49; `git diff --check` clean.
`rg "reducer\.SharedProjectionIntentRow"` returns zero hits in Go and
Markdown sources.

Local toolchain note: the repo pins go 1.26.9 while the host mise `go` is
1.27.2, whose build-cache export data the pinned toolchain cannot read, so
package-loading tests fail under the host driver. All Go proof above ran
with the 1.26.6 binary (auto-switches to cached 1.26.9), `CC=clang`, and
`GOROOT`/`GOBIN` unset.

## Observability

Observability Evidence: no new metric instrument, metric label, span,
structured log field, queue domain, worker, lease, or runtime knob. The
shared-projection writer/reader paths keep reporting through the existing
spans and counters; only the static type spelling at call sites changed.
