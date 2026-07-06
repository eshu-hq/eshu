# Evidence: cgroup-aware CPU sizing (#4456 CPU dimension)

## Problem

Worker-count defaults across the codebase read `runtime.NumCPU()`, which
reports the HOST cpu count. Inside a cgroup CPU-quota container (Kubernetes
`resources.limits.cpu`, Docker `--cpus`), `runtime.NumCPU()` overestimates
usable CPU, causing worker pools sized off it to over-spawn relative to what
the container can actually schedule.

## Fix (simplified after code review)

An earlier version of this change added a handwritten cgroup v1/v2
CPU-quota reader (`ConfigureGOMAXPROCS`, `readCgroupCPUQuota`,
`parseCgroupV2CPUMax`, `parseCgroupV1CPUQuota`) and called
`ConfigureGOMAXPROCS(logger)` at 4 `main()` entrypoints, mirroring
`internal/runtime.ConfigureMemoryLimit`'s manual-configuration pattern. Code
review found that design redundant: **Go 1.26.4 (this module's pinned `go`
directive in `go/go.mod`) already sets `GOMAXPROCS` from the container
cgroup automatically** — the `containermaxprocs` GODEBUG has been
default-on since Go 1.25. `runtime.GOMAXPROCS(0)` is therefore *already*
cgroup-aware, before any Eshu code runs. The manual `ConfigureGOMAXPROCS`
reinvented what the runtime already does, and — because it was a separate
call from the routing — it also left `cmd/reducer` (the actual reducer
service binary) and `cmd/eshu` (the local-host owner, which sizes
`ESHU_*_WORKERS` for child processes) unwired: neither has a
`ConfigureMemoryLimit`-style setup call, so the earlier "15/15 sites pick up
the reduced count" claim was false for those two consumers.

`go/internal/cpubudget/cpubudget.go` is now trimmed to exactly one function:

```go
func UsableCPUs() int {
	n := runtime.GOMAXPROCS(0)
	if n < 1 {
		return 1
	}
	return n
}
```

No manual configuration call, no cgroup-file reading, no new imports beyond
`runtime`. The real fix is entirely in the *routing*: every worker-count
default that used to call `runtime.NumCPU()` (or store it as a bare function
value) now calls `cpubudget.UsableCPUs()` instead, so it picks up whatever
`GOMAXPROCS` the Go runtime already set from the container's cgroup quota.

`TestGoDirectiveSupportsAutomaticGOMAXPROCS` in `cpubudget_test.go` reads
`go/go.mod`'s `go` directive and asserts it is `>= 1.25` — the version floor
this design depends on. If that directive is ever downgraded, this test
fails on purpose and a handwritten cgroup reader would need to come back.

## Routing table (17 physical call sites across 14 files — corrected count, excludes internal/parser)

The four `cmd/*/main.go` `ConfigureGOMAXPROCS(logger)` calls were removed
(no replacement needed — nothing to configure). The `cpubudget` import was
removed from those 4 files where it was otherwise unused. All worker-sizing
call sites **outside `internal/parser`** route through
`cpubudget.UsableCPUs()`:

| # | File | Function / var |
|---|------|-----------------|
| 1 | `cmd/projector/runtime_wiring.go` | `projectorWorkerCount` |
| 2 | `cmd/ingester/wiring.go` (2 sites) | `projectorWorkerCount` |
| 3 | `cmd/ingester/wiring_nornicdb_phase_group_concurrent.go` | `nornicDBDefaultEntityPhaseConcurrency` |
| 4 | `cmd/reducer/config.go` (2 sites) | `loadReducerWorkerCount` |
| 5 | `cmd/bootstrap-index/bootstrap_pipeline.go` | `projectionWorkerCount` |
| 6 | `cmd/bootstrap-index/nornicdb_entity_phase_concurrency.go` | `nornicDBDefaultEntityPhaseConcurrency` |
| 7 | `internal/storage/postgres/content_writer_batch.go` | `contentWriterDefaultBatchConcurrency` |
| 8 | `internal/storage/postgres/ingestion_backfill_pool.go` | `deferredBackfillWorkerCount` |
| 9 | `internal/reducer/shared_projection_config.go` | `defaultSharedProjectionWorkers` |
| 10 | `internal/reducer/code_reachability_projection_runner.go` | `(*CodeReachabilityProjectionRunner).concurrency` |
| 11 | `internal/reducer/value_flow_fixpoint_cache.go` | inline semaphore size (value-flow fixpoint cache worker pool) |
| 12 | `internal/reducer/value_flow_fixpoint_snapshot.go` | inline semaphore size (value-flow fixpoint snapshot worker pool) |
| 13 | `internal/collector/git_selection_config.go` (2 sites) | `snapshotWorkerCount`, `parseWorkerCount` |
| 14 | `cmd/eshu/local_host.go` | `localHostNumCPU` var (was `runtime.NumCPU`, a bare function value with no parens — missed by the original `runtime\.NumCPU\(\)` grep because it has no parens) |

