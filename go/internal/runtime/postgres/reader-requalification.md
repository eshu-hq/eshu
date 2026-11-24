# Reader member requalification

A direct reader member (`ESHU_POSTGRES_READ_MEMBERS`) pins its postmaster
incarnation and resolved addresses at bootstrap. A standby restart changes
both, so the next borrow failed the pinning check and excluded the member on
every call until the process restarted: `/readyz` and `/metrics` stayed
failing with `PostgreSQL reader connection unavailable` while the replica
itself was healthy (restartpoints progressing, replay current, endpoints
present, no authentication failures server-side).

The fleet now requalifies a member once per call when it reports a
member-local topology mismatch: it re-reads the identity over a throwaway
connection and republishes the pinning only after the member proves the
lined-up system identifier, database, and standby role. An unverifiable
member stays excluded exactly as before, and a foreign cluster can never be
published. No borrow, checkpoint, identity, or replay statement changed.

No-Regression Evidence: hermetic `go test ./internal/runtime/postgres/
-count=1` (plus `-race`) green on the final commit, including
`TestFleetRequalifiesRestartedMember` (stale member with a verifiable
re-read: the borrow succeeds, the pinning refreshes stale to fresh, exactly
one requalification read), `TestFleetRequalifyRefusedStaysUnavailable` and
`TestFleetRequalifyRunsOncePerMember` (refused and persistently stale members
stay excluded with the member-local topology marker, one read at most), and
`TestPublishRequalifiedIdentityVerifiesCluster` (foreign cluster, unchanged
read, and out-of-range member refused). Backend: stub pools, no live
database. A live standby-restart recovery run is NOT_CHECKED here; in
production it is observed through the log line below plus
`eshu_dp_postgres_reader_member_attempts_total` returning to `ok` and
`/readyz` returning 200 after the next reader restart.

Scope: requalification covers fenced borrows through runFleet. The
stale-reader path (runStaleFleet) still excludes memberLocalTopology
members permanently; that behavior is unchanged, not a regression.

Observability Evidence: a successful refresh logs
`event_name=postgres.reader.member_requalified` with `member_ordinal`,
`member_id`, and the new incarnation. Requalification failures stay silent
and keep the existing `member_attempts_total{outcome="error"}` and
`reader_borrow{outcome="error"}` stage signals. No metric, span, or label
was added or renamed.
