# #7246 change-planning topic terms

## Accepted result change

The caller approved removing file and entity results that match only the internal
`change_surface` workflow words. A topic of `showImage` must search for
`showimage`, not for `change` or `surface`. Explicit caller topics of `change`
and `surface` still search those words. The public code-topic route still uses
its caller-supplied intent when it forms search terms.

The three change-planning routes use one production code-surface backend:
change-surface investigation, pre-change impact, and developer change plan.
They retain repository authorization, the topic reader's bounded pool, limit,
offset, truncation and coverage markers, and answer truth metadata. The changed
selection is intentional, so old and new row equality is not claimed for this
one correction.

## Theory evidence and limits

A read-only candidate query on the current ops-qa PostgreSQL read replica used
the same `showImage` repository argument and a single repeatable-read snapshot.
The candidate selected only the requested topic term. All 11 returned rows
matched `showimage`; zero matched only `change` or `surface`. The complete
`EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON)` execution took 186.691 ms, and the
separate result query took 95.860 ms. The candidate pool reported no cap.
These are SQL observations, not endpoint p95 or a controlled old/new speedup.
The original SQL's separate custom plan took 991.278 ms in a different
connection and storage state, so those times are not paired comparison evidence.

No-Observability-Change: the existing query span, topic read operation,
repository scope, candidate-pool status, truncation and response truth fields
remain in place. No new metric, span, graph read or database write is added.

Performance Evidence: the bounded representative candidate above proved the
selection theory before production code changed. The built route's API and MCP
cold/warm p95 below one second remains NOT_CHECKED. The owner deploy and a
same-argument sweep are still required for issue closure.
