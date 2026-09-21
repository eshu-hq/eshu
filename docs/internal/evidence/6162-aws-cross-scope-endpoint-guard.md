# #6162 cross-scope endpoint guard, AWS side

Mirrors the GCP guard from #6913 for the AWS relationship join.

## What this change does

`ExtractAWSRelationshipEdgeRows` now refuses a relationship endpoint scanned in
a different ingestion scope than the executing intent, counts it under a
`cross_scope_endpoint` join mode keyed by target type, and logs a WARN when the
count is non-zero.

`cloudjoin.CloudResourceJoinIndex` documented the same rule the GCP index did —
"a cross-account or cross-region ARN target resolves only if that
account+region resource was scanned in the same scope (the trust-boundary rule,
design §10.3)" — and did not enforce it either.

`CloudResourceEdgeWriter` stamps `rel.scope_id` and `rel.generation_id` from the
intent and never from the row, exactly as the GCP writer does, so an admitted
cross-scope row is written under this intent's scope.

## Why the enforcement point is the uid

The index carries four maps — `ByARN`, `ByUID`, `ByResourceID`, `ByAnchor` — and
all of them resolve into one uid space. Checking the resolved uid therefore
covers the ARN, bare-id and correlation-anchor paths at once, without widening
four exported map types.

## Performance Evidence:

`BenchmarkExtractAWSRelationshipEdgeRows`, Go 1.27.1 darwin/arm64,
`-benchtime=200x -count=5`, each variant run back to back on the same machine.

| | median ns/op | ns/op range | B/op | allocs/op |
| --- | --- | --- | --- | --- |
| baseline `cf433ea5c` | 36,084,210 | 35,485,305 – 36,270,348 | 41,749,224 | 441,696 |
| first attempt, uid-keyed scope map | 37,242,079 | 37,130,584 – 37,805,654 | 42,405,028 | 441,731 |
| shipped, single-scope fast path | 36,317,086 | 36,148,798 – 36,595,101 | 41,749,208 | 441,696 |

The first attempt allocated a fifth uid-keyed map on every build and measured
**+3.2% time and +1.57% memory**, with ranges that did not overlap the baseline.

The shipped version records the one scope every resource was scanned in and
falls back to a uid-keyed map only for resources that disagree with it. The
scoped fact loader means nothing disagrees in production, so that map stays nil
and the check is a string compare. Allocations and bytes per op return exactly
to baseline (441,696 allocs, within 16 B), and time is +0.65% at the median with
overlapping ranges.

This repeats the GCP result: the guard is free when implemented so the normal
path does no extra bookkeeping, and costs a few percent when it allocates for a
case that never happens.

## No-Observability-Change:

No span, metric, status surface or dashboard input is added, removed or renamed.
Two structured-log fields are added on the reducer's own completion path:
`cross_scope_endpoint_count` on the existing INFO line, and a new WARN,
`aws relationship materialization refused cross-scope endpoints`, emitted only
when the count is non-zero.

The WARN is deliberate. #6162 records that the original failure produced "860
INFO lines, zero ERROR lines" and that its cause "has never been captured in any
run on any branch". A refusal counted only inside an INFO burst would repeat
that.

## Seeded RED/GREEN

```
guard disabled:  FAIL  len(rows) = 1, want 0: a cross-scope source endpoint must not produce a row
guard restored:  PASS  go test ./internal/reducer/... -count=1  exit 0 (68 packages)
```

## Not covered here

`awscloud` and `iamescalation` build the same shared index and resolve endpoints
through it. They can adopt `ScopeInBounds` cheaply now that the index carries the
scope, but neither is changed here and neither is measured here.
