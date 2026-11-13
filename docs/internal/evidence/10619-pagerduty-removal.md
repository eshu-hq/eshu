# #10619 PagerDuty satellite cutover: in-tree removal and activation truth note

## Scope

Lane HEAD deletes the in-tree PagerDuty producer
(`go/cmd/collector-pagerduty`, `go/internal/collector/pagerduty`) after the
satellite cutover (#8863, satellite `eshu-hq/eshu-collector-pagerduty`
v0.1.4), and repoints every in-tree pointer the merged-shape removals (#9259
loki, #9249 tempo, jira #8858-analog) repoint: Dockerfile and local-install
build lines, the binary-contract test (rewritten to the satellite posture),
the `collector_entrypoints.yaml` stanza (manifest now empty; the loader and
the `collector-entrypoints-generated` gate trigger updated for the empty
set), the surface-inventory collector row (partial lane, satellite owner;
command surface dropped on regen), the fact-kind producer record and the
`incident_routing`/`incident_context` family shards (regenerated at the
satellite repo), the extraction catalog (`external_ready`, first of its
kind), the replay manifest fault + cassette rows (report and dashboard
regenerated), the telemetry-coverage file column (satellite row), the
env-registry rows (docs regenerated), and the docs rows that named the
in-tree command or package (service page deleted with its mkdocs nav entry,
live-smoke section replaced with the satellite proof path, evidence and
extension policy proof commands repointed).

Kept by design: the coordinator `pagerduty` claim-planning path,
`scope.CollectorPagerDuty`, the `incident_routing.*`/`incident.record`/
`change.record` fact kinds and their SDK structs, the query/MCP read path
(writer and routing matrix fixtures rebuilt on the satellite's durable
payload keys: provider, provider_object_id/service_id, source_class,
source_kind, outcome, resource_class, scope_id, declared_match_state,
redaction_state), the `eshu_dp_pagerduty_*` instrument registrations and
span contracts (telemetry-coverage rows now point at the instrument home),
`testdata/cassettes/pagerduty`, and the remote-e2e `collector-pagerduty`
profile service name (satellite workload behind it).

Out of scope by evidence: Grafana and Prometheus-Mimir stay in tree. The
deployment of record runs the in-tree `eshu-collector-grafana` and
`eshu-collector-prometheus-mimir` binaries 1/1 with no satellite activation
behind either scope, so criterion 1 is unmet for those two and this lane
deletes nothing of theirs.

## Live activation truth (deployment of record, observed at this HEAD)

- No workload in any namespace runs the in-tree
  `eshu-collector-pagerduty` binary (cluster-wide command sweep).
- The `pagerduty` scope is served by the component-extension satellite
  activation running `/usr/local/bin/eshu-collector-component-extension`,
  1/1 ready. Exactly one producer holds the scope.
- The in-tree Grafana and Prometheus-Mimir Deployments are 1/1 on their
  in-tree binaries; neither scope has a component-extension activation.

## Regression proof at this HEAD

No-Regression Evidence: this change removes the in-tree producer and
repoints (not rewrites) every consumer. No runtime, queue, lease, Cypher,
or reducer path is altered: the deleted packages have no remaining
importers (`go build ./...` clean on the whole module; every test binary
in the module compiles), the read path is preserved (query, facts,
replay/inputtape, runtime, status, envregistry, extraction, cigates, and
capabilitycatalog suites green with `-count=1`; the writer and routing
matrix fixtures carry the satellite's durable keys and typecheck under
`go vet`), and corpus coverage is preserved at 455/455 satisfied with zero
uncovered or unresolved.

No-Observability-Change: no metric, span, log key, or status signal is
added, removed, or relabeled. The `eshu_dp_pagerduty_*` instrument
registrations in `go/internal/telemetry/instruments.go` and the span
contracts in `contract/thirdparty/pagerduty.go` are untouched (one comment
notes the satellite emitter); the telemetry-coverage rows now point at the
instrument home instead of the deleted package.

- `go build ./...` clean (whole module).
- `go vet` clean on all touched packages (test files included).
- Focused suites green: `internal/extraction`, `internal/cigates`,
  `internal/collector`, `internal/collector/entrypoints/...`,
  `internal/envregistry`, `internal/facts/...`,
  `internal/replay/inputtape`, `internal/query`, `internal/runtime`
  (rewritten satellite-posture test; remaining failures identical to the
  base tree), `internal/status/...`, `internal/synth/gcp`,
  `internal/telemetry`, `internal/capabilitycatalog/...`,
  `cmd/eshu`, `cmd/replay-coverage-gate`, `cmd/capability-inventory`
  (`-count=1`).
- `go run ./cmd/capability-inventory -mode verify`: no findings.
- `bash scripts/verify-fact-kind-registry.sh`: current.
- `bash scripts/verify-collector-entrypoints-generated.sh`: current
  (manifest empty; generator accepts zero manifests).
- `bash scripts/verify-telemetry-coverage.sh` (base `origin/main`): agree,
  no new untracked stages.
- Replay coverage gate (blocking): report refreshed, 455/455 surfaces
  satisfied, dashboard regenerated.
- `bash scripts/verify-moved-file-refs.sh --base origin/main`: 38 vacated
  go paths, no dangling references (dated design/census records for the
  pre-extraction paths exempted in
  `scripts/moved-file-refs-allowlist.d/go-collector-pagerduty.txt`).
- `bash scripts/verify-doc-citations.sh`: 294 test, 341 fixture, 254 raw
  line citations tracked, clean.
- Docs build (`mkdocs build --strict`): clean after the nav entry removal.

Not run here (owner's lane): `make pre-push`, `make pre-pr`, the live
Postgres story-target-support matrices (need a disposable backend; fixtures
compile and the non-live query suite passes), and the golden-corpus replay
(the pagerduty cassette is retained; its manifest coverage row is dropped
with the partial lane).
