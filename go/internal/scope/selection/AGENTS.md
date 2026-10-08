# AGENTS.md — go/internal/scope/selection

Scope: `go/internal/scope/selection` only. The parent instructions in
`go/internal/scope/AGENTS.md` still apply.

## Read first

1. `go/internal/scope/selection/README.md`: the rules and the aggregate table.
2. `go/internal/scope/selection/doc.go`: the package contract.
3. `go/internal/collector/repo/git/membership/observation.go`: the writer side,
   which aliases `State` and delegates `Confirmed` here.

## Invariants

- This package is the only definition of liveness, confirmation, and the
  scope-level aggregate. Do not copy these predicates into the collector or
  the status package. The storage live-row filter and the upsert's state math
  are the only SQL mirrors, and live Postgres parity tests pin them to `Live`
  and `project`; change both sides together.
- Keep the dependency set to the standard library. `internal/status`,
  `internal/storage/postgres`, and the git collector import this package; an
  import back into any of them is a cycle or a layering break.
- `unknown` means "no evidence". Callers must leave their decision unchanged
  for it and for `excluded_still_ingested`, never treat either as selected or
  not selected.
- The state values match the `repository_selection_observations` CHECK
  constraint (migration 166). Adding a state needs a migration, the writer,
  and the aggregate rules changed together.
- Evidence only: nothing here may delete, hide, or retire a scope.

## Verification

```bash
cd go && go test ./internal/scope/selection ./internal/collector/repo/git/membership ./internal/status -count=1
```
