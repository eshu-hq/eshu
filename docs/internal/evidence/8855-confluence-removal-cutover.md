# #8855 Confluence satellite cutover: in-tree removal and activation truth note

## Scope

Lane HEAD deletes the in-tree Confluence producer
(`go/cmd/collector-confluence`, `go/internal/collector/confluence`) after the
satellite cutover on the QA environment, and repoints every in-tree pointer
the merged-shape removals (#9271 jira, #9259 loki, #9249 tempo) repoint:
Dockerfile and local-install build lines, the Helm Deployment and Compose
service binary-source notes, the surface-inventory overlay row (inventory
regenerated), telemetry file columns (ledger re-pinned to the instrument
home), and the docs rows that named the in-tree command or package.

There is no `collector_entrypoints.yaml` stanza, no envregistry row, no replay
manifest row, and no golden-corpus cassette for Confluence, so those
generators run unchanged and stay green. The `documentation_*` fact kinds stay
core-owned (`git-document-core`, `doctruth-verifier` in
`go/internal/facts/docs/documentation.go`): no producer record or fact-kind
registry change. TruthExtractor/doctruth stays.

Kept by design: the `confluenceCollector` chart block (disabled default) and
its Deployment/Service/NetworkPolicy/ServiceMonitor templates (single
satellite-source comment added), the remote-e2e `collector-confluence` and
`collector-confluence-preflight` profile services (satellite-source comment
added), the `documentation_*` fact kinds and their SDK structs, the query/MCP
read path, the status and scope test fixtures that carry documentation scopes
as data, the shared SDK transport, and the `eshu_dp_confluence_*` instrument
registrations in `go/internal/telemetry/instruments.go`. The golden-corpus
gate never carried a Confluence cassette, so no cassette-replay alias is
needed; the B-12 snapshot needs no update.

## Live activation truth (the QA environment, observed 2026-10-27 ~18:05 UTC)

Instance ids below are stable placeholders (`confluence-satellite` for the
activation; the retired in-tree producer ran as a direct collector with no
collector instance). The scope shape is
`doc-source:confluence:<host>:<space-id>`; the mapping of host and space id
is held outside the repository.

- The satellite worker activation: Running; the in-tree
  `confluence-collector` Deployment does not exist. Exactly one enabled
  producer holds the scope.
- Worker result (first cycle, 18:02:30Z): `state=complete`,
  `fact_count=7013`, generation `component-generation:94de626f`,
  `partial=false`, all ingestion commit stages completed.
- Read replica (`fact_records`): newest scope generation is
  `component-generation:94de626f` with 7013 live facts:
  `documentation_source` 1 + `documentation_document` 554 +
  `documentation_section` 554 + `documentation_link` 5904 = 7013, parity with
  the in-tree steady state of 554 pages / 7013 facts.
- Rendered QA values (activation assertions, run piecemeal): single-producer
  check passes, digest-pinned seed image (satellite v0.1.0), four
  `documentation_*` producer grants (scope `documentation`), Secret-only
  credentials, in-tree `confluenceCollector.enabled=false` in the same
  change.

## Known rehearsal gaps (not in this diff)

- The Confluence in-tree-restore patch and rollback rehearsal per the cutover
  runbook (shipped with the activation change) are still outstanding. Rollback
  rehearsal past the render checks waits on that rehearsal.
- Version-pair qualification is covered by arbiter RE:version-pair-001 and
  #9268; no separate version-pair proof rides this diff.
- The extracted collector emits no metrics or traces of its own, so resource
  and health evidence comes from the component-extension host. The
  `eshu_dp_confluence_*` instrument registrations stay as the shared contract;
  the telemetry-coverage row now points at that instrument home.

## Regression proof at this HEAD

No-Regression Evidence: this change removes the in-tree producer and
repoints (not rewrites) every consumer. No runtime, queue, lease, Cypher,
or reducer path is altered: the compose and Helm edits are comments, the
deleted packages have no remaining importers (`go build ./...`, `go vet
./...` clean on the whole module), the read path is preserved (documentation
query/MCP/reducer suites pass on the unchanged truth paths), and corpus
coverage is preserved (the replay report refreshes only for the removed
command row; no cassette row changes).

No-Observability-Change: no metric, span, log key, or status signal is
added, removed, or relabeled. The `eshu_dp_confluence_*` instrument
registrations in `go/internal/telemetry/instruments.go` are untouched; the
telemetry-coverage row now points at the instrument home instead of the
deleted package.

- `go build ./...` and `go vet ./...` clean (whole module).
- Focused suites green: `internal/runtime`, `internal/envregistry`,
  `internal/collector`, `internal/collector/entrypoints/...`,
  `internal/facts`, `internal/replay/inputtape`, `internal/query`,
  `internal/status/collector`, `internal/telemetry`,
  `internal/capabilitycatalog/...` (`-count=1`).
- `go run ./cmd/capability-inventory -mode verify`: no findings.
- `bash scripts/verify-fact-kind-registry.sh`: current.
- `bash scripts/verify-collector-entrypoints-generated.sh`: current (no
  confluence stanza; manifest regenerates byte-identical).
- `bash scripts/verify-replay-coverage-gate.sh` (advisory): report
  refreshed, all surfaces satisfied.
- `go run ./cmd/skillgen gen`: clean (no confluence baseline rows remain).
- Moved-file-refs gate: clean (dated design/evidence records quoting the
  pre-extraction tree allowlisted in
  `scripts/moved-file-refs-allowlist.txt`).
