---
name: golang-engineering
description: Apply Eshu-specific Go contracts, regression coverage, and verification when changing Go code or tests.
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

## Local verification and docs

Use `gofumpt` on changed Go files; it includes `gofmt` formatting. Run focused
proof, then broader checks when callers or shared contracts are affected.
Required lint, race, documentation, and promotion gates still apply regardless
of how small the focused test is. Use
[local testing](../../../docs/public/reference/local-testing.md) as the gate
source of truth; the coordinator owns the late `make pre-pr` promotion run.

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
