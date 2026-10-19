# Evidence: retire reducer.Intent (#6626 batch 6, bucket 6)

## What changed

`reducer.Intent` (`= reducercontract.Intent`, the only content of its root
file) is deleted with that file, and every caller now spells the canonical
`reducercontract.Intent`:

- 124 external caller files: `reducer.Intent` repointed, adding the aliased
  `reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"`
  import where missing (the 458-file convention; the package itself is named
  `contract`) and dropping 5 now-unused reducer-root imports.
- Six `package reducer_test` files and the `gpphase/phasestate.go` comments
  repointed the same way.
- In-package bare `Intent` uses across the reducer root repointed, including
  4 files whose `package reducer //nolint` suffix initially hid them from the
  file census (`code_import_repo_edge_handler.go`,
  `correlated_workload_projection_input_loader_test.go`,
  `defaults_test.go`, `workload_materialization_handler_test.go`).
- Guard test `TestBucket6IntentRetired` in
  `compat_alias_retirement_bucket6_test.go` scans the whole reducer package
  directory for any re-declared alias (bucket 3/4/5 pattern).
- The sibling `sqlrelationship.Intent` alias and its files are a different
  symbol, not `reducer.X`, and stay out of scope.
- No inventory re-pin: no enrolled replay-tier source carries the retired
  spelling (verified by the golden-corpus mirror passing unmodified).

257 `.go` files (256 repointed + the new guard test, minus the deleted
`intent_value.go`), plus this note. No golden, spec, manifest, README, or
inventory content changed.

Deliberate remainder: `rg "reducer\.Intent"` still hits two historical
evidence records (`docs/internal/evidence/5837-aws-drift-reopen.md`,
`docs/internal/evidence/reducer-ack-duplicate-claim.md`), which describe past
code in prose. Rewriting them would falsify the record, and the
evidence-continuity gate treats them as ledger, so they stay with this
disclosure. Every Go source is clean.

## Why no regression

No-Regression Evidence: the alias and the canonical type are the identical
type, so every repoint is spelling-only; no signature, envelope, row, or
ordering change.
Proof on this head: `go build ./...` exit 0; `go vet ./...` exit 0; gofmt
clean; `go test -count=1` green over every touched package tree
(`internal/reducer/...`, `internal/storage/...`,
`internal/replay/crashreplay/...`, `internal/replay/faultreplay/...`,
`internal/replay/schedulereplay/...`, `internal/ifa/materializededges/...`,
`internal/query/admin/...`, `internal/collector/repo/git/...`,
`cmd/reducer/...`) across three consecutive full runs (174 packages ok; one
earlier run exited 1 with its failure lines lost to output truncation and no
recurrence across the three saved runs); plus `-race` on
`internal/reducer/`, `internal/reducer/contract/`, and
`internal/storage/cypher/edge/writer/`; guard RED (seeded re-declaration
fails) / GREEN (clean tree passes, with the bucket 3/4/5 guards);
`scripts/test-verify-golden-corpus-gate.sh` pass (B-7 green, B-12 snapshot
byte-identical — no testdata changes); `ci-gates validate --drift` pass;
`test-verify-telemetry-coverage.sh` 49/49; `git diff --check` clean.
`rg "reducer\.Intent"` over Go sources returns zero hits.

Local toolchain note: the repo pins go 1.26.9 while the host mise `go` is
1.27.2, whose build-cache export data the pinned toolchain cannot read, so
package-loading tests fail under the host driver. All Go proof above ran
with the 1.26.6 binary (auto-switches to cached 1.26.9), `CC=clang`, and
`GOROOT`/`GOBIN` unset with go1.26.6 first on `PATH` (the 1.27.2 driver also
trips the go1.26-built lint/gosec binaries).

## Observability

Observability Evidence: no new metric instrument, metric label, span,
structured log field, queue domain, worker, lease, or runtime knob. Intent
plumbing keeps reporting through the existing spans and counters; only the
static type spelling at call sites changed.
