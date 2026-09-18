# Partition Lease Expiry Bound At Commit (#6760)

No-Regression Evidence: the claim path is unchanged except for when the
expiry timestamp is resolved. Baseline (client-side expiry bound at call
time): a 3s-blocked 8s-TTL claim committed a row keeping 4.998s of TTL.
After (server-side expiry at commit): the same blocked claim keeps ~8s.
Backend: PostgreSQL 16 Alpine throwaway container for the live proof;
production CI gates exercise PostgreSQL 18. Input shape: one partition
lease claim on an empty `shared_projection_partition_leases` table blocked
3s behind a holder's `pg_advisory_xact_lock` on the domain key, TTL 8s.
Row counts: exactly one lease row committed per run, owned by the claimant,
expiry within [7s, 9s] of commit after the fix. Lock ordering (advisory CTE
before INSERT), the NOT EXISTS rescale fence, the ON CONFLICT update
predicates, and the `ClaimPartitionLease` signature are unchanged, so all
~20 existing callers (shared-projection runners, TTL/2 heartbeat renewals,
repo-dependency paths) keep their behavior; the heartbeat renew executes
unblocked and resolves the same expression. Full proof:
`TestClaimPartitionLeaseBindsTTLForServerSideExpiry` (hermetic RED/GREEN),
`TestClaimPartitionLeaseBlockedClaimKeepsFullTTLAgainstPostgres` (live
RED/GREEN), `go test ./internal/storage/postgres/...`
`./internal/reducer/intents/shared/worker/` green, EXPLAIN plan validated
(advisory CTE precedes insert, same pkey arbiter). No query-shape change
beyond the expiry expression: no new index, no predicate change, no batch
or worker-count change, so queue drain and contention behavior are
preserved; the rescale-guard query-shape test passes unmodified.

Rival-row liveness (follow-up #6765, folded in): the NOT EXISTS rescale
fence and ON CONFLICT update guard compared rival expiry against call-time
$6, so a claim blocked past a rival expiry reported a stale false negative.
Both now use clock_timestamp(). Live proof
TestClaimPartitionLeaseBlockedClaimTakesExpiredRivalAgainstPostgres (rival
expiring 1s after call, 2s block): RED claimed=false on unpatched code,
GREEN claimed=true with owner takeover after the fix. The cell TTL stays
8s: with server-side expiry the cell keeps its original sensitivity to a
client-bound revert (stale captures still fail the >4s filter).

No-Observability-Change: the change adds no metric, span, log line, status
field, or pprof surface. Claim latency remains observable through the
existing `LeaseClaimDurationSeconds` and the heartbeat-missed counter
(`eshu_dp_shared_projection_partition_heartbeat_missed_total`); blocked
claims still surface as advisory-lock waiters in `pg_stat_activity`, and
the Ifá killworker cells continue to assert the capture freshness boundary.
Operators diagnose lease contention exactly as before.
