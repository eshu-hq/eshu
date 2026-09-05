# Verification and Linting

Use this file when deciding what to run, how broad to verify, and how to treat lint findings.

## Eshu entrypoints

Use `docs/public/reference/local-testing.md` (repository-relative) for required
gates. Inspect the relevant script or CI entry when its scope or behavior is
unclear; do not load an unrelated repository tool inventory. Run Go commands
from `go/`. Prefer `scripts/dev/precommit-go.sh lint` for ordinary lint checks;
bare `golangci-lint` needs the local custom plugins built first.

## Targeted vs Full Verification

Start narrow, then expand:

1. Run the smallest relevant test scope first.
   Example: `go test ./internal/cache -run TestStoreGet`
2. Run the affected package.
   Example: `go test ./internal/cache`
3. Run broader package coverage when the change crosses package boundaries.
   Example: `go test ./...`
4. Run repo-level verification entrypoints when they exist and are relevant.

Use targeted verification when:
- the change is local to one package
- the contract is proven by a focused unit or integration test
- fast feedback matters during red-green-refactor

Use broader verification when:
- exported APIs changed
- shared packages or cross-package contracts changed
- build tags, generated code, or workspace wiring changed
- the change affects startup, config loading, logging, persistence, or transport boundaries

## When `go test -race` Is Warranted

Run `go test -race` when the change touches:
- goroutines or worker pools
- channels, mutexes, atomics, or condition variables
- shared mutable maps or slices
- cancellation, deadlines, or context propagation
- background cleanup, retry loops, or event fan-out
- flaky tests or behavior that smells like a data race

Do not skip race detection just because the change looks small. If the behavior is concurrent, race detection is part of the proof when practical.

## Formatting

- Run `gofmt` on changed files unless the repo uses a stricter or wrapped formatter.
- Use `goimports` only if the repo already uses it in CI, editor config, or documented workflows.
- Do not hand-format code to match personal preference.

## Treating `golangci-lint` Findings Pragmatically

- If the repo configures `golangci-lint`, running it is mandatory before claiming completion unless the user explicitly limits verification.
- Fix the underlying readability, correctness, or maintainability problem when reasonable.
- Follow the repo's configured linter set. Do not optimize for a generic global profile that the repo does not use.
- If a finding exposes a noisy or low-value rule, check whether the repo already has a convention for handling it before changing code style broadly.
- Prefer a local code improvement over a suppression.

## Strong Rule on Disabling Linters

- Do not disable a linter globally to make one change pass.
- Do not add blanket `nolint` directives.
- If suppression is truly necessary, scope it as tightly as possible and explain why in a comment.
- The justification must be concrete, not "lint is wrong" or "needed for tests."

Prefer this:

```go
//nolint:errcheck // Best-effort cleanup; caller already has the authoritative write result.
_ = file.Close()
```

Over this:

```go
//nolint
_ = file.Close()
```

## Reporting Verification

Before finishing, report:
- what commands ran
- what scope they covered
- what was not run
- any remaining risk or unverified paths