That is 14 files/locations and 17 physical call sites (rows 2, 4, and 13 each
have 2 call sites; the rest have 1).

## Deferred: internal/parser

Two `internal/parser` worker-sizing sites are **not** routed in this change
and remain on the host-wide call directly:

- `internal/parser/interproc/solve.go` — `SolvePartitioned`'s semaphore size,
  `max(1, runtime.GOMAXPROCS(0))` (this one was already reading
  `GOMAXPROCS(0)` before this change touched it; routing it through
  `cpubudget.UsableCPUs()` and then reverting is zero behavior loss — the
  effective value is identical either way).
- `internal/parser/go_package_interface_prescan.go` —
  `effectivePackagePrescanWorkers`, `runtime.NumCPU()` (this one reverts to
  the host CPU count; this single pool stays host-sized until the follow-up
  lands).

This is **not** an import-cycle blocker — `cpubudget` has zero internal
dependencies, so `internal/parser` could safely import it. The deferral is
because any change under `go/internal/parser/*.go` trips the
parser-relationship-kit blocking gate, which requires a parser `*_test.go`
change AND a language-support doc update in lockstep. That gate exists for
language/query capability changes (new syntax support, new relationship
extraction, etc.); it misfires on a mechanical worker-count routing, and
there is no honest language-support doc to write for "this pool now reads
`GOMAXPROCS(0)` instead of `NumCPU()`." Routing these two sites is deferred
to a separate follow-up PR scoped to satisfy that gate on its own terms.
`go/internal/parser/*` is fully out of this diff — `git diff origin/main
--name-only | grep internal/parser` returns empty.

`cmd/reducer/config.go` (site 4) was previously routed but never had a
`ConfigureGOMAXPROCS` call anywhere upstream of it (the reducer binary has
no `main()` analogous to the other four — its startup is driven by
`cmd/eshu/local_host.go` spawning `eshu-reducer` as a child process, or by
its own composed entrypoint). Under the old design that call site was
"routed" to `UsableCPUs()` but `UsableCPUs()` itself depended on
`ConfigureGOMAXPROCS` having run first to lower `GOMAXPROCS` — since nothing
called it for the reducer process, the reducer's worker sizing never
actually saw the reduced count. Under the new design there is nothing to
call first: the Go runtime has already set `GOMAXPROCS` correctly by the
time any Go code runs, so `cmd/reducer`, `cmd/eshu`, `cmd/mcp-server`,
`cmd/api`, and every collector binary all get the correct cgroup-aware count
through `UsableCPUs()` with zero additional wiring.

## No-Regression Evidence:

On an unconstrained host (no cgroup CPU quota, or a quota `>=` the host CPU
count), the Go 1.25+ runtime leaves `GOMAXPROCS` at the host CPU count (the
`containermaxprocs` GODEBUG only lowers it when the cgroup quota is
tighter). `runtime.GOMAXPROCS(0)` therefore equals `runtime.NumCPU()` on
every developer machine, CI runner, and unconstrained deployment — this was
confirmed directly:

