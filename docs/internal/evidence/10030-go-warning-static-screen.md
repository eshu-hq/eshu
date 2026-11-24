# Go warning static screen: no-regression evidence (#10030)

The engaged authorized loader screens every fact the static impact load returns.
Before this change it failed the pass closed on any `scanner_worker.warning`,
because the kind is target-only for image analyzers. The cross-scope Go stage
returns a `go_reachability` warning from a go-reachability scope, so a Go scan
that did not finish tripped the screen while the same scan's call verdicts
passed. The screen now decides by analyzer and scope for that one warning.

## What runs

`staticEnvelopeNeedsScanReceipt` replaces the direct `targetOnlyImpactFactKind`
call inside the loader's existing loop over the static envelopes. For a
target-only kind it reads the tombstone flag, trims the scope id's
`go-reachability://` prefix, matches the rest against the canonical repository
id pattern, and compares the `analyzer` payload value with `go_reachability`.
Only a `scanner_worker.warning` reaches those checks; every other kind returns
at the first switch, as before.

There is no new query, no new row read, no new write and no new loop. The input
is the envelopes of one authorized load, and the loop already visited each one.

## Measurement

Baseline: `b9d630a45a`, the base of this branch, in a throwaway detached
worktree. After: this branch. Backend: none. The benchmark
(`BenchmarkStaticEnvelopeScreen`) calls the screen over 1,000 in-memory
envelopes: 70 percent package-consumption facts, 10 percent go-reachability
module evidence, 10 percent `go_reachability` warnings in a go-reachability
scope, and 10 percent image-analyzer warnings. At the base commit the same file
calls `targetOnlyImpactFactKind(envelope.FactKind)`, which is what the loop
called before, so both trees run identical inputs. Host: shared Linux box with
other agents running, with `nice -n 19` and `GOMAXPROCS=2`; two rounds per tree,
alternating, of 10 runs each.

```text
go test ./internal/storage/postgres/ -run '^$' -bench BenchmarkStaticEnvelopeScreen -benchtime=20000x -count=10
```

To reproduce the baseline column, check out the base commit (`b9d630a45a`), copy
`supply_chain_impact_authorized_static_screen_bench_test.go` from this branch
into `go/internal/storage/postgres/`, and make two substitutions in it, because
the base has no `staticEnvelopeNeedsScanReceipt` and its screen fails closed on
every warning:

```text
sed -i -e 's/staticEnvelopeNeedsScanReceipt(envelope)/targetOnlyImpactFactKind(envelope.FactKind)/' \
       -e 's/failClosed != 100/failClosed != 200/' \
       go/internal/storage/postgres/supply_chain_impact_authorized_static_screen_bench_test.go
```

The first line swaps the predicate for the call the loader made before this
change; the second moves the expected count to the 200 warnings the base fails
closed on. Then run the command above. On this branch the benchmark fails
itself unless exactly 100 envelopes fail closed.

| | baseline | after |
| --- | --- | --- |
| ns/op per 1,000 envelopes, median of 10 (round 1 / round 2) | 3,978 / 3,984 | 18,358 / 18,332 |
| ns/op per 1,000 envelopes, minimum of 10 (round 1 / round 2) | 3,976 / 3,980 | 18,285 / 18,303 |
| B/op, allocs/op | 0, 0 | 0, 0 |

The screen adds about 14 microseconds per 1,000 envelopes, about 14 nanoseconds
per envelope, and no allocation. The cost sits almost entirely in the pattern
match for the 100 Go-scope warnings in this input, so it is an upper bound: a
real authorized load carries a handful of warnings among many thousands of
facts. The load itself is a repeatable-read snapshot of database reads that take
milliseconds to seconds, so the added work is not measurable beside it.

Terminal counts: the benchmark counts envelopes that fail closed and, on this branch, fails unless that count is exactly 100. The baseline
fails closed on 200 of 1,000 (every warning); the after tree fails closed on 100
(the image-analyzer warnings), which is the intended change. A twin table test
(`TestStaticEnvelopeNeedsScanReceiptKeepsGoEvidenceStatic`) pins each edge: an
empty, padded or upper-case analyzer, no payload, a scope with a path suffix, a
non-canonical scope id, an image scope, and a tombstoned warning all still fail
closed.

No-Regression Evidence: the table above. The predicate adds about 14 ns per
envelope with zero allocations over a loop the loader already ran, and adds no
read or write to the load.

## Observability

No-Observability-Change: the load already reports its outcome through the
engaged flag, the complete flag, the bounded coverage reason (`policy_unavailable`
when a target-only fact fails the pass closed), the static non-advisory evidence
fact ids, and the `authorized impact coverage incomplete` warn log with
`failure_class` and `coverage_reason`. No metric, span or log field was added.

At 3 AM, an operator who sees a Go scan that did not finish reads it on the
finding itself: `reachability.missing_evidence` carries
`govulncheck scan <reason> at <path>` (for example `govulncheck scan
toolchain_unsupported at legacy/go.mod`) and `runtime_reachability` reads
`analysis_failed`. A pass that still fails closed on a target-only fact shows
the `policy_unavailable` coverage reason in the warn log and in the retried
queue row, as before.

## Limits

- The benchmark measures the predicate, not a Postgres load. No terminal queue
  or row counts apply: this change reads and writes no row.
- The engaged finding verdict is proven through a fake authorized loader and a
  live engaged load; no live test builds an engaged finding from registered
  public advisory authority.
