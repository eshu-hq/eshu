# AGENTS.md — go/internal/cli/change guidance for LLM assistants

## Read first

1. `go/internal/cli/change/README.md` — what this package owns and what it
   deliberately leaves in the cobra wrapper
2. `go/internal/cli/change/doc.go` — the godoc contract
3. `go/cmd/eshu/change_impact.go` — the wrapper: flag reading, the API client,
   and `changeExitCode`, the exit-code table
4. `go/internal/cli/apierr/README.md` — how an HTTP status crosses the
   `package main` boundary

## Invariants this package enforces

- **No cobra, no environment, no os.Stdout, no file handles.** Every function
  takes plain values and an `io.Writer`. Check it, do not assume it:
  `cd go && go list -deps ./internal/cli/change | rg spf13` must print nothing.
- **This package never decides an exit code.** It returns a `Failure` naming a
  `FailureKind`; `changeExitCode` in the wrapper turns that into a number.
  Moving the table in here looks like tidying and changes behavior: the shared
  `traceExitCode` answers 1 for `building` and 1 for `truncated`, where this
  family exits 4 and 5.
- **Message checks run before the status switch in `ErrorCodeFromTransport`.**
  Reordering them is a behavior change with exactly one visible case, and
  `TestErrorCodeFromTransportPrecedence` is the test that shows it.
- **The operator-facing strings are the CLI contract.** Every `Validate`
  message names flags an operator typed, and the summary lines are parsed by
  people and scripts. Reword nothing without treating it as a user-visible
  change.
- **Freshness before truncation** in both `ClassifyImpact` and `ClassifyPlan`.

## Common changes and how to scope them

- **A new flag** → add the field to `Options`, read it in the wrapper's
  `changeImpactOptionsFromCommand`, add it to `ImpactRequestBody`, and update
  the key-count assertions in `TestRequestBodiesCarryEveryOption`. The counts
  are there so a new key cannot arrive untested.
- **A new fail-closed condition** → add a `FailureKind`, add it to `Kinds()`
  right below the const block, extend `ClassifyImpact` or `ClassifyPlan`, and
  add the matching arm to `changeExitCode`. The `exhaustive` linter will not
  catch a missing arm: the switch has a `default` and `go/.golangci.yml` sets
  `default-signifies-exhaustive`, so it reads as complete whatever it lists.
  `TestChangeExitCodeMapping` walks `change.Kinds()` and fails if a declared
  kind has no table row, or if its only rows expect the same exit code an
  unrecognised kind gets — which is what a missing arm produces. That guard is
  only as complete as `Kinds()`, so the one step nothing checks for you is
  adding the constant to that slice.
- **A new rendering line** → put it in `renderImpactSummary` or
  `renderPlanSummary` and extend the exact-output assertions in
  `render_test.go`. Those compare whole strings on purpose.
- **A third `change` subcommand** → it gets its own `Classify*`, `Finish*`, and
  request body. Do not add a mode flag to the existing pair; the two contracts
  already differ in three places (`blocked`, the freshness line, the message
  wording).

## Failure modes and how to debug

- Symptom: `eshu change impact` exits 1 where it used to exit 4 or 5 → someone
  routed `KindFreshness` or `KindIncomplete` through `traceExitCode`. Run
  `go test ./cmd/eshu/ -run TestChangeExitCodeMapping`.
- Symptom: a rename shows up as an unrelated add and delete → the
  `--find-renames` / `--find-copies` flags were dropped from
  `GitDiffNameStatus`, or the three-field line is being read two-field.
- Symptom: the API rejects a request complaining about a missing
  `changed_paths` → something returned a nil slice where `CleanValues` used to
  guarantee an empty one, and it marshaled as `null`.
- Symptom: an unreachable backend reports `invalid_argument` → the message
  checks in `ErrorCodeFromTransport` moved after the status switch.

## Anti-patterns specific to this package

- **Sharing the trace helpers with `go/cmd/eshu` by importing across the
  boundary.** It cannot be done — that directory is `package main`. The
  duplication here is deliberate and both sides still have callers. A shared
  home is a separate decision.
- **Screening the envelope error message.** It is printed verbatim today, the
  behavior is pinned by a test, and quietly adding a filter would hide the
  service URL an operator needs in order to see which endpoint failed.
- **Testing rendering by re-implementing the format string in the test.** The
  assertions compare against literal expected output for a reason: a test that
  builds the expectation the same way the code does passes while both are
  wrong.

## What NOT to change without an ADR

- The exit-code split between this package and the wrapper. Operators script
  against these numbers.
- The two route constants and the request-body key sets. They are an API
  contract shared with `go/internal/query`.
