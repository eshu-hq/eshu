# Reindex Progress

`GET /api/v0/admin/reindex/progress?scope_id=...` reports how far a reindex
watermark has propagated. It records nothing, claims nothing, and completes
nothing: it compares each named scope's newest activated full generation, by
its own ingest time, against its effective watermark — the later of the fleet
watermark and the scope's per-repository watermark — using the same rule the
[reconciliation sweep](../reconciliation-sweep.md#reindex-requests) decides
on. It is the read side of `POST /api/v0/admin/reindex`, which records the
watermarks this endpoint measures.

## Request

Repeat `scope_id` for 1 to 100 ingestion scope IDs (deduplicated). At least
one is required. `fleet_requested_at` optionally overrides the live fleet
watermark with an RFC 3339 timestamp, for what-if comparisons against a
watermark that was never recorded.

## Response

Each scope is `satisfied` (its newest activated full generation was ingested
at or after its effective watermark), `pending` (it has an activated full
older than that), or `never_seen` (it has no activated full at all, which
blocks completion). A re-projection acknowledgement never satisfies a scope:
it re-projects old parse output instead of storing a re-parse. `status` is
`complete` only when no named scope is `pending` or `never_seen`. There are
no pending reason sub-codes in v1.

`fleet_requested_at` echoes the fleet watermark compared — the live fleet
watermark by default, the override when given, null when neither exists.
Each scope echoes its effective `requested_at` (a per-repository row at or
before the fleet watermark is already subsumed by it, so the fleet time is
the value compared), its `last_stored_full_at` (null with no activated
full), and its `active_generation_id` (empty with no active generation).

`counts` covers every named scope. `scopes` carries at most 10 samples —
unfinished scopes first, each group by scope ID, never null — with
`truncated` set when scopes were left out.
