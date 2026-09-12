# #6627 coordinator SBOM-attestation package move

## Scope

Base: `9ff3bfad2091d3a14fd8989b0668087d2ae655c0`

This slice moves the hosted SBOM and attestation planning package from the
historical flat path `internal/coordinator/sbomattestation` to
`internal/coordinator/sbom/attestation`. The package name becomes
`attestation`; `PlanRequest`, `WorkPlanner`, and `PlanSBOMAttestationWork`
remain exact. All three consumers use the shorter package qualifier.

The new `internal/coordinator/sbom` package is a documentation-only namespace.
It adds no runtime declaration, import, or initialization. The `attestation`
leaf owns request validation, target parsing, requested-scope composition, and
deterministic workflow-row construction. It is not independently deployable or
extractable: repository-local workflow, fact, scope, and plan-key contracts
remain dependencies. Coordinator root still owns scheduling order and clocks,
active and claims gates, durable open-target admission, retries, queues,
leases, and telemetry.

## Inventory and dependency edges

At the base, the leaf contained exactly six files: `AGENTS.md`, `README.md`,
`doc.go`, `planner.go`, `planner_contract_test.go`, and `planner_test.go`. The
destination contains those same six file names. The new namespace parent
contains exactly three direct files: `AGENTS.md`, `README.md`, and
declaration-only `doc.go`.

Coordinator root contained 49 direct non-test Go files before the move and
still contains 49. Its dirgate count and digest remain `49` and
`e39400aa04976497d8e4f169e8b8de10e3e1b16d40a5f9b9f31ff7f7da64377e`.
Adding the `sbom` namespace makes `sbom_attestation_service.go` collide with
the new child basename. Its package clause has a narrow file-local dirgate
rationale because the file retains SBOM scheduling and durable admission on
root `Service` methods. The dirgate source ledger and generated Go map do not
change.

The destination leaf's internal production imports remain limited to
`internal/coordinator/planner/contract`, `internal/facts`, `internal/scope`,
and `internal/workflow`. It imports neither coordinator root nor a sibling,
storage, transport, provider client, graph backend, or telemetry
implementation. NornicDB v1.3.1 is therefore outside this package-move proof
surface.

The three reverse importers are unchanged after substituting the package path:

- `cmd/workflow-coordinator/main.go`
- `internal/coordinator/sbom_attestation_service.go`
- `internal/coordinator/service_sbom_attestation_test.go`

The command still wires `WorkPlanner`; root still names `PlanRequest` in its
structural planner interface and scheduling call; the root test still captures
the request. No forwarding package or duplicate planner remains.

## TDD evidence

The genuine red phase happened before production moved. Only the two test
files moved to the destination and changed to `package attestation`. With the
pinned Go 1.26.6 toolchain:

```text
go test ./internal/coordinator/sbom/attestation -count=1
internal/coordinator/sbom/attestation/planner_contract_test.go:32:21: undefined: WorkPlanner
internal/coordinator/sbom/attestation/planner_contract_test.go:32:81: undefined: PlanRequest
internal/coordinator/sbom/attestation/planner_test.go:30:22: undefined: WorkPlanner
internal/coordinator/sbom/attestation/planner_test.go:30:74: undefined: PlanRequest
FAIL github.com/eshu-hq/eshu/go/internal/coordinator/sbom/attestation [build failed]
exit=1
```

Production then moved without a forwarding package. The destination tests pass
after the move.

## Preserved contracts

- Collector-instance, collector-kind, enabled, claims-enabled, observation
  time, plan-key, configuration, and duplicate-scope validation are unchanged.
- Configured work-item order, sorted requested-scope metadata, run and
  work-item identities, trigger selection, fairness keys, and timestamps are
  unchanged.
- Durable requested-scope metadata still omits document URLs and credentials,
  and the planner still performs no artifact or provider I/O.
- Normalized diffs of base `planner.go`, `planner_test.go`, and
  `planner_contract_test.go` against their destinations, substituting only the
  package declaration, are empty.
- No wire, API, fact, reducer, query, graph, storage, queue, lease,
  concurrency, or runtime behavior changes.

## Verification

All Go commands ran under `GOTOOLCHAIN=go1.26.6`, `CC=clang`,
`CGO_CFLAGS=-std=gnu17`, an isolated worktree-local `GOCACHE`, and a short
`GOTMPDIR` outside every worktree:

- `go test ./internal/coordinator/sbom/attestation ./internal/coordinator ./cmd/workflow-coordinator -count=1`: exit 0.
- The same three-package command with `-race`: exit 0.
- `go build ./...`: exit 0.
- `go vet ./...`: exit 0.
- Repository lint selected the four affected packages and reported zero
  issues, exit 0.
- `go doc ./internal/coordinator/sbom` and
  `go doc ./internal/coordinator/sbom/attestation`: exit 0.

The destination leaf has six direct files and the namespace parent has three;
both contain `AGENTS.md`, `README.md`, and `doc.go`. The changed-path dirgate,
telemetry coverage, Markdown line-cap, strict documentation build, normalized
source comparison, old-import reference scan, and `git diff --check` checks
pass.

The base and working tree have the same B-7 cassette tree
`16f9f804dd5f303ea600ad986c75f278317c264b` and B-12 snapshot blob
`e28983e5ad44eb23de8751ed97dfc7d6eb99e472`; the B-12 SHA-256 remains
`894b8e51905461c8f328a52e5b1c789d89a54c7c24bb14b14785d62ae357fe63`.
The working-tree diff against `internal/query`, `internal/reducer`, and both
golden surfaces is empty.

No-Performance-Change: this is a package path and name move plus documentation
and a file-local lint ownership comment. The normalized production source is
unchanged. No executable algorithm, I/O, allocation, lock, query, queue, or
concurrency path changes, so there is no performance theory or benchmark
claim.

No-Observability-Change: the planner emitted no signal before the move and
emits none after it. Root scheduling and durable admission remain covered by
`eshu_dp_workflow_coordinator_reconcile_total`,
`eshu_dp_workflow_coordinator_reconcile_duration_seconds`, workflow rows and
claim status, and existing duplicate/admission logs. No metric, label, span,
log field, status surface, or telemetry ownership changes.
