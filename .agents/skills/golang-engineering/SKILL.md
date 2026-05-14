---
name: golang-engineering
description: Use when changing, reviewing, testing, or documenting Go code in Eshu, including parser work, runtime services, storage adapters, query handlers, package APIs, test additions, and Go docs tied to code changes.
---

# Golang Engineering

Use this skill for Eshu Go work. Repository guidance in `AGENTS.md`, package
`AGENTS.md`, `README.md`, and `doc.go` wins over generic Go advice. Read the
reference files only when local repo guidance does not answer the question.

References:

- `references/go-best-practices.md` for default Go design, API, naming, error,
  and concurrency choices.
- `references/tdd-workflow.md` for red-green-refactor, regression tests,
  table-driven tests, and subtests.
- `references/documentation-guidelines.md` for package docs and doc comments.
- `references/verification-and-linting.md` for Go verification and linting.

## Mandatory Workflow

- MUST inspect the owning package docs and entrypoint/orchestrator before
  editing runtime, collector, reducer, storage, parser, query, or command code.
- MUST write or update the failing test before implementation for bug fixes and
  parser/runtime behavior changes.
- MUST run `gofmt` on changed Go files.
- MUST run focused Go tests before broader package tests.
- MUST run `golangci-lint` when Go code changed unless the user explicitly
  narrows verification.
- MUST run `scripts/verify-package-docs.sh` for Go package changes under
  `go/internal` or `go/cmd`; new packages need `doc.go`, `README.md`, and
  `AGENTS.md` before they land.
- MUST run `scripts/verify-performance-evidence.sh` when Go changes touch
  Cypher, graph writes, runtime stages, workers, queues, leases, batching, or
  concurrency. New collectors are covered when they introduce those patterns.
- MUST report exactly what passed and what was not run.

## Design Rules

- Prefer simple, readable Go over clever abstractions.
- Prefer concrete types until a real consumer needs an interface.
- Define interfaces where they are consumed.
- Keep exported APIs narrow and package boundaries cohesive.
- Prefer the standard library before adding dependencies.
- Preserve error context; wrap with `%w` when callers may inspect the cause.
- Keep concurrency explicit and justified. Use `concurrency-deadlock-rigor`
  when workers, queues, locks, transactions, retries, or shared state change.

## Documentation Rules

- Document every new or changed exported identifier whose behavior is part of a
  package contract.
- Document unexported helpers when they encode a contract, storage/query
  assumption, concurrency rule, retry rule, or regression purpose.
- Update package `README.md`, `doc.go`, and package `AGENTS.md` when code
  changes alter the package contract or contributor guidance.
- For a new package, create all three package docs before merging:
  `doc.go` for godoc, `README.md` for humans, and `AGENTS.md` for future AI
  edits. CI enforces this with `scripts/verify-package-docs.sh`.
- Prefer short comments that explain intent, invariant, or failure mode; do not
  add comments that only restate the identifier.

## Review Checklist

- Local conventions and package docs were read.
- The behavior has focused test coverage.
- The test failed for the intended reason before the fix when TDD applies.
- API surface, naming, errors, context use, and package boundaries stayed clear.
- Concurrency and storage changes have an explicit safety argument.
- Verification evidence is listed with any gaps or remaining risk.
