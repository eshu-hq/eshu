# compparity

## Purpose

Logic behind `eshu competitive-parity validate`, the offline gate that checks
shipped Eshu surfaces against the peer-baseline expectations in
`internal/competitiveparity`. This package assembles the live inventory the
validator scores and runs the exercises that prove sibling artifact paths
still work.

## Ownership boundary

This package owns inventory assembly and exercise execution. It does not own
scoring: expectations, validation, and report rendering belong to
`internal/competitiveparity`, and `Artifact` only delegates to that package's
renderers. It also owns nothing cobra-shaped — registration, flags, streams,
and exit codes stay in `go/cmd/eshu/competitive_parity_cmd.go`, which is
`package main` and therefore cannot be imported from here.

## Exported surface

See `doc.go` for the godoc contract. Callers use `Inventory` and `Artifact`;
that is the whole caller-facing surface.
One input is injected by the wrapper because its source lives in
`package main`: the CLI command paths, since walking the cobra tree needs
`rootCmd`. The supply-chain fixture builder is unexported
(`supportedSupplyChainPacket`): its only callers are in this package, and
`exercises_test.go` is an in-package test, so it needs no export to reach it.

## Dependencies

Intra-repo: `internal/capabilitycatalog` (surface inventory and catalog),
`internal/competitiveparity` (inventory/report types and rendering),
`internal/cli/firstrun`, `internal/cli/opdigest` and `internal/cli/evidpacket`
(exercised sibling CLI logic), `internal/packetdogfood` (benchmark parsing and
scoring), and `internal/query` (investigation packet building and rendering).
No cobra:
`go list -deps ./internal/cli/compparity` resolves nothing under
`github.com/spf13`. Re-derive import lists with
`go list -f '{{.Imports}}' ./internal/cli/compparity` rather than editing
them by hand.

## Telemetry

None. The gate runs inline with a single CLI invocation and reports through
its artifact and exit code.

## Gotchas / invariants

- Filesystem access is read-only: the committed parity docs at `docPaths`
  and the dogfood fixture benchmark, both joined onto the caller's repoRoot.
  No file is written, no environment variable is read, no subprocess or
  network call is made.
- A missing doc file is skipped by `Inventory` — the validator reports it as
  a failed doc check — but any other read failure is returned as an error.
- Exercise failure details are static per-ID strings. The underlying errors
  can carry local paths, and the artifact is share-safe; never put the real
  error text in an `ExerciseResult`.
- Every exercise runs from this package. The `first_run_report_artifact`
  exercise drives `internal/cli/firstrun`'s pure evidence render — it starts
  no runtime and contacts nothing — so no exercise is injected any more and
  none can be left unwired.
- The exercise IDs and their order are part of the artifact consumers see;
  treat them as a contract.

## Related docs

`docs/public/reference/local-testing/competitive-parity-gate.md` describes
the gate; `internal/competitiveparity/README.md` covers scoring.
