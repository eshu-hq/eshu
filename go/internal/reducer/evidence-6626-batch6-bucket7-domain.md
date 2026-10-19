# Evidence: retire reducer.Domain (#6626 batch 6, bucket 7)

## What changed

`reducer.Domain` (`= reducercontract.Domain` in `go/internal/reducer/intent.go`,
which keeps its error/const forwarders) is deleted, and every caller now
spells the canonical `reducercontract.Domain`:

- 82 external caller files: `reducer.Domain` repointed, adding the aliased
  `reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"`
  import where missing (the 458-file convention; the package itself is named
  `contract`) and dropping 5 now-unused reducer-root imports.
- Two in-tree files (`package value_test` fence-domains test with 4 code
  uses; one `package reducer` comment) repointed the same way.
- In-package bare `Domain` uses across the reducer root repointed, with a
  collision-aware rule: `Domain:` struct-literal field keys (the field named
  Domain) and `.Domain` selectors are never touched; line-start
  `Domain <type>` field declarations keep their name with a repointed type.
- Two mis-rewritten struct field names (`repo_dependency_acceptance_gate.go`,
  `runtime.go`) were caught by gofmt/build and restored; one boundary-table
  entry turned tautological and is deleted up front (bucket 6 P2 lesson);
  three `"Domain = %q` test log labels reverted.
- Guard test `TestBucket7DomainRetired` in
  `compat_alias_retirement_bucket7_test.go` scans the whole reducer package
  directory for any re-declared alias (bucket 3/4/5/6 pattern).
- Six living docs updated (projector, schedulereplay AGENTS + README,
  cypher + postgres + truth READMEs), all line-neutral.
- The sibling `sqlrelationship.Domain` alias and its files are a different
  symbol, not `reducer.X`, and stay out of scope.
- No inventory re-pin: no enrolled replay-tier source carries the retired
  spelling (verified by the golden-corpus mirror passing unmodified).

110 `.go` files (109 repointed + the new guard test), 6 living docs, plus
this note. No golden, spec, manifest, or inventory content changed.

Deliberate remainder: `rg "reducer\.Domain"` still hits two historical
evidence records (`docs/internal/evidence/5426-golden-corpus-coverage.md`,
`go/internal/replay/schedulereplay/evidence-4367-projection-ordering.md`),
which describe past code in prose. Rewriting them would falsify the record,
and the evidence-continuity gate treats them as ledger, so they stay with
this disclosure. Every Go source is clean.

## Why no regression

No-Regression Evidence: the alias and the canonical type are the identical
type, so every repoint is spelling-only; no signature, envelope, row, or
ordering change.
Proof on this head: `go build ./...` exit 0; `go vet ./...` exit 0; gofmt
and gofumpt clean; `go test -count=1` green over every touched package tree;
plus `-race` on the reducer root, contract, and cypher edge writer packages;
guard RED (seeded re-declaration fails) / GREEN (clean tree passes, with the
bucket 3/4/5/6 guards); `scripts/test-verify-golden-corpus-gate.sh` pass
(B-7 green, B-12 snapshot byte-identical — no testdata changes);
`ci-gates validate --drift` pass; `test-verify-telemetry-coverage.sh` 49/49;
`git diff --check` clean. `rg "reducer\.Domain"` over Go sources returns
zero hits.

Local toolchain note: the repo pins go 1.26.9 while the host mise `go` is
1.27.2, whose build-cache export data the pinned toolchain cannot read, so
package-loading tests fail under the host driver. All Go proof above ran
with the 1.26.6 binary (auto-switches to cached 1.26.9), `CC=clang`, and
`GOROOT`/`GOBIN` unset with go1.26.6 first on `PATH` (the 1.27.2 driver also
trips the go1.26-built lint/gosec binaries).

## Observability

Observability Evidence: no new metric instrument, metric label, span,
structured log field, queue domain, worker, lease, or runtime knob. Domain
plumbing keeps reporting through the existing spans and counters; only the
static type spelling at call sites changed.
