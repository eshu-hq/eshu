# NornicDB re-pin to the fix-499 self-built image (#6162)

Change: the default NornicDB backend pin moves from the eshu-hq self-built
`ghcr.io/eshu-hq/nornicdb-amd64-cpu:fix-490-a427a468@sha256:eb69530f...`
index to the eshu-hq self-built
`ghcr.io/eshu-hq/nornicdb-amd64-cpu:fix-499-6ac958a9@sha256:fc90a2c3115d5dc0fe9a69ac676e5c77428bcfdcadc3320f2e99f887bea22f26`
index (amd64 child `a1fc8d72...`, arm64 child `24ac01cb...`), built from upstream main
at the merged Close-versus-commit fix (orneryd/NornicDB#501, commit
`6ac958a9`), which also carries the numID counter floor (orneryd/NornicDB#498,
commit `3f997e04`). Compose, Helm, the replay-tier/R-5 gates, the IFA
fault-injection digest assertions, the k8s governance proof fixtures, the
live-test docker-run pins, and the operator docs move together; prior
evidence notes stay untouched as historical records.

Why this image: it carries the two upstream fixes for the
`restart-backend-between-phase-groups` digest mismatch tracked in #6162;
whether the cell stays green on this pin is what CI observes next, and #6162
stays open until it does. On the fix-490 image a backend restart that landed
on an in-flight explicit commit left the commit durable but skipped the
post-commit tail (the Bolt session logged `transaction commit panicked: nil
pointer dereference`), so the numID counter high-water mark was never
persisted; on the next open the dictionary reissued live numIDs to the next
tenant's nodes and every numID-keyed index (adjacency, label, edge-between,
MVCC heads) merged the two tenants' k-th nodes. The graph dump then returned
each GCP relationship a second time under the other tenant's node (63 -> 126
edges per affected scope, node set unchanged), which is exactly the artifact
from run 35780108812. Upstream #498 floors the counters at the durable forward
map on open, and #501 makes `Close` wait for in-flight durable writes so the
tail is never torn and a durable commit is never reported as failed. Both are
storage-engine changes with no storage-format change, so existing
`nornicdb_v132_data` volumes stay readable; the chart's fresh-claim rebuild
from Postgres facts remains the rollback path.

Root-Cause Evidence: run 35780108812 artifact
`ifa-fault-injection-shard-4-attempt-1-failure`: `compose-services.log`
carries the `explicit transaction commit failed ... transaction commit
panicked` and `connection handler panic` lines at 20:26:52.619Z, the reducer
log shows both affected relationship intents ran once with `edge_count=63
resolved_count=63 cross_scope_endpoint_count=0`, and resolving the dump's
endpoint digests against the node list gives 63 extra edges per affected
scope whose source is the other tenant's k-th node with in-degree 2 on every
target. The NornicDB source chain (persistCounters after badgerTx.Commit,
loadFromBadger trusting the counter key, releaseClosedStateLocked nil-ing
b.db) is recorded on #6162 and reproduced by
`pkg/storage/id_dictionary_counter_reopen_test.go` and
`pkg/storage/badger_close_commit_race_test.go` upstream.

No-Regression Evidence: `scripts/verify-ifa-fault-injection.sh --shard 4/4`
against an amd64 image built from the #498 tree
(`nornicdb-amd64-cpu:idcounter-local`) on Eshu `97a9cbcf14`: all 12 cells
green, `restart-backend-between-phase-groups` non-vacuous (sentinel fired,
NornicDB restarted mid-drain), digest
`21b3b3553a03140cb92e1edfcc838383638e800e60f87e8982c072abdae953d4` equal to
baseline and to the CI artifact's baseline. Then twice more on the exact
pinned artifact as the compose default from this branch (no image override):
the restart cell was non-vacuous and digest-equal to baseline in both runs.
The first run failed later in `killworker_handles_route` (runner-lease
reclaim assertion, observed `4|3|4` against expected `4|4|4`) while an
independent reviewer's test suites shared the host; the second run, with the
host quiet, passed all 12 cells (`PASS: fault-injection matrix green`,
`killworker_handles_route` digest `063ee937…` wall 16 s). One failure in two
samples of a timing-sensitive Postgres lease cell is recorded, not explained
away; it is not on the NornicDB path this pin changes. The re-pinned image self-reports
`NornicDB v1.3.3` (embedded `VERSION` unchanged at 6ac958a9), so the governance
provenance gate's exact-match assertion holds unchanged. The fixes touch only
the engine open path, `Close`, and the write barrier; no query shape or
projection path changes, so no drain or read timing is re-measured here.

No-Observability-Change: the pin changes no signal. No span, metric, log
field, status endpoint, or pprof surface is added, removed, or renamed.
Operators keep verifying the stack with `docker compose config --images` plus
`docker compose exec nornicdb /app/nornicdb version` as
`docs/public/run-locally/docker-compose.md` describes.
