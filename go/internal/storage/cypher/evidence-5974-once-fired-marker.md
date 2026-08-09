# Evidence: once-fired fault marker (#5974)

## What changed

`FaultingExecutor.maybeFailOnce` now writes a small marker file the moment the
scripted `fail-graph-write-once-then-succeed` fault fires, recording the lane and
the Cypher operation it hit. `cell_failgraphwrite_sql` reads that marker instead
of polling the reducer's captured stderr.

## Why it was needed

The old proof was a bounded `rg` poll over the reducer log with a 10s budget.
That races the logger's flush: this repo already abandoned the same technique
once, and the comment recording why sits a few lines above it in
`scripts/lib/ifa_fault_injection_common.sh` — the injected-failure line reached
the captured file a minute-plus after the event in CI. The SQL cell inherited
that race and went inert in CI while passing locally, which is #5974.

A wider budget would not fix it. It would make the cell pass without making the
evidence arrive any sooner, and a budget generous enough to be safe against a
minute-plus lag is indistinguishable from not checking at all.

## No-Regression

No-Regression Evidence: the marker cannot execute in a production binary. The
whole file is behind `//go:build ifafaultinjection`; the untagged build compiles
`fault_executor_off.go` instead, and `go test ./internal/storage/cypher/`
without the tag stays green. Released binaries are built untagged, so this code
is absent from them rather than merely inactive.

Inside the tagged build the write is bounded by construction, not by
convention. It sits after
`fe.onceFired.CompareAndSwap(false, true)` succeeds, which by definition happens
at most once per process — the same guard that makes the fault fire exactly
once. A run with no scripted once-fault never reaches it (`fe.onceLane == ""`
returns earlier), and a run whose sentinel path is empty skips it outright.

So the cost is one `os.WriteFile` of roughly 120 bytes, once, in a test-only
binary, on a path that is already returning an injected error. It is not on the
success path of any graph write.

Measured on the `ifa-fault-injection` matrix (Postgres 15642 + NornicDB 7801),
all nine cells produced the identical graph digest
`5bfd92cacbb6758b29d3073426ce69e69c6cdcc87535981d8550873be86acfdb` — except
`deltaretract`, which changes the graph on purpose and is asserted against its
expected-v2 edge set — with zero dead letters. Per-cell wall time on the clean
run: baseline 20s, killworker 73s, expirelease 9s, failgraphwrite 68s,
restartbackend 9s, killworkersql 70s, duplicatedelivery 12s, deltaretract 11s,
failgraphwritesql 7s. The cell this change targets is the fastest in the matrix;
nothing in that spread is attributable to the marker.

## Observability

No-Observability-Change: this adds no metric, span, or operator-facing log. The
marker is a coordination file between a test binary and the gate script that
launched it, written under the gate's own working directory and torn down with
the cell. `scripts/verify-telemetry-coverage.sh` reports no untracked stages.

The operator-facing signal for this failure class already exists and is
unchanged: `SharedProjectionRunner` still logs partition-processing failures
with domain, partition id, and error text (#5555). What changed is only that the
*gate* no longer depends on that log line arriving within a poll window.

## Reproduce

```
cd go && go test -tags ifafaultinjection ./internal/storage/cypher/ -run OnceFiredMarker -count=1
bash scripts/verify-ifa-fault-injection.sh
```

The unit test fails without the change: deleting the
`fe.writeOnceFiredMarker(ordinal, stmts)` call makes
`TestFaultingExecutorWritesOnceFiredMarker` red at the marker read, and
restoring it goes green. The live gate exercises the same path end to end with
`cell_failgraphwrite_sql` enabled for the first time since it was held out.
