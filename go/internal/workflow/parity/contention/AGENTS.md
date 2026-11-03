# AGENTS.md — internal/workflow/parity/contention guidance for LLM assistants

## Read first

1. `README.md` here — spec readings, environment, and the live runbook
2. `go/cmd/workflow-coordinator/REMOVAL_CONTENTION_SPEC.md` — the contract
3. `doc.go` — godoc contract
4. `run.go`, `worker.go`, `loops.go` — phases, the §5 mix, the sampler
5. `tracker.go`, `observer.go` — the two independent §7 detectors

## Invariants this package enforces

- **One prefix** — every id the harness writes starts with `Prefix`
  (`synth-8875-rehearsal-`). Cleanup and residue filter on it. Never add a
  write without the prefix, and never widen a LIKE pattern.
- **Never TRUNCATE, never the parity satellite reset** — cleanup is
  `Observer.DeletePrefixed`: child tables first, bounded ctid batches.
- **Never call `ReconcileCollectorInstances` or `ReconcileWorkflowRuns`** —
  both are global and rewrite rows the harness does not own. The live leg
  also never calls the reap route (`HarnessReaper` false): it is global too.
- **Fail closed** — `LoadSpec` refuses a blank §2 field; the entry tests
  refuse a non-loopback DSN or URL and a satellite DSN naming `eshu` or a
  maintenance database.
- **Two detectors per abort trigger** — the tracker orders events only by
  happens-before (a response received before a request left); the sampler
  reads the database. Do not reduce either to response-arrival order: that
  produces false dual claims under real concurrency.
- **Seeded-violation proof** — each abort trigger has a RED case in
  `run_test.go` against `fake_test.go`. A new trigger needs a new planted
  fault there. The fake must mirror the real backend's identity rules: the
  guarded-admission target key caught a harness bug only once it did.
- **No serialization** — workers run concurrently at W = 1x and 2x the
  production worker count. Do not reduce W, the batch size, or the reaper to
  make a criterion pass.
- **Not applicable is not PASS** — `NOT_APPLICABLE` records carry the parity
  sentinel (`ErrPinNotSupported`, `ErrGuardedAdmissionNotSupported`).

## Verification

```sh
go test ./internal/workflow/parity/contention/ -count=1 -race
```

Without `ESHU_CONTENTION_TARGET` the two entry tests skip; the unit and
fake-leg tests always run. Live runs follow the README runbook only.
