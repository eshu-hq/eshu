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

Benchmark Evidence: A historical, isolated PostgreSQL 18.3 fixture run checked
the harness mechanics before this package was relocated. The PostgreSQL image
was `sha256:7e32e9833a6fb1c92c32552794cb6ed569d51b445a54907d35fc112ef39684db`
with 2 CPU and 1 GiB limits, 263 entities, and 753 files. The canonical
workload used three ABBA rounds (six samples per route). Baseline round samples
in milliseconds were `15.233,16.564`; `17.451,13.141`; `17.778,12.859`.
Candidate samples were `17.239,16.268`; `14.694,15.252`; `14.451,14.196`.
Medians were 15.899 ms baseline and 14.973 ms candidate. Both routes returned
1,014 rows; left-only and right-only differences were zero, pages were equal,
persisted eligibility passed, and the command exited zero. The database had
no queue work. The full historical invocation argv and exact binary hash were
not retained. This small synthetic run is not a representative speedup, a
current-HEAD backend proof, or evidence that the deployed endpoint is under
one second. A fixed-corpus same-state comparison remains required before any
performance claim. This package is not deployed, so product-path latency is
unchanged by adding the harness.

Observability Evidence: The command prints `dynamic_case` row counts,
differences and snapshot age, `dynamic_timing_round` samples, and route
medians. The historical `fixed_exit=0` was recorded by the invoking shell,
not emitted by the command. Some initial-screen failures include phase,
subject, and elapsed time; other failures report contextual errors without
all three fields.
All changes are confined to this internal proof package; no product metric,
span, log, or status contract changes.
