# AGENTS.md — go/internal/cli/demo guidance for LLM assistants

## Read first

1. `go/internal/cli/demo/README.md` — purpose, ownership boundary, exported
   surface, invariants
2. `go/internal/cli/demo/doc.go` — the godoc contract, including the exact
   subprocess, HTTP, and file surface
3. `go/cmd/eshu/demo.go` — the cobra `RunE` wrapper. It shows how the two
   halves fit: `newResolvedDemoRuntime` is where `os.Getwd` and `os.Getenv`
   live.
4. `docker-compose.demo.yaml` and `docker-compose.demo.runtime.yaml` — the
   overlay this package drives. `serviceName` in teardown.go must match a
   service key in the runtime fragment.
5. `specs/demo-first-answers.v1.yaml` — the manifest `LoadManifest` parses.

## Invariants this package enforces

- **`Down` refuses a Compose project this command did not create.** The check
  is `ownsProject` plus `configFilesNameTheOverlay`: read the
  `com.docker.compose.project.config_files` label off every container in the
  project, split on commas, and require some entry's basename to equal
  `ComposeFileName`. An earlier version of this guard always returned true,
  which is worse than no guard — project membership alone does not prove the
  demo created the stack. `TestDemoDown_RefusesAProjectItDoesNotOwn` and
  `TestOwnsProjectRejectsALookalikeConfigPath` are the regression tests; do
  not relax either into a substring match.
- **No process wiring in this package.** No cobra flags, no `os.Getenv`, no
  reads of the real `os.Stdin`, no `os.Exit`. `APIBase`, `MCPBase`, and
  `ResolveComposeFile` take the lookup function as a parameter. `os.ReadFile`
  behind a path parameter (`LoadManifest`) is fine — that is acting on an
  explicit argument, the same shape as `internal/cli/servicereport`.
- **Readiness means the queue is empty**, not that the process is healthy.
  `IndexStatus.Complete` is the single definition; do not add a second one.
- **Every compose invocation carries `-p` and `-f`** via `composeArgs`.

## Common changes and how to scope them

- **Add a flag to `eshu demo`** → the flag registration and its
  `cmd.Flags().Get*` read go in `go/cmd/eshu/demo.go`; pass the value into
  `Options` or as a function parameter. Never read it here.
- **Change what readiness requires** → `IndexStatus.Complete` in endpoint.go,
  and update `TestDemoUp_WaitsForIndexCompletenessNotProcessHealth`.
- **Add a docker invocation** → route it through `r.exec` so the fake in
  `runtime_test.go` records it, and add it to the enumeration in `doc.go`.
  That list is meant to be exhaustive; a call added without updating it makes
  the doc wrong rather than incomplete.
- **Add a scored criterion** → add the name to criteria.go (typed
  `firstrunbench.CriterionName`; only demo-only names belong there), the
  evaluator to benchmark.go, and a row to `EvaluateBenchmark`'s append. A
  criterion that is `Required` and can be `firstrunbench.CriterionNotMeasured`
  will fail every unprobed run; follow `evaluateModeCriterion`, which clears
  `Required` when it downgrades.

## Failure modes and how to debug

- Symptom: `eshu demo down` refuses a stack the demo really did start → check
  the container labels with `docker ps --all --filter
  label=com.docker.compose.project=eshu-demo --format '{{.Label
  "com.docker.compose.project.config_files"}}'`. If `ESHU_DEMO_COMPOSE_FILE`
  pointed at an overlay whose basename is not `docker-compose.demo.yaml`, the
  refusal is correct and the fix is the operator's path, not this guard.
- Symptom: `eshu demo status` reports a healthy stack as not ready → the key
  recovery failed. `recoverKey` execs `printenv ESHU_API_KEY` in the service
  named by `serviceName`; if that service was renamed in the overlay the probe
  goes out unauthenticated, gets 401, and reads as not-ready.
- Symptom: an operator's `ESHU_DEMO_*` override stops working while every test
  here still passes → this package cannot catch that, because it takes the
  lookup as a parameter. `TestNewResolvedDemoRuntime_ReadsTheComposeFileOverride`
  in `go/cmd/eshu` is the test that covers it.

## Anti-patterns specific to this package

- **Reaching into `go/cmd/eshu`.** It cannot be imported (`package main`). If
  new logic needs something only the wrapper has, add a parameter.
- **Re-copying the criterion vocabulary.** `Criterion`, `CriterionName`,
  `CriterionStatus`, and their constants are imported from
  `go/internal/cli/firstrunbench`; `EnvelopeError` from
  `go/internal/cli/firstrun`, which owns the envelope contract. That is what
  lets one harness read both scorecards. A local mirror of any of them
  reintroduces the silent-drift risk the import removed. The one deliberate
  exception is `quoteIfEmpty`, whose placeholder must stay mode-neutral —
  see `criteria.go`.
- **Making the explicit build unconditional.** It was measured at 221,590 ms
  on a warm run. Instrumentation that slows what it measures is worse than the
  attribution it was added for.
- **Wrapping the errors marked `//nolint:wrapcheck`.** Those returns are text
  an operator reads; `go/cmd/eshu` is exempt from wrapcheck and this package is
  not, so wrapping them would silently change messages that were stable before
  the extraction.

## What NOT to change without an ADR

- The self-ownership rule in `Down`. Removing or loosening it means
  `eshu demo down --project <name>` can destroy a stack, its volumes, and its
  networks that an operator built for real work.
- The Compose project separation (`DefaultProject`). It is why the demo cannot
  collide with the local stack in the first place.
