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

# Follow-up: recording what actually ran (#5974)

## What changed

While a substring-matched once-fault is armed, `FaultingExecutor` appends each
**distinct** statement shape it sees (first line only, deduped in memory) to a
file beside the marker. When the marker is missing, the gate prints those next
to the anchor.

## Why

"The fault never fired" is half an observation. The other half — what did run,
so the anchor can be compared against it — was never recorded, and #5974 stalled
precisely there: the QUERIES_TABLE MERGE demonstrably executed in CI, in a
process whose decorator provably worked, and nothing captured the text it
executed with. A red run now answers "here is what the anchor should have said".

## No-Regression

No-Regression Evidence: this cannot execute in a released binary. The file is
behind `//go:build ifafaultinjection`; untagged builds compile
`fault_executor_off.go`, and releases are built untagged, so the code is absent
rather than inactive.

Inside the tagged build the work is bounded by distinct statement shapes, not by
call count: a drive issuing thousands of writes appends a handful of lines,
because every repeat short-circuits on a map lookup under a mutex held only for
that check. It is skipped entirely when the fault matches by ordinal rather than
by substring, and when no marker path was supplied.

Measured on the `ifa-fault-injection` matrix (Postgres 15642 + NornicDB 7801),
all nine cells green, exit 0, digest
`5bfd92cacbb6758b29d3073426ce69e69c6cdcc87535981d8550873be86acfdb` across the
recovery cells with `deltaretract` differing as designed, zero dead letters. The
targeted cell ran in 8s — unchanged from the 7s and 9s measured before this
recorder existed, which is within the run-to-run spread of that cell.

## Observability

Observability Evidence: one gate-facing signal, printed only on the failure path:

```
=== statement shapes this cell actually executed (anchor: <anchor>) ===
<distinct first lines>
=== end observed statements ===
```

An empty record is reported differently on purpose — it means the executor saw
no statements at all while armed, which points at wiring rather than at the
anchor. Distinguishing those two is the whole point; conflating them is what
#5974 did for weeks.

No product metric, span, or telemetry-contract entry is added, so
`scripts/verify-telemetry-coverage.sh` reports no untracked stages.

## Reproduce

```
cd go && go test -tags ifafaultinjection ./internal/storage/cypher/ -run RecordsObservedOperations -count=1
bash scripts/verify-ifa-fault-injection.sh
```

The test arms an anchor that matches nothing, asserts no marker is written, and
asserts both distinct shapes are recorded while the repeated one is not.
Disabling the recorder turns it red.
