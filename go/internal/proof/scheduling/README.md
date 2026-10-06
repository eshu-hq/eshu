# #7033 fixed-corpus proof harness

This non-deployed Go command compares the current four fixed term groups with four
workers that draw one term at a time. Both routes use Eshu's unchanged probe
and page-assembly SQL. It is a read-only experiment, not an API replacement.

Run `go test ./internal/proof/scheduling -count=1` from `go/`. The fixed-corpus command
accepts only `ESHU7033_MODE=fixed_canonical`. It reads a PostgreSQL connection
string from standard input, forces loopback TCP (or an absolute socket path),
requires an explicit database name and PostgreSQL system ID, and rejects a
standby or a session that is not read-only. `ESHU7033_FIXED_PORT` selects a
non-default loopback port; it is valid only in fixed mode. Set
`ESHU7033_EXPECTED_DATABASE` and `ESHU7033_EXPECTED_SYSTEM_ID` from a separate
read-only preflight, not from the connection string alone.

The run uses a shared repeatable-read snapshot, a 50-second case deadline,
five-second SQL statement limit, and bounded rollback and connection cleanup.
It checks persisted eligibility, path-first caps, pool agreement, and page
stability before and during interleaved timing rounds. The database container,
host resource gates, source commit, and corpus/index fingerprints must be
verified separately. Stop and clean up any task-owned container or claim after
the run; do not delete preserved volumes.

The measured duration covers warmed probe scheduling and page assembly after
connections and the snapshot exist. It does **not** establish the deployed
endpoint's one-second budget. Report raw interleaved samples, corpus/backend
identity, and any capped-page differences; never infer endpoint performance
from this harness alone.
