# #7033 fixed-corpus proof harness

This non-deployed Go command compares the current four fixed term groups with four
workers that draw one term at a time. Both routes use Eshu's unchanged probe
and page-assembly SQL. It is a read-only experiment, not an API replacement.

Run `go test ./internal/proof/scheduling -count=1` from `go/`. The fixed-corpus command
accepts `ESHU7033_MODE=fixed_canonical` for the existing assertion and timing
run, `ESHU7033_MODE=fixed_canonical_reader` for the same canonical proof on a
read-only standby, or `ESHU7033_MODE=fixed_diagnostic` for a separate
correctness diagnosis.
It reads a PostgreSQL connection
string from standard input, forces loopback TCP (or an absolute socket path),
requires an explicit database name and PostgreSQL system ID. The original two
modes reject standbys; reader mode requires a standby and a read-only session
on each of its four connections. Reader mode allows only loopback TCP, not a
socket path or fallback host. `ESHU7033_FIXED_PORT` selects a
non-default loopback port; it is valid only in fixed mode. Set
`ESHU7033_EXPECTED_DATABASE` and `ESHU7033_EXPECTED_SYSTEM_ID` from a separate
read-only preflight, not from the connection string alone. The external runner
must collect fresh database/system IDs and corpus/index fingerprints for each
run; the fixture values below do not identify an ops-qa reader.

All modes use a shared repeatable-read snapshot, a 50-second whole-case deadline,
five-second SQL statement limit, and bounded rollback and connection cleanup.
The primary canonical mode checks persisted eligibility, path-first caps, pool
agreement, and page stability before and during interleaved timing rounds.
Reader mode runs exactly three baseline-candidate-candidate-baseline rounds
without separate warmup requests. Its first baseline and candidate requests
seed the correctness witness; each request is checked before the next starts.
After both first requests pass persisted eligibility and conditional-pool
checks, reader mode emits exactly one `reader_witness_pool` line per canonical
term index (0 through 15). Each line has baseline and candidate entity/file
row counts, cap booleans, and `baseline_eligibility=pass` and
`candidate_eligibility=pass`. No term text, path, repository, or entity ID is
printed. The `dynamic_case` aggregate follows these 16 lines.
It queries standby recovery, read-only status, replay LSN, replay timestamp,
database/system identity, receive LSN, apply backlog, WAL-receiver streaming
status and message age on the exporter transaction before the first request,
between requests, and after the last. Each checkpoint has a two-second SQL
deadline and clears the PostgreSQL statistics snapshot before it reads the WAL
receiver. Reader mode requires `ESHU7033_READER_HEALTH_MODE` before connecting.
Use `strict_receiver` to require visible streaming status and a fresh receiver
message, or explicitly select `redacted_receiver` when the database role lacks
`pg_read_all_stats` and PostgreSQL redacts both fields. Both modes require
`ESHU7033_EXPECTED_RECEIVER_PID` as a positive decimal PID from independent
preflight. Every
barrier rejects a missing or changed receiver row/PID, newly visible receiver
fields, changed statistics privilege, or stale replay timestamp, even when
receive and replay LSNs match. It does not claim visible streaming status.
Reader mode requires explicit, bounded decimal ceilings in
`ESHU7033_MAX_APPLY_BACKLOG_BYTES`, `ESHU7033_MAX_REPLAY_AGE_MS`, and
`ESHU7033_MAX_RECEIVER_MESSAGE_AGE_MS`; it has no permissive defaults.
At each barrier it also loads the atomic JSON file named by the absolute
`ESHU7033_RESOURCE_GATE_FILE` path. The file must be no more than two seconds
old, declare positive resource ceilings, contain no breaches, and match the
freshly pinned `ESHU7033_READER_POD_UID` and `ESHU7033_WRITER_POD_UID`. The
watcher is external to this command; its presence and reliability require
separate verification before a live run.
When a pool reaches its cap, selected rows and their page may differ between
requests; each timed request must still match its route's first pool cardinalities,
keep uncapped pools exact, and pass the persisted eligibility check. Reusing
the same probe-row multiset with a different page fails, including across
rounds. Every timed request is checked against its route's witness, not only
the first request in a round.
The database container, host resource gates, source commit, and corpus/index
fingerprints must be verified separately. Stop and clean up any task-owned
container or claim after the run; do not delete preserved volumes.

The diagnostic mode runs baseline, candidate, candidate, baseline in one shared
read-only snapshot. Each route retains the full probe-row order and prints a
per-term/source row count, cap state, full-row multiset hash, and arrival-order
hash. It submits each exact captured payload twice to the unchanged assembly
SQL, then reports full 26-row, first-25 visible, and 26th lookahead hashes.
For the first differing rank it reports score, NULL flags and hashes of text
sort keys, plus the database collation provider and locale. It does not print
raw repository, path, or entity identifiers. This mode does not run timing
rounds or establish a performance improvement; a differing capped pool can be
legitimate, while a same-route difference needs diagnosis before optimization.

The measured duration in either canonical mode covers probe scheduling,
payload encoding, and consumption of all direct typed assembly rows after
connections and the snapshot exist. Both arms run the unchanged production
`AssemblySQL`; page hashing and correctness checks are outside the measured
duration. The earlier proof command wrapped assembly in an outer JSON SELECT,
so its timings are a different harness epoch and must not be combined with
direct-assembly timings. This change does not establish which shape is faster
or why an earlier run stopped. It does **not** establish the deployed
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
Medians were 15.899 ms baseline and 14.973 ms candidate in the earlier
JSON-wrapper epoch. Both routes returned
1,014 rows; left-only and right-only differences were zero, pages were equal,
persisted eligibility passed, and the command exited zero. The database had
no queue work. The full historical invocation argv and exact binary hash were
not retained. This small synthetic run is not a representative speedup, a
current-HEAD backend proof, or evidence that the deployed endpoint is under
one second. A fixed-corpus same-state comparison remains required before any
performance claim. This package is not deployed, so product-path latency is
unchanged by adding the harness.

Observability Evidence: both canonical modes print `dynamic_case` row counts,
differences, `dynamic_timing_round` samples, and route medians. The primary
mode also prints snapshot age; reader mode prints its declared SQL ceilings and
13 standby-health and resource-gate barriers. `fixed_diagnostic` prints probe-pool and direct-assembly hashes,
redacted rank differences, snapshot age, and `timing=not_run`; it does not
print timing samples. The historical `fixed_exit=0` was recorded by the
invoking shell, not emitted by the command. Some initial-screen failures
include phase, subject, and elapsed time; other failures report contextual
errors without all three fields.
All changes are confined to this internal proof package; no product metric,
span, log, or status contract changes.
