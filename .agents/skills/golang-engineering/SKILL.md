---
name: golang-engineering
description: Use when writing or changing Go code or tests under go/: package and file naming, TDD, exported contracts, lint, the 40-file directory cap, compat-surface family moves, and build-tag traps.
---

# Go engineering in Eshu

Use owning-package guidance to resolve ownership and behavior before changing
its contract. Read the relevant entrypoint or dispatcher when the change crosses
runtime boundaries; a small edit does not require an unrelated package tour.

## Behavioral proof

Follow the root TDD policy. Bug fixes need a regression that fails for the
intended reason before the implementation changes. Tests must exercise the
production path, not a copied implementation or a stand-in for the behavior
being asserted. Cover every affected dispatch variant of shared helpers,
query builders, replay paths, and retry classifiers. Confirm filtered tests
actually ran. Use deliberate mutation of the production assertion when test
sensitivity is uncertain; do not require a separate mutation exercise for every
test whose regression failure already demonstrates that sensitivity.

Keep ownership boundaries intact. For workers, queues, transactions, retries,
or shared-state changes, use `concurrency-deadlock-rigor` for the relevant proof.
Runtime and performance work retains the repo's accuracy, performance, and
concurrency evidence requirements.

## Restructuring and naming

Apply [naming.md](../../../docs/internal/naming.md) to every package,
directory, and file a change touches, not only the ones being moved: plain
English at each path level, never repeat the directory name in the file name,
nest compound names into directories instead of gluing them, no package-name
stutter in exported identifiers, and leave every touched name better than you
found it. The same reasoning extends to a name that shadows a standard-library
package (`sort`, `context`, `errors`, ...) — nest or rename instead of
colliding with it.

`golangci-lint`'s `dirgate` plugin caps non-test `.go` files per directory at
40 (issue #6054). Directories already over the cap are pinned in
`scripts/lib/dirgate-grandfather.tsv` by file count and content digest; every
PR touching a pinned directory must shrink the count or re-derive both — see
`generator-script-discipline` for regenerating
`tools/golangci-lint-dirgate/grandfather.go` from the ledger, and
[reducer-target-tree.md, "Restack rule (the dirgate ledger trap)"](../../../docs/internal/design/reducer-target-tree.md)
for the merge-conflict trap when two moves re-pin the same row.

When a family moves to its own package, keep root callers compiling through a
compat surface: type aliases and thin forwarders grouped by family under a
`compat_*.go` file. A family move adds a stanza to an existing compat file; it
never creates a new one (see `go/internal/reducer/compat_correlation.go` and
its sibling `compat_*.go` files). Delete each aliased entry once its last
caller has moved.

`go build/vet/test ./...` silently skips any file behind a `//go:build` tag
that no workflow, Makefile target, or script ever passes — a moved or renamed
build-tagged file can look green on the default checks while it never compiles
or runs anywhere (see the incident recorded in
`go/internal/query/cloud_inventory_account_alias_live_test.go`). Before
trusting a default `go test ./...` on tagged code, grep the tag's
build-constraint name across `.github/workflows`, `Makefile`, and `scripts/`.

## Local verification and docs

Use `gofumpt` on changed Go files; it includes `gofmt` formatting. Run focused
proof, then broader checks when callers or shared contracts are affected. Use
[local testing](../../../docs/public/reference/local-testing.md) as the gate
source of truth.

`make pre-push` is the fast local floor before every push: changed-package
test, lint, build, and vet, the file-count cap, the registry-selected static
gates (slow ones deferred to CI with a printed reason), and
docs-contradiction. `make pre-pr` and `make pre-pr-full` are recommended, not
required, for higher-risk changes — queue, lease, or claim code; schema DDL;
hot-path Cypher or graph writes; reducer projection or materialization; and
package moves (use `pre-pr-full` for moves). CI's `required-gates-complete`
(plus `go-core-complete` and `go-race-complete`) is the blocking authority and
covers every Ifá/Odù, contract, performance, and end-to-end gate; when CI
fails, reproduce only that failing gate locally.

- Go code changes require the repo lint entrypoint unless the user narrows
  verification. See [verification](references/verification-and-linting.md) when
  selecting the scope or diagnosing lint setup.
- Changed packages under `go/internal` or `go/cmd` need `doc.go`, `README.md`,
  and `AGENTS.md`; run `scripts/verify-package-docs.sh`. Update their contents
  when contracts or contributor guidance change, using `eshu-folder-doc-keeper`.
- Run `scripts/verify-performance-evidence.sh` for Cypher, graph-write, runtime
  stage, worker, queue, lease, batching, or concurrency changes, including new
  collectors that introduce those patterns. Use the reviewed branch base.
- Report commands, actual results, and unverified scope.

When running Go tools on a shared host or with concurrent agents, read
[shared-machine constraints](references/shared-machine.md) before execution.
They cover isolated caches, process-local environment settings, and short
`GOTMPDIR` paths outside worktrees.

For a specific unresolved Go design, testing, or documentation question, consult
only the relevant reference: [design](references/go-best-practices.md),
[testing](references/tdd-workflow.md), or
[documentation](references/documentation-guidelines.md). Repository contracts
and conventions take precedence over generic examples.
