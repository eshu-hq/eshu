# #6162 cross-scope endpoint guard

## What this change does

`ExtractGCPRelationshipEdgeRows` now refuses a relationship endpoint that was
scanned in a different ingestion scope than the intent being executed, counts
the refusal under `cross_scope_endpoint`, and logs a WARN naming the scope and
the affected target types.

The join index type already documented the rule — "an endpoint resolves only if
that resource was scanned in the same scope (the trust-boundary rule)" — but
nothing enforced it. The scoped fact loader was the only thing standing between
a cross-scope fact and a misattributed edge.

## Why it matters

`GCPCloudResourceEdgeWriter.WriteCloudResourceEdges` stamps `rel.scope_id` and
`rel.generation_id` from the executing intent, never from the row:

```go
cloned["scope_id"] = scopeID
cloned["generation_id"] = generationID
```

So a row built against another scope's resource is written under *this* intent's
scope. `rel.scope_id` is also what the prior-generation retract filters on, so
the misattribution is durable rather than transient.

## Observed failure this guards against

Ifá run 35598054762, `fault-injection (shard 4/4)`, cell `restartbackend`, from
the artifact `ifa-fault-injection-shard-4-attempt-1-failure`:

| scope | baseline edges | after restart |
| --- | --- | --- |
| `acme-demo-gcp-00` | 63 | 0 |
| `acme-demo-gcp-03` | 63 | 97 |
| `supply-chain-demo-project` | 123 | 152 |

All 34 surplus edges on `acme-demo-gcp-03` and all 29 on
`supply-chain-demo-project` have `from` endpoints belonging to
`acme-demo-gcp-00`; 34 + 29 = 63, exactly the vanished scope's edge count. Node
and edge totals were identical across both dumps (678 / 707), which is why every
count-based assertion passed and only the graph digest moved.

All 622 `CloudResource` nodes were present in both dumps, including all 64 of
`acme-demo-gcp-00`'s, so this was not lost endpoint nodes.

## Scope of the fix, and what it does not close

This is defence in depth at the join. It does not explain how cross-scope facts
reached a loader scoped by `(scope_id, generation_id)`; that remains open on
#6162. The guard converts a silent misattribution into a counted, logged refusal
so the next occurrence names itself instead of being reconstructed from dumps.

`aws_relationship_join.go` carries the same unenforced trust-boundary language,
but resolves endpoints by ARN or resource id through separate helpers rather
than one full-resource-name map. That is a differently shaped change and is not
bundled here; it needs its own measurement.

## Performance Evidence:

`BenchmarkExtractGCPRelationshipEdgeRows`, Go 1.27.1 darwin/arm64,
`-benchtime=200x -count=5`, baseline and candidate run back to back on the same
machine (an earlier cross-time comparison was discarded: the same baseline
measured 43.1 ms earlier and 45.0 ms later, so only back-to-back numbers are
used here).

| | baseline `e3666db96` | with guard |
| --- | --- | --- |
| ns/op (median) | 44,985,596 | 44,800,987 |
| ns/op (range) | 44,772,769 – 45,438,566 | 44,232,880 – 45,298,171 |
| B/op | 56,533,067 | 56,795,391 |
| allocs/op | 575,138 | 575,141 |

Time is within noise — the ranges overlap and the candidate median is lower.
Memory is +262,324 B (+0.46%) and +3 allocs/op, from widening the index value
to carry the scope.

A first implementation used a parallel `map[string]string` for scope and
measured +5.6% on this benchmark. Folding the scope into the existing map value
(`gcpResolvedResource{uid, scopeID}`) removed the second allocation and the
second hash lookup and erased the regression; that is the implementation here.

## No-Observability-Change:

No span, metric, status surface or dashboard input is added, removed or renamed.
Two structured-log fields are added to the reducer's own completion path:
`cross_scope_endpoint_count` on the existing INFO line, and a new WARN,
`gcp relationship materialization refused cross-scope endpoints`, emitted only
when the count is non-zero.

The WARN is deliberate. #6162 records that the original failure produced "860
INFO lines, zero ERROR lines" and that its cause "has never been captured in any
run on any branch". A refusal counted only inside an INFO burst would repeat
that.
