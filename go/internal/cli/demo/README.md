# Demo Stack Lifecycle

## Purpose

`demo` runs the credential-free demo stack: it brings up a Docker Compose
project seeded with a synthetic organization, waits until the stack has
finished indexing, asks the first question from
`specs/demo-first-answers.v1.yaml`, and reports per-phase wall times. It also
scores the resulting `{data, truth, error}` envelope for time-to-first-answer,
which is what `eshu demo-benchmark` prints.

The stack runs under its own Compose project name (`eshu-demo` by default), so
the demo never adopts, restarts, or tears down a stack an operator started for
real work.

## Ownership boundary

This package owns the lifecycle logic: building the Compose argument vector,
the ownership guard, the readiness loop, the manifest question executor, and
the TTFA scorecard. It does not own process wiring. Reading cobra flags,
calling `os.Getwd` and `os.Getenv`, resolving `cmd.InOrStdin()` /
`cmd.OutOrStdout()`, and mapping an error to an exit code all stay in
`go/cmd/eshu/demo.go` and `go/cmd/eshu/demo_benchmark_cmd.go`, because
`go/cmd/eshu` is `package main` and nothing can import it.

`APIBase`, `MCPBase`, and `ResolveComposeFile` each take a
`getenv func(string) string` parameter instead of calling `os.Getenv`. The
wrapper passes `os.Getenv` in. Reading a file behind an explicit path
parameter is not process wiring — `LoadManifest` calls `os.ReadFile` directly,
the same shape as `internal/cli/servicereport`'s `ReadInput`.

## Exported surface

Lifecycle:

- `Options` / `NewRuntime` / `Runtime` — the resolved inputs and the runtime
  built from them
- `Runtime.Up`, `Runtime.Down`, `Runtime.Status` — the three commands
- `Result`, `Answer`, `IndexStatus` (with `IndexStatus.Complete`) — what they
  return
- `ExecFunc`, `ProbeFunc`, `AskFunc` — the injectable seams
- `DefaultProject`, `ComposeFileName`

Resolution helpers the wrapper calls before building a `Runtime`:

- `APIBase`, `MCPBase`, `ResolveComposeFile`
- `EnvBindAddr`, `EnvAPIPort`, `EnvMCPPort`, `EnvComposeFile` — the variable
  names those three consult through the caller's lookup

Manifest:

- `ManifestPath`, `LoadManifest`, `Manifest`, `Question`, `Execute`
- `Question.RunnableForm` — the pasteable command for one question
- `ExecuteQuestion`, `AskQuestion`

Output and scoring:

- `Envelope`, `EnvelopeFor`, `WriteJSON`, `PrintSuccess` (the envelope's
  error field is `firstrunbench.EnvelopeError`, imported, not mirrored)
- `EvaluateBenchmark`, `BenchmarkMeasurements`, `BenchmarkVerdict` (with
  `Criterion` and `FailureReasons`), `RenderBenchmarkVerdict`
- `ParseImageState`, `ImageState` (`ImagesUnknown` / `ImagesPresent` /
  `ImagesAbsent`), `ModeCold`, `ModeWarm`, `RequiredPhases`
- `Criterion`, `CriterionName`, `CriterionStatus` and their constants

See `doc.go` for the godoc contract and the full subprocess/network/file
surface.

## Dependencies

Outside the standard library: `gopkg.in/yaml.v3`, for the manifest. Its only
Eshu import is `go/internal/cli/firstrunbench`, for the shared scorecard
vocabulary and envelope error object — `go list -deps ./internal/cli/demo`
names exactly those two beyond the standard library. The package sits below
the graph, storage, and query layers rather than beside them.

Consumed by `go/cmd/eshu`: `demo.go` (the `demo up|down|status` tree) and
`demo_benchmark_cmd.go` (the `demo-benchmark` scorer).

## Telemetry

None. The demo runs inline with the CLI invocation and reports its own
per-phase timings in `Result.PhaseMillis`; there is no background pipeline
stage to instrument. The timings in `PhaseMillis` are the raw input the TTFA
measurement lane consumes.

## Gotchas / invariants

- **`Down` refuses a project it did not create.** `ownsProject` reads
  Compose's own `com.docker.compose.project.config_files` label off every
  container in the project and requires each entry's basename to equal
  `ComposeFileName`. Comparison is per comma-separated entry and on the
  basename, not a substring search over the label: `not-docker-compose.demo.yaml.bak`
  must not count as ownership. A project with no containers counts as owned,
  so a `down` after a failed `up` can still clean up.
- **Readiness is indexing completeness, not health.** `IndexStatus.Complete`
  requires `status == "healthy"`, at least one indexed repository, and an
  empty queue. Dropping the queue check would let the demo ask its question
  mid-projection and get a thin answer.
- **`Up` never adopts a running project.** `alreadyRunning` fails closed: a
  `compose ps` error is not proof the project is free.
- **Every compose call goes through `composeArgs`**, which prepends
  `-p <project> -f <file>`. A call that skips it would act on the operator's
  default stack.
- **The build phase is conditional.** An unconditional `docker compose build`
  revalidates every build context even when nothing changed, measured at
  221,590 ms on an otherwise warm run, so the explicit build runs only when
  `docker image inspect` reports a missing image.
- **`Status` recovers the stack's own key.** `Up`'s ephemeral credential dies
  with its process, so `Status` reads `ESHU_API_KEY` back out of the running
  `eshu` service instead of minting a second key the stack would reject.
  `serviceName` must match the service key in
  `docker-compose.demo.runtime.yaml`; `TestDemoServiceNameMatchesComposeOverlay`
  reads the committed fragment and fails if they drift.
- **The scorecard vocabulary is imported from
  `go/internal/cli/firstrunbench`**, not copied. `Criterion`, `CriterionName`,
  `CriterionStatus`, their constants, and `EnvelopeError` are that package's
  exported types, so the two scorecards cannot drift apart. Only the two
  demo-only criterion names (`CriterionPhaseTimings`, `CriterionModeObserved`)
  live in `criteria.go`.

## Related docs

- `docs/public/run-locally/docker-compose.md` — the Compose stacks this
  overlay sits beside
- `specs/demo-first-answers.v1.yaml` — the acceptance oracle for the questions
  the demo asks
- `go/internal/cli/servicereport/README.md` — the sibling extraction whose
  wrapper/package split this one follows
