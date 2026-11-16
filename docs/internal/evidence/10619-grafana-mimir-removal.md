# #10619 Grafana / Prometheus-Mimir satellite cutover: in-tree removal and activation truth note

## Scope

Lane HEAD deletes the in-tree Grafana and Prometheus-Mimir producers
(`go/cmd/collector-grafana`, `go/cmd/collector-prometheus-mimir`,
`go/internal/collector/observability/grafana`,
`go/internal/collector/observability/prometheus`) after the satellite
cutovers (#8857 grafana, satellite `eshu-hq/eshu-collector-grafana`; #8864
prometheus-mimir, satellite `eshu-hq/eshu-collector-prometheus-mimir`), and
repoints every in-tree pointer the merged-shape removals (#9259 loki, #9249
tempo, #10661 pagerduty, eshu-hq/eshu#10620) repoint: Dockerfile and
local-install build lines, the binary-contract test (rewritten to the
satellite posture), the surface-inventory collector rows (partial lane,
satellite owner), the fact-kind producer records and the observability family
shards (satellite repository, path `.`), the extraction catalog (both
families `external_ready`, third and fourth of their kind), the replay
manifest fault + cassette rows (report and dashboard regenerated), the
telemetry-coverage family rows (satellite rows), the env-registry rows (docs
regenerated), and the docs rows that named the in-tree commands or packages
(service-runtimes satellite rows, environment-collectors satellite notes,
reducer-readiness satellite wording, planner README ownership, SDK README
runnable commands, synth parity-test precedent pointer).

Kept by design: the coordinator `grafana` / `prometheus_mimir`
claim-planning paths (`go/internal/coordinator/planner/grafana`,
`go/internal/coordinator/planner/prometheus`, `grafana_service.go`,
`prometheus_mimir_service.go`), `scope.CollectorGrafana` /
`scope.CollectorPrometheusMimir`, the `observability.*` fact kinds and their
SDK structs, the query/MCP read path, the `eshu_dp_grafana_*` /
`eshu_dp_prometheus_mimir_*` instrument registrations and span contracts
(telemetry-coverage rows now point at the instrument home), the
`testdata/cassettes/grafana` and `testdata/cassettes/prometheusmimir`
cassettes (still replayed by the golden-corpus gate through the
`collector-cicd-run` vehicle aliases), the `mock-prometheus-mimir` metrics
source (serves the API trend-chart read path, not the retired collector),
the `ESHU_PROMETHEUS_MIMIR_COLLECTOR_INSTANCE_ID` API trend-source selector
(`go/cmd/api/metrics_source.go` still reads it; re-homed to the `api`
registry subsystem with `cmd/api/metrics_source.go` added to the registry
coverage scan), and the remote-e2e `collector-grafana` /
`collector-prometheus-mimir` profile service names (satellite workloads
behind them).

Out of scope by evidence: the in-tree Deployments
(`eshu-grafana-collector`, `eshu-prometheus-mimir-collector`) live in the
`iac-eks-eshu` overlay, not in this repo; pruning them (selective sync, per
the mimir cutover lanes) and revoking the in-tree grants ride the cutover
lanes and gate this PR's merge (see the merge gate in the PR body).

## Live activation truth (deployment of record, as recorded on #10619)

- Mimir is LIVE with facts through the component-extension satellite
  activation (post-#693: first claim `fact_count: 1`, empty failure_class;
  #10619 comment 2026-11-16T01:30:57Z).
- Grafana goes live under the parallel Lane C overlay; this lane does not
  duplicate its proof.
- No RETIRE-GO line is present on #10619 at PR open: this PR stays open
  until the coordinator posts it.

## Regression proof at this HEAD

No-Regression Evidence: this change removes the in-tree producers and
repoints (not rewrites) every consumer. No runtime, queue, lease, Cypher,
or reducer path is altered: the deleted packages have no remaining
importers (`go build ./...` clean on the whole module; every test binary
in the module compiles), the read path is preserved (query, facts,
replay/inputtape, runtime, status, envregistry, extraction, cigates, and
capabilitycatalog suites green with `-count=1`), and corpus coverage is
preserved at 452/452 satisfied with zero uncovered or unresolved.

No-Observability-Change: no metric, span, log key, or status signal is
added, removed, or relabeled. The `eshu_dp_grafana_*` /
`eshu_dp_prometheus_mimir_*` instrument registrations in
`go/internal/telemetry/instruments.go` and the span contracts in
`contract/observability/` are untouched (comments note the satellite
emitters); the telemetry-coverage rows now point at the instrument home
instead of the deleted packages.

- `go build ./...` clean (whole module).
- `go vet` clean on all touched packages (test files included).
- Focused suites green: `internal/extraction`, `internal/cigates`
  (lockstep), `internal/collector`, `internal/envregistry`,
  `internal/facts/...`, `internal/replay/inputtape`, `internal/query`,
  `internal/runtime` (rewritten satellite-posture test; remaining failures
  are the pre-existing `ESHU_DEPLOY_CHECKOUT`-gated helm/compose tests,
  identical to the base tree), `internal/status/...`,
  `internal/synth/gcp`, `internal/telemetry`,
  `internal/capabilitycatalog/...`, `cmd/eshu`, `cmd/replay-coverage-gate`,
  `cmd/capability-inventory`, `cmd/golden-corpus-gate` (`-count=1`).
- `go run ./cmd/capability-inventory -mode verify`: no findings.
- `bash scripts/verify-fact-kind-registry.sh`: current.
- `bash scripts/verify-collector-entrypoints-generated.sh`: current
  (manifest already empty since the pagerduty removal).
- `bash scripts/verify-telemetry-coverage.sh`: agree, no new untracked stages.
- Replay coverage gate (blocking): report refreshed, 452/452 surfaces
  satisfied, dashboard regenerated.
- `bash scripts/verify-moved-file-refs.sh --base origin/main`: vacated
  go paths with dated-record allowlist fragments
  (`scripts/moved-file-refs-allowlist.d/go-collector-grafana.txt`,
  `scripts/moved-file-refs-allowlist.d/go-collector-prometheus-mimir.txt`).
- `bash scripts/verify-doc-citations.sh`: tracked citations clean.
- `bash scripts/verify-replay-coverage-gate.sh` (advisory): PASS.
- Golden-corpus self-tests green:
  `scripts/lib/test-golden-corpus-cassette-replay.sh`,
  `scripts/test-verify-golden-corpus-gate.sh` static requires
  (vehicle switched to `collector-cicd-run`; grafana + prometheusmimir
  cassettes ride cicd-run aliases).

Not run here (owner's lane): `make pre-push`, `make pre-pr`, the live
Postgres story-target-support matrices (need a disposable backend), the
golden-corpus replay (cassettes retained and re-vehicled; full replay is a
CI leg), and the docs build.

## Retired-coverage honesty note

Tier-1 plus satellite conformance is NOT equivalent to the retired
coverage. Retired with this lane:

- `TestPrometheusMimirRecordReplayProducesIdenticalFacts`
  (`go/internal/replay/inputtape/acceptance_test.go`, deleted with the
  file): record/replay byte-identity through a real collector client. The
  inputtape machinery stays covered by the roundtripper suite and the AWS
  SDK record/replay demo; collector-shaped parity for these two families
  now lives in the satellites' conformance suites plus the live cutover
  records on #10619.
- The `collector:grafana` / `collector:prometheus_mimir` fault-probe cases
  and their manifest rows: no in-tree HTTP client remains to fault-probe;
  boundary-fault behavior for these families is satellite-owned now.
- The `collector:grafana` / `collector:prometheus_mimir` manifest cassette
  rows: the cassette files stay and still replay in the golden-corpus gate
  via the `collector-cicd-run` vehicle aliases, but the in-tree proof lane
  is unmet by construction (surface inventory `partial`).

A gap that is accepted is recorded; a gap that is silent is a defect.
