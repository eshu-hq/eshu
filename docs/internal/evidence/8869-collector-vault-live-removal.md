# #8869 in-tree vault-live removal (proposal)

Base: `origin/main` at `73418463ed` (rebased from `d363d77b85`; the
rebase takes upstream deletions of the in-tree deploy lanes and the
live-test ledger row for #9474, and drops the lockstep reader entry
whose only test reader was the deleted census test). This record is
the tracked
performance/no-regression/observability note for the mechanical removal
of the superseded in-tree vault-live collector. It authorizes no merge:
the removal PR stays a proposal until recorded production acceptance
lands on #8869.

## Scope

Deletes the in-tree binary (`go/cmd/collector-vault-live`), source
packages (`go/internal/collector/vaultlive`), coordinator planner
(`go/internal/coordinator/vault/live`, `vault_live_service.go`,
scheduler seam), and chart surface. Kind-level contracts, posture
envelope builders, scope kinds, fact registries, cassettes, and the
claim-latency envelope row stay: the satellite
(`eshu-hq/eshu-collector-vault-live` v0.1.1) serves the same scopes.

## Performance Evidence

Baseline: `origin/main d363d77b85`. Removal deletes in-tree hot code
and adds none: no new Cypher, queue, lease, batching, or concurrency
knob. Live QA cadence is unchanged by the removal (satellite path):
collections completed 04:22:30Z, 04:52:34Z, and 05:22:35Z on the ~30m
plan window with source latency 36-41ms, fact_count 13, zero warnings.
Claim-latency envelope for `vault_live` (per-Vault snapshot, bounded)
still describes the satellite path.

## No-Regression Evidence

`go vet ./...` clean on the removal tree. Focused proof (each green):
`./internal/coordinator/...`, `./cmd/workflow-coordinator`,
`./internal/runtime` (Helm render contracts), `./internal/telemetry`,
`./internal/envregistry`, `./internal/cigates` (lockstep + ledger),
`./internal/workflow/...`, `./internal/scope/...`,
`./internal/status/collector/...`,
`./internal/collector/access/posture/...`,
`./cmd/capability-inventory/...` (regenerated artifacts committed).
Registry-selected static gates: 280 PASS, 0 FAIL (see gate log;
rebased head `a50bc9ba8d`, rg 15.2.0 floor toolchain, disk-backed
TMPDIR, `ESHU_DEPLOY_CHECKOUT` set for the deploy-contract tests).

## Observability Evidence

In-tree source-side counters (`eshu_dp_secrets_iam_source_*`,
`eshu_dp_vault_request_total`) are removed: their only emitter was the
deleted source package, and the telemetry verifier flagged them dead.
Reducer/graph signals stay (`eshu_dp_secrets_iam_graph_nodes_written_total`,
`vault_live.snapshot` span, redaction field-class dimension). The
satellite emits its own copies under the same OTLP names; live worker
OTEL (grants 03:51:30Z, collections above) is the operator signal.

No-Observability-Change: this removal adds no metric, span, log, status
field, or dashboard label in-tree.
