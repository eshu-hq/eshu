# #8858 Jira satellite cutover: in-tree removal and activation truth note

## Scope

Lane HEAD deletes the in-tree Jira producer (`go/cmd/collector-jira`,
`go/internal/collector/jira`) after the satellite cutover on the QA environment, and
repoints every in-tree pointer the merged-shape removals (#9259 loki, #9249
tempo) repoint: Dockerfile and local-install build lines, the
`collector_entrypoints.yaml` stanza, the binary-contract test, the Helm
Deployment binary-source note, envregistry rows (docs regenerated),
`facts/work_item.go` producer record (registry shards regenerated),
surface-inventory overlay row (inventory regenerated), replay manifest rows
(report and dashboard regenerated), telemetry file columns, and the docs
rows that named the in-tree command or package.

Kept by design: the `jiraCollector` chart block (disabled default) and its
Deployment/Service/NetworkPolicy/ServiceMonitor/PDB templates (single
satellite-source comment added), the `jira:` webhook listener block, the
coordinator `jira` claim-planning path, `workflow/jira_config.go`,
`scope.CollectorJira`, the `work_item.*` fact kinds and their SDK structs,
the query/MCP read path (live matrix fixtures rebuilt on the satellite's
durable payload keys), the status leaf's Jira failure classes (still pinned
against the in-tree SDK policy classes), `testdata/cassettes/jira`, and the
remote-e2e `collector-jira` profile service (satellite-source comment added).
The golden-corpus gate keeps replaying the unchanged jira cassette through
the new `jira-cassette-replay` alias (generic cassette source, byte-identical
facts); the B-12 snapshot needs no update.

## Live activation truth (the QA environment, observed 2026-10-27 ~14:50 UTC)

Instance ids below are stable placeholders (`jira-in-tree` for the retired
in-tree instance, `jira-satellite` for the activation); the mapping is held
outside the repository.

- The satellite worker Deployment (ns `eshu`): 1/1 available; the in-tree
  `deploy/eshu-jira-collector` does not exist.
- Worker log (`component-extension-collector`): scope `jira:site:boats`,
  generation `component-generation:6b39e959...`, `upsert_facts`
  `fact_count=1079`, `state=complete`, `partial=false`.
- Read replica (`fact_records`): newest `jira:site:boats` generation is
  `component-generation:6b39e959...` with 1079 live facts ingested
  2026-10-27 14:50:24 UTC; every recent generation carries
  `collector_instance_id=jira-satellite`.
- `collector_instances`: `jira-in-tree` enabled=false, claims_enabled=false;
  `jira-satellite` enabled=true, claims_enabled=true. Exactly one enabled
  producer holds the scope.
- Rendered QA values (the Jira activation test script's assertions, run
  piecemeal): single-producer check passes, activation
  scope `jira:site:boats`/`jira_site`/`jira` with the in-tree JQL and
  limits, seed image exactly
  `ghcr.io/eshu-hq/eshu-collector-jira@sha256:f96abedd...` (v0.1.0), nine
  `work_item.*` producer grants.

## Known rehearsal gaps (not in this diff)

- The QA Jira activation test script fails at its sibling-collector
  assertions: the coordinator instance list no longer carries `aws`,
  `terraform_state`, `package_registry`, `pagerduty`, or
  `vulnerability_intelligence` (those lanes cut over since the script was
  written). The Jira sections pass. The script lives in iac-eks-eshu and
  needs a sibling-cutover update there.
- The QA Jira in-tree-restore patch no longer applies to
  the current values file (eight activations now; the patch predates the
  second). The runbook itself says to regenerate it once a second
  activation exists. Rollback rehearsal past the render checks waits on
  that regeneration (iac-eks-eshu side).
- Version-pair qualification stays open: the activation runs with
  `componentExtensionCollector.coreVersion: "dev"`, which skips the
  satellite's `compatibleCore >=0.0.5 <0.2.0` range, and the newest core
  tag (`v0.0.3-pre-release-18`) cannot satisfy the template floor. Needs
  the product decision (core release >= 0.0.5 or lower template floor).

## Regression proof at this HEAD

No-Regression Evidence: this change removes the in-tree producer and
repoints (not rewrites) every consumer. No runtime, queue, lease, Cypher,
or reducer path is altered: the compose and Helm edits are comments, the
deleted packages have no remaining importers (`go build ./...`, `go vet
./...` clean on the whole module), the read path is preserved (all four
story-target-support live matrices pass against scratch Postgres on the
rewritten fixtures), and corpus coverage is preserved (the unchanged jira
cassette replays through the `jira-cassette-replay` alias with
byte-identical facts).

No-Observability-Change: no metric, span, log key, or status signal is
added, removed, or relabeled. The `eshu_dp_jira_*` instrument
registrations in `go/internal/telemetry/instruments.go` and the span
contracts in `contract/thirdparty/jira.go` are untouched; the
telemetry-coverage rows now point at the instrument home instead of the
deleted package.

- `go build ./...` and `go vet ./...` clean (whole module).
- Focused suites green: `internal/runtime`, `internal/envregistry`,
  `internal/collector`, `internal/collector/entrypoints/...`,
  `internal/facts`, `internal/replay/inputtape`, `internal/query`,
  `internal/status/collector`, `internal/telemetry`,
  `internal/capabilitycatalog/...` (`-count=1`).
- `go run ./cmd/capability-inventory -mode verify`: no findings.
- `bash scripts/verify-fact-kind-registry.sh`: current.
- `bash scripts/verify-collector-entrypoints-generated.sh`: current
  (jira stanza removed; pagerduty stanza regenerates byte-identical).
- `bash scripts/verify-replay-coverage-gate.sh` (advisory): report
  refreshed, 460/460 surfaces satisfied.
- `bash scripts/lib/test-golden-corpus-cassette-replay.sh`: PASS
  (`PASS: golden cassette replay helper`, exit 0 at this HEAD;
  mechanism unchanged; the jira cassette now rides the
  `jira-cassette-replay` alias through the pagerduty binary).
- Moved-file-refs gate: clean (dated evidence and design references to the
  pre-extraction paths allowlisted in
  `scripts/moved-file-refs-allowlist.txt`).
