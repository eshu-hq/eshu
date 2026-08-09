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

---

# Follow-up: probes and a loud marker write (#5974)

## What changed

Three things, all in the fault-injection gate's own path:

1. `writeOnceFiredMarker` reports a failed `os.WriteFile` on stderr instead of
   discarding it (`_ =`).
2. `fresh_stack` fails the run when `docker compose down -v` fails, instead of
   `|| true` with both streams to `/dev/null`.
3. `cell_failgraphwrite_sql` gained two probes: `shared_projection_intents` must
   be empty before the drive, and after the drain the SQL edge set is asserted
   and the cell's intent window is printed.

## No-Regression

No-Regression Evidence: none of this can execute in a released binary or on a
serving path. The marker write lives behind `//go:build ifafaultinjection`;
untagged builds compile `fault_executor_off.go` and released binaries are built
untagged, so the code is absent rather than merely inactive. The two probes and
the teardown check are lines in `scripts/lib/*.sh`, which only the gate runs.

Within the tagged build the marker write is still bounded by the same
`onceFired.CompareAndSwap` that makes the fault fire exactly once — at most one
write per process — and the added `if err != nil` costs a comparison on a path
that is already returning an injected error.

Measured on the `ifa-fault-injection` matrix (Postgres 15642 + NornicDB 7801 via
`docker-compose.yaml`), all nine cells green, exit 0. Digest
`5bfd92cacbb6758b29d3073426ce69e69c6cdcc87535981d8550873be86acfdb` across every
recovery cell, `deltaretract` differing as designed against its expected-v2 set,
zero dead letters throughout. Per-cell wall time: baseline 20s, killworker 73s,
expirelease 9s, failgraphwrite 68s, restartbackend 9s, killworkersql 70s,
duplicatedelivery 12s, deltaretract 11s, failgraphwritesql 7s. The probes add
one `COUNT(*)`, one `assert-edges` Bolt read, and one aggregate query to a
single cell; the cell remains the fastest in the matrix.

The teardown check adds one file write per cell (`compose-down-<cell>.log`) and
turns a previously-ignored non-zero exit into a `die`. That is strictly more
work only on the path that was silently broken before.

## Observability

Observability Evidence: this adds two operator-facing signals, both inside the
gate rather than the product.

```
ifa fault: once-fired marker write failed: <err> (path=<marker>)
fail-graph-write-once-then-succeed-sql: fresh-stack precondition: 0 shared_projection_intents
  sql_relationships intent window: <total>|<pending>|<first_created>|<last_completed>
```

The first is the point: a failed marker write used to be byte-identical to "the
fault never fired", so the gate reported one cause as fact when it had two. The
cell's failure message now names this prefix and tells the reader to look for it
before concluding anything.

The intent window is what distinguishes the remaining hypotheses. Local
baseline from the run above: `10|0|2026-08-09 00:26:33.045237+00|2026-08-09
00:26:34.630915+00` — ten intents, none pending, created and completed inside
the cell's own window. A CI run showing intents that predate the cell means the
stack was not fresh; one matching this shape with no marker means the statement
flowed and the fault did not intercept it.

No product metric, span, or telemetry-contract entry is added, so
`scripts/verify-telemetry-coverage.sh` reports no untracked stages.

## Reproduce

```
bash scripts/verify-ifa-fault-injection.sh
cd go && go test -tags ifafaultinjection ./internal/storage/cypher/ -count=1
bash scripts/test-verify-ifa-fault-injection.sh
```

Each guard fails without its fix: restoring the swallowed `down -v` turns the
mirror red on "the stack is NOT fresh", removing probe 1 turns it red on
"survived fresh_stack", and restoring the single-cause die message turns it red
on the assertion that a missing marker also means the marker write failed.

---

# Root cause: the assertion called a binary CI does not have (#5974)

## What changed

`ifa_fault_assert_once_fault_marker` in
`scripts/lib/ifa_fault_injection_common.sh` matched the marker's contents with
`rg`. ripgrep is not installed on the fault-injection runner, so the call exited
"command not found". A non-zero status there meant "the marker does not name the
operation", and the cell reported a fault that had fired perfectly as one that
never fired. The match is now a bash substring test, which cannot go missing.

The assertion also returns three distinct statuses instead of one, so the cell
can say which thing happened: 0 the marker names the targeted write, 2 it exists
but names a different write, 1 it is absent. Conflating 1 and 2 is what let a
wrong-target firing read as no firing at all.

## Why the earlier diagnoses were wrong

Two published explanations — a stderr-flush race, then "the fault does not fire
in CI" — were artifacts of this one missing binary. Both were retracted on the
issue. The eliminations behind them still stand; only the conclusion they pointed
at was wrong. A checker that cannot run must never be readable as a clean result,
which is the defect this fix removes.

A statement recorder was built along the way to answer "the fault never fired, so
what did run?". It is not part of this change: once the missing binary explained
everything, the recorder was answering a question that no longer existed, and its
own comments argued for a byte-identity theory this finding disproves. It was
removed before merge rather than shipped as a false explanation standing beside
the true one.

## No-Regression

No-Regression Evidence: no production Go changed. `fault_executor.go`,
`fault_executor_marker.go`, and `fault_executor_once_marker_test.go` are
byte-identical to the merge base, so there is no compiled surface to regress in
either build tag. The Go in this diff is two `internal/ifa` test files whose
assertions were inverted to match the corrected truth, and the shell change
replaces an external process call in a gate assertion with a bash substring
test, which removes a fork per assertion rather than adding one.

The last measured run of the targeted cell — 7s on the `ifa-fault-injection`
matrix (Postgres 15642 + NornicDB 7801), zero dead letters, recovery cells
holding digest
`5bfd92cacbb6758b29d3073426ce69e69c6cdcc87535981d8550873be86acfdb` — was taken on
the commit that still carried the recorder, so it is an upper bound on this
revision rather than a measurement of it: the removal only takes work off the
armed path. The number is stated that way deliberately instead of being carried
forward as though it had been re-measured here.

## Observability

No-Observability-Change: no metric, span, or telemetry-contract entry is added or
removed; `scripts/verify-telemetry-coverage.sh` reports no untracked stages. The
gate's failure text changed only in what it claims, not in where it prints.

## Reproduce

```
bash scripts/test-verify-ifa-fault-injection.sh
bash scripts/verify-ifa-fault-injection.sh
```

The guard bites. Run `ifa_fault_assert_once_fault_marker` under
`env PATH=/usr/bin:/bin`, which is the runner's condition in that `rg` is not on
it: the shipped version returns 0 for a marker naming the targeted operation, 2
for one naming a different write, and 1 for an absent marker, while restoring the
`rg` match returns non-zero for all three — including the marker that is
correct, which is the false negative that hid this for months.

The mirror bites too: removing the bash-substring rule, reintroducing an
`rg`-based match, or dropping `cell_failgraphwrite_sql` from the default matrix
each turn `test-verify-ifa-fault-injection.sh` red with a message naming the
rule, and restoring them returns it to green.
