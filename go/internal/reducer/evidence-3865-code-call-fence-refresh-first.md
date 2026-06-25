# Evidence: code-call fence refresh-first ordering (#3865)

The code-call projection DB fences (`codeCallProjectionWholeFenceSQL` and the
non-file branch of `codeCallProjectionFileFenceSQL` in
`go/internal/storage/postgres/shared_intents_history.go`) ranked candidate
intents by raw `(created_at, intent_id)`, while the batch query
(`shared_intents_partition_candidates.go`, #3451/#3474) and the in-memory fence
(`code_call_projection_selection.go`) rank them `is_refresh_intent`-first. When a
repository's whole/file repo-refresh intent and an older-created edge intent for
the same repo landed in the same projection partition, the DB fence blocked the
refresh behind the edge while the in-memory fence blocked the edge behind the
refresh — a mutual deadlock that held the repo's `code_calls`
`shared_projection_intents` non-terminal at pipeline terminal (the B-7 gate #3800
surfaced it; tracked as #3865). The fix ranks both DB fences
`is_refresh_intent`-first so a refresh is never fenced behind its own edges.

No-Regression Evidence: pre-fix,
`ESHU_CODE_CALL_FENCE_PROOF_DSN=<pg> go test ./internal/storage/postgres -run
'TestCodeCall(Whole|File)FenceRefreshNotBlockedByOlderEdgeIntegration' -count=1`
fails — the refresh is fenced behind its older edge (the #3865 deadlock) on both
the whole and file lanes; post-fix both pass (refresh not blocked AND the edge is
still fenced behind the refresh, so retract-before-write is preserved). The
fake-DB unit tests `TestSharedIntentStoreCodeCallWholeFenceRanksRefreshFirst` and
`TestSharedIntentStoreCodeCallFileFenceRanksRefreshFirst` assert both fence SQLs
encode the refresh-first guard so a revert is caught in CI. End-to-end, the B-7
golden-corpus gate now requires `code_calls` to drain (the advisory quarantine is
removed) and a full local run drains it to zero
(`shared_projection_intents_nonterminal: required-nonterminal=0 ... total=0`);
with the fixed reducer the previously-held intents drained within 10s versus
staying stuck for 4+ minutes before.

No-Observability-Change: the fix only changes the ORDER BY-equivalent ranking
expression inside two existing fence selection queries (it adds an
`is_refresh_intent` precedence term that mirrors the existing batch ordering). It
adds no graph query, graph write shape, queue table, worker, lease, batch
setting, runtime knob, metric instrument, metric label, span, route, status
field, or log key. The fence still gates only intent selection ordering; retract
and edge writes flow through the unchanged `CodeCallProjectionRunner`
processing path, and operators continue to diagnose code-call projection through
the existing `code call projection skipped acceptance units ...` log fields and
shared-projection telemetry.
