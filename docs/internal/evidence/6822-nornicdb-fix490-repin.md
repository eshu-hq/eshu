# NornicDB re-pin to the fix-490 self-built image (#6822)

Change: the default NornicDB backend pin moves from the Docker Hub
`timothyswt/nornicdb-cpu-bge:v1.3.3@sha256:81cedbf4...` multi-arch manifest to
the eshu-hq self-built
`ghcr.io/eshu-hq/nornicdb-amd64-cpu:fix-490-a427a468@sha256:eb69530f...`
index (amd64 child `75ab7efc...`, arm64 child `26adce57...`), built from
upstream main at the merged conjunct index-seek fix (orneryd/NornicDB#491,
which closed #490). Compose, Helm, the replay-tier/R-5 gates, the IFA
fault-injection digest assertions, the k8s governance proof fixtures, and the
operator docs all move together; prior evidence notes stay untouched as
historical records.

Why this image: the #6822 Function residual is the upstream WITH-path
full-scan root cause. Neo4j `EXPLAIN` shows an index seek where NornicDB
v1.3.3 scans, and the merged upstream fix adds the equality-conjunct seek
helper. The re-pin carries exactly that fix with no storage-format change
(the upstream diff is planner-only), so existing `nornicdb_v132_data` volumes
stay readable; the chart's fresh-claim rebuild from Postgres facts remains
the rollback path.

Benchmark Evidence: paired container probe, multi-repo cold drain: 12.4 s on
the v1.3.3 pin against 17 ms on the fix-490 image (recorded in the #6822
re-pin claim, 2026-09-21). Backend conformance
(`verify_backend_conformance_live.sh`) PASS and the probed-drain live check
PASS against the new image before the pin. The new image self-reports
`NornicDB v1.3.3` (embedded `VERSION` is still 1.3.3 at a427a468 and the
Docker build sets only `-X main.buildTime`, which never reaches
`buildinfo`), so the governance provenance gate's exact-match assertion
holds unchanged. Deploy-side Function re-measure follows the merge; until
then the 12.4 s to 17 ms paired probe plus conformance is the ship basis.

No-Observability-Change: the pin changes no signal. No span, metric, log
field, status endpoint, or pprof surface is added, removed, or renamed; the
`nornicdb retract drain completed` fields (`mode`, `probe_duration_s`,
`total_drained`, `batch`, `duration_s`) and the `canonical_probe` /
`canonical_retract_drain` backpressure labels are byte-identical. Operators
keep verifying the stack with `docker compose config --images` plus
`docker compose exec nornicdb /app/nornicdb version` as
`docs/public/run-locally/docker-compose.md` describes.
