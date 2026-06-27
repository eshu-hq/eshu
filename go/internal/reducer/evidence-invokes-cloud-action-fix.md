<!-- SPDX-License-Identifier: MIT -->
<!-- Copyright (c) 2025-2026 eshu-hq -->

# Evidence: INVOKES_CLOUD_ACTION upsert no longer dropped by filterUpsertRows

## Change

`buildInvokesCloudActionIntentRows` stored the cloud action (e.g. `s3:putobject`)
in the shared-projection intent payload under the `action` key. The shared
projection worker's `filterUpsertRows` reads `payload["action"]` as the
upsert/refresh/delete discriminator and drops any row whose action is not
`"upsert"`, so every INVOKES_CLOUD_ACTION upsert row was silently dropped — the
intent completed but `WriteEdges` received zero rows, and the
`Function-[:INVOKES_CLOUD_ACTION]->CloudAction` edge never materialized on any
backend. The cloud action now lives under `cloud_action`; the canonical writer
(`buildInvokesCloudActionRowMap`) reads it from there. The refresh intent keeps
`action: "refresh"`, so the repo-wide-retract fence is unchanged.

## Performance

No-Regression Evidence: This is a payload-key rename plus a one-field read change
on the existing INVOKES_CLOUD_ACTION shared-projection path; it adds no new query,
graph-write, intent, or per-row work. Before the fix the edge never wrote (0 rows
reached `WriteEdges`); after the fix exactly the resolved rows write through the
unchanged batched UNWIND upsert. The B-7 golden-corpus gate drains the full corpus
green in ~35s across 3 consecutive fresh-graph runs, with `rc-10`
`(Function)-[:INVOKES_CLOUD_ACTION]->(CloudAction)` count=1 every run and all
other required correlations (rc-1..rc-8, rc-11..rc-23) unchanged at 32 pass / 0
required-fail / 0 advisory-warn. Unit coverage: a regression test asserts the
emitted upsert intent survives `filterUpsertRows`, plus the reducer intent and
cypher edge-writer dispatch tests.

## Observability

No-Observability-Change: the fix reuses the existing shared-projection
instrumentation (the `eshu_dp_*` shared-edge-write counters/spans and the
per-domain `invokes_cloud_action` projection signals). No new metric, span, or
log key is introduced; an operator now sees the edge actually write through the
same shared-edge-write signals that previously reported zero rows for the domain.
