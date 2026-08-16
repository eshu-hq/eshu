# AGENTS — cmd/ci-gates

Scoped rules for editing the CI gate CLI. Load `golang-engineering`.

## Read first

1. `README.md` — subcommand semantics and ownership boundary.
2. `main.go` — CLI parsing, output formatting, shell dispatch.
3. `go/internal/cigates/README.md` — registry loader and selector.
4. `specs/ci-gates.v1.yaml` — the live registry this command reads.

## Invariants

- **No business logic here.** Selection, validation, and glob matching belong
  in `internal/cigates`. This package owns only CLI flags, output formatting,
  and the `/bin/sh -c` dispatch in `executeGates`.
- **Accumulate, never stop early.** `executeGates` must run all selected gates
  even when a blocking gate fails. The exit code is set at the end.
- **Self-tests are executable policy.** A selected gate runs `local.command`
  and then a distinct, non-empty `local.test_command`; byte-identical pairs run
  once. A primary-command failure does not suppress the self-test.
- **Advisory failures do not fail the exit code.** Only `Gate.Blocking==true`
  failures contribute to a non-zero exit.
- **Gate subprocesses never inherit GOROOT.** `gateSubprocessEnv` strips it
  because the `go run` that launches this binary switches toolchains for
  go/go.mod and exports the switched GOROOT; handing it to a gate whose module
  the host toolchain satisfies mixes go driver and tools ("compile: version …
  does not match go tool version …"). Removing the strip turns
  `TestGateSubprocessEnvStripsLeakedGOROOT` and the sdk-go-collector /
  scorecard-example-conformance gates red on such hosts (see #6113 for the
  precommit-go.sh sibling).
- **`--paths-from` enables hermetic tests.** Any test that needs deterministic
  path input must use `--paths-from`; never rely on git state in tests.
- **Files stay under 500 lines.** Split into a new file before the cap.

## Common changes

- Adding a flag: add to the relevant `flag.FlagSet`, thread through to the
  logic, add a test case in `main_test.go`.
- Adding a subcommand: add a `case` in `main()`, write a `run<Sub>` function,
  add tests, update `usage()`.
- Changing output format: update both `printSelectText`/`printSelectJSON` and
  the corresponding test assertions.

## Tests

```bash
cd go && go test ./cmd/ci-gates/ -count=1
```

Tests compile and run the binary via `exec.Command`; they are integration-style
but credential-free and do not touch the network.
