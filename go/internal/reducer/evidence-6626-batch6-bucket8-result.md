# Evidence: retire reducer.Result (#6626 batch 6, bucket 8)

## What changed

`reducer.Result` (`= reducercontract.Result` in
`go/internal/reducer/runtime.go`) is deleted, and every caller now spells
the canonical `reducercontract.Result`:

- 63 caller files outside the reducer tree: qualified `reducer.Result`
  repointed, adding the aliased
  `reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"`
  import where missing and dropping 6 now-unused reducer imports.
- 4 caller files inside the reducer tree with qualified uses (3 root ack
  tests, `status/summary/probes_test.go`) repointed the same way.
- 42 reducer-root files with bare in-package uses repointed, with a
  code-span-only rule: strings and `//`/`/* */` comments are masked, so
  prose such as `Result.SubSignals` / `Result.SubDurations` in comments and
  the `"deferred Result = ..."` test log label are untouched; `Result:`
  struct-literal field keys (none present) and `.Result` selectors are never
  touched; longer names (`envResult`, `skippedResult`,
  `refreshResultSignals`) are boundary-excluded.
- `runtime.go` by hand: alias plus its doc comment deleted, 7 own-signature
  bare uses repointed (the file already imported the contract package).
- One boundary-table entry (`{name: "Result", ...}` in
  `contract_boundary_test.go`) turned tautological and is deleted up front
  (bucket 6 P2 lesson).
- Guard test `TestBucket8ResultRetired` in the new
  `compat_alias_retirement_bucket8_test.go` scans the whole reducer package
  directory for any re-declared alias (bucket 3/4/5/6/7 pattern). Audit
  before writing it: no competing `Result` type, func, method, var, or
  `Result:` field key exists in the reducer root, and the subpackage-owned
  `Result` types (`code/value/cleanup`, `maintenance/*`,
  `workload/retract`) live in other packages and are untouched.
- No living-doc updates: no `AGENTS.md`/`README.md` carries the retired
  spelling. The sibling `sqlrelationship.Result` alias and its files are a
  different symbol, not `reducer.X`, and stay out of scope.
- No inventory re-pin: no enrolled replay-tier source carries the retired
  spelling (verified by the golden-corpus mirror passing unmodified).

111 `.go` files (521 insertions, 518 deletions), plus the new guard test,
plus this note. No golden, spec, manifest, or inventory content changed.

Deliberate remainder: `rg "reducer\.Result"` still hits one historical
evidence record
(`docs/internal/design/6785-value-flow-cloud-sink-refresh.md`), which
describes past code in prose. Rewriting it would falsify the record, and
the evidence-continuity gate treats it as ledger, so it stays with this
disclosure. Every Go source is clean.

## Why no regression

No-Regression Evidence: the alias and the canonical type are the identical
type, so every repoint is spelling-only; no signature, envelope, row, or
ordering change.
Proof on this head: `go build ./...` exit 0; `go vet ./...` exit 0; gofmt
clean over every touched file (one untouched pre-existing gofmt-dirty file,
`go/internal/collector/scannerworker/service.go`, left alone);
`go test -count=1` green over all 9 touched package trees; plus `-race` on
the reducer root and contract packages; guard RED (seeded re-declaration
fails, exit 1) / GREEN (clean tree passes, exit 0);
`scripts/test-verify-golden-corpus-gate.sh` exit 0 (6 PASS, 0 FAIL —
no testdata changes);
`ci-gates validate --drift` pass; `test-verify-telemetry-coverage.sh`
49/49; `git diff --check` clean. `rg "reducer\.Result"` over Go sources
returns zero hits.

Local toolchain note: the repo pins go 1.26.9 while the host mise `go` is
1.27.2, whose build-cache export data the pinned toolchain cannot read, so
package-loading tests fail under the host driver. All Go proof above ran
with the 1.26.6 binary (auto-switches to cached 1.26.9), `CC=clang`, and
`GOROOT`/`GOBIN` unset with go1.26.6 first on `PATH` (the 1.27.2 driver
also trips the go1.26-built lint/gosec binaries).

## Observability

Observability Evidence: no new metric instrument, metric label, span,
structured log field, queue domain, worker, lease, or runtime knob. Result
plumbing keeps reporting through the existing spans and counters; only the
static type spelling at call sites changed.