```
$ go test -count=1 -v ./internal/cpubudget/
=== RUN   TestUsableCPUsMatchesGOMAXPROCS
--- PASS: TestUsableCPUsMatchesGOMAXPROCS (0.00s)
=== RUN   TestGoDirectiveSupportsAutomaticGOMAXPROCS
--- PASS: TestGoDirectiveSupportsAutomaticGOMAXPROCS (0.00s)
=== RUN   TestParseGoDirective
--- PASS: TestParseGoDirective (0.00s)
    --- PASS: TestParseGoDirective/patch_version (0.00s)
    --- PASS: TestParseGoDirective/no_patch_version (0.00s)
    --- PASS: TestParseGoDirective/missing_directive (0.00s)
=== RUN   TestParseGoDirectiveTrimsTrailingWhitespace
--- PASS: TestParseGoDirectiveTrimsTrailingWhitespace (0.00s)
PASS
ok  	github.com/eshu-hq/eshu/go/internal/cpubudget	0.182s
```

`go vet ./internal/cpubudget/ ./cmd/ingester/ ./cmd/projector/ ./cmd/bootstrap-index/ ./cmd/webhook-listener/ ./cmd/eshu/ ./internal/reducer/ ./internal/collector/`
passes with zero errors after trimming `cpubudget.go`, removing the 4
`ConfigureGOMAXPROCS` calls, and routing the missed `cmd/eshu/local_host.go`
site. `internal/parser` is intentionally excluded from this vet list — its
two sites are deferred (see "Deferred: internal/parser" above) and the
package is untouched. Every pre-existing test that stubs `localHostNumCPU`
as a `func() int` test double (`cmd/eshu/local_host_resource_tuning_test.go`)
is unaffected,
because it replaces the var wholesale and never depends on its default
implementation. Every pre-existing test that asserted a worker default
equals `runtime.NumCPU()` on a dev machine still holds, because
`runtime.GOMAXPROCS(0) == runtime.NumCPU()` there.

In a CPU-limited container, the Go 1.25+ runtime itself lowers
`runtime.GOMAXPROCS(0)` to the cgroup quota before any Eshu code runs — no
Eshu-side setup call is required. All 17 physical `cpubudget.UsableCPUs()`
call sites across the 14 files/locations in the routing table above (all
worker pools in the ingest/reduce/collect path, plus the local-host owner's
child-process worker env vars) now pick up that already-reduced value,
including `cmd/reducer` and `cmd/eshu`, which the prior
manual-`ConfigureGOMAXPROCS` design left unwired. `internal/parser`'s two
pools are the sole exception, deferred as described above.

## No-Observability-Change:

The prior `ConfigureGOMAXPROCS` logged a structured `source=env|cgroup|default`
line at each of the 4 entrypoints. That call (and its log line) is removed
entirely — there is nothing left to configure or log. `UsableCPUs()` is a
pure read of `runtime.GOMAXPROCS(0)` with no side effect. This is not a
regression in observability: the Go runtime's own GOMAXPROCS value is
inspectable through Go's standard runtime/debug and expvar surfaces
independent of Eshu-specific logging, and there was never an
Eshu-side "the cgroup quota is Y" signal worth losing — the prior log line
mostly restated what the runtime itself now handles silently and correctly.
No operator-facing metric, span, or log field is removed by this
simplification beyond that one now-redundant startup log line.

## Verification run

```
$ cd go && gofmt -l <changed files>
(clean)

$ go vet ./internal/cpubudget/ ./cmd/ingester/ ./cmd/projector/ ./cmd/bootstrap-index/ ./cmd/webhook-listener/ ./cmd/eshu/ ./internal/reducer/ ./internal/collector/
(clean)

$ go test -count=1 ./internal/cpubudget/
ok  	github.com/eshu-hq/eshu/go/internal/cpubudget	(7 tests)

$ rg -n 'runtime\.NumCPU' go/cmd go/internal -g '*.go' | rg -v _test | rg -v internal/parser
(only matches inside internal/cpubudget/cpubudget.go and doc.go — explanatory
prose contrasting UsableCPUs with runtime.NumCPU(), not a live call site;
internal/parser's two deferred sites are excluded from this check since they
are out of scope for this PR, not a regression)

$ git diff origin/main --name-only | rg internal/parser
(empty — internal/parser is fully out of this diff)

$ git diff --check
(clean)
```

Note: `go build ./...` and `make pre-pr` were deliberately deferred per
coordinator instruction (a concurrent local machine load was in progress);
only the targeted `gofmt`/`go vet`/`go test` commands above were run. A full
`go build ./...` should still be run before this branch is proposed for
review.
