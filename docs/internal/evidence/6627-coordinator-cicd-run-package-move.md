# #6627 coordinator CI/CD run package move

## Scope

Base: `43c0606ca6690433421172bc684651f0f735cd4b`

This slice moves the CI/CD run planning package from the historical flat path
`internal/coordinator/cicdrun` to `internal/coordinator/cicd/run`. The package
name changes to `run`; `PlanRequest`, `WorkPlanner`, and `PlanCICDRunWork`
remain exact. The three callers use the explicit import alias `cicdrun`, so
their existing qualified names remain unchanged.

The new `internal/coordinator/cicd` package is a documentation-only namespace.
It adds no runtime declaration, import, or initialization. The `run` leaf owns
request validation, target parsing, requested-scope composition, and
deterministic workflow-row construction. It is not an independently deployable
service. Coordinator root still owns scheduling order and clocks, active and
claims gates, durable open-target admission, retries, queues, leases, and
telemetry.

## Inventory and dependency edges

At the base, the leaf contained exactly five files: `AGENTS.md`, `README.md`,
`doc.go`, `planner.go`, and `planner_test.go`. The destination contains those
same five file names. The new namespace parent contains exactly three direct
files: `AGENTS.md`, `README.md`, and declaration-only `doc.go`.

Coordinator root contained 49 direct non-test Go files before the move and
still contains 49. Its dirgate count and digest remain `49` and
`e39400aa04976497d8e4f169e8b8de10e3e1b16d40a5f9b9f31ff7f7da64377e`.
Adding the `cicd` namespace makes `cicd_run_service.go` collide with the new
child basename. Its package clause has a narrow file-local dirgate rationale:
the file retains CI/CD scheduling and durable admission on root `Service`
methods. The dirgate source ledger and generated Go map do not change.

The destination leaf's internal production imports remain limited to
`internal/coordinator/planner/contract`, `internal/facts`, `internal/scope`,
and `internal/workflow`. It imports neither coordinator root nor a sibling,
storage, transport, provider client, or telemetry implementation.

The three reverse importers are unchanged after substituting the package path:

- `cmd/workflow-coordinator/main.go`
- `internal/coordinator/cicd_run_service.go`
- `internal/coordinator/service_cicd_run_test.go`

The command still wires `WorkPlanner`; root still names `PlanRequest` in its
structural planner interface and scheduling call; the root test still captures
the request. No forwarding package or duplicate planner remains.

## TDD evidence

The genuine red phase happened before production moved. Only
`planner_test.go` was moved to the destination and changed to `package run`.
With the pinned `go1.26.6` toolchain:

```text
go test ./internal/coordinator/cicd/run -count=1
internal/coordinator/cicd/run/planner_test.go:31:22: undefined: WorkPlanner
internal/coordinator/cicd/run/planner_test.go:31:66: undefined: PlanRequest
FAIL github.com/eshu-hq/eshu/go/internal/coordinator/cicd/run [build failed]
exit_code=1
```

Production then moved without a forwarding package. The same destination test
passes after the move.

## Preserved contracts

- Request validation, collector-kind and active/claims requirements, target
  validation, duplicate-scope rejection, and exact error behavior are
  unchanged.
- Configured work-item order, sorted requested-scope metadata, run and work-item
  identities, trigger selection, fairness keys, and timestamps are unchanged.
- Credential environment names remain absent from durable requested-scope
  metadata, and the planner still makes no provider call.
- Normalized diffs of base `planner.go` and `planner_test.go` against their
  destinations, substituting only `package cicdrun` with `package run`, are
  empty.
- No wire, API, fact, reducer, query, graph, storage, queue, lease,
  concurrency, or runtime behavior changes.

## Verification

All Go commands ran under `GOTOOLCHAIN=go1.26.6`, `CC=clang`,
`CGO_CFLAGS=-std=gnu17`, an isolated worktree-local `GOCACHE`, and a short
`GOTMPDIR` outside every worktree:

- `go test ./internal/coordinator/cicd/run ./internal/coordinator ./cmd/workflow-coordinator -count=1`: exit 0.
- The same three-package command with `-race`: exit 0.
- `go build ./...`: exit 0.
- `go vet ./...`: exit 0.
- Repository lint selected the four affected packages and reported zero
  issues, exit 0, with its cached Go 1.26.6 binary and package loader matched
  through `GOTOOLCHAIN=go1.26.6`.
- `go doc ./internal/coordinator/cicd` and
  `go doc ./internal/coordinator/cicd/run`: exit 0.

The destination leaf has five direct files and the namespace parent has three;
both contain `AGENTS.md`, `README.md`, and `doc.go`. The dirgate, telemetry
coverage, Markdown line-cap, moved-reference scan, strict documentation build,
and `git diff --check` checks pass. The move-specific reference scan finds no
live import or exact-file pointer to the old path.

The base and working tree have the same B-7 cassette tree
`16f9f804dd5f303ea600ad986c75f278317c264b` and B-12 snapshot blob
`e28983e5ad44eb23de8751ed97dfc7d6eb99e472`; the B-12 SHA-256 remains
`894b8e51905461c8f328a52e5b1c789d89a54c7c24bb14b14785d62ae357fe63`.
The working-tree diff against `internal/query`, `internal/reducer`, and both
golden surfaces is empty.

No-Performance-Change: this is a package path/name move plus documentation and
a file-local lint ownership comment. The normalized production source is
unchanged. No executable algorithm, I/O, allocation, lock, query, queue, or
concurrency path changes, so there is no performance theory or benchmark
claim.

No-Observability-Change: the planner emitted no signal before the move and
emits none after it. Root scheduling and durable admission remain covered by
`eshu_dp_workflow_coordinator_reconcile_total`,
`eshu_dp_workflow_coordinator_reconcile_duration_seconds`, workflow rows and
claim status, and existing duplicate/admission logs. No metric, label, span,
log field, status surface, or telemetry ownership changes.
