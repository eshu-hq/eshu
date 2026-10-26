# Extraction PREP Census Lock — collector-security-alerts (#8866)

Bounded preparation inventory for extracting `collector-security-alerts` to
`eshu-hq/eshu-collector-security-alerts`. This file locks the current in-tree
census; it authorizes no move, no deployment, and no in-tree deletion.
Cutover stays gated by #4047 with production acceptance NOT_CHECKED.

Scope of this PREP: `go/cmd/collector-security-alerts/`,
`go/internal/collector/securityalerts/` (including `alertruntime/`).
Out of scope: `sdk/go` (Codex #8851), all other collectors, org repos,
deployment.

## 1. Command surface

- `go/cmd/collector-security-alerts/main.go` (`parseArgs`, `run`): `--version`
  flag handling, `--preflight-provider-access` credential check, `-mode
  claimed-live|cassette` (`-cassette-file` required in cassette mode),
  then `run()`.
- `go/cmd/collector-security-alerts/service.go` (`buildClaimedService`):
  loads claimed runtime config, wires `alertruntime.NewClaimedSource`,
  commits through `postgres.NewIngestionStore` with the shared workflow
  control store and generation dead-letter sink, returns
  `collector.ClaimedService` with `CollectorKind:
  scope.CollectorSecurityAlert` (`"security_alert"`), claim lease TTL,
  heartbeat interval, `MaxAttempts`,
  `DefaultClaimMaxAttempts()`. Claim-aware only: no direct fact writes, no
  graph writes. `buildCassetteService` wires the credential-free cassette
  replay path (`cassette.NewSource`); no live credentials needed.
  `attachReadState` hands each org claim its Postgres read-state writer
  and fan-out cursor reader together (#8943/#8946): a writer without the
  reader would commit a cursor no claim reads. `newClaimID` mints
  `security-alert-claim-` identities with a monotonic fallback.
- `go/cmd/collector-security-alerts/config.go`
  (`loadClaimedRuntimeConfig`): parses `ESHU_COLLECTOR_INSTANCES_JSON`,
  selects the single enabled claim-driven `security_alert` instance
  (ambiguous multi-instance without
  `ESHU_SECURITY_ALERT_COLLECTOR_INSTANCE_ID` fails closed), maps
  targets/derived targets, resolves poll / lease / heartbeat durations,
  rejects heartbeat >= lease.
- `config.go` credential handling (`mapTarget`, `resolveCredentialEnv`):
  `token_env` (token-env auth mode) or `github_app_id_env` /
  `github_app_installation_id_env` / `github_app_private_key_env`
  (GitHub-App auth mode) resolve from the environment at startup into a
  runtime-only `TargetConfig` field. The two modes are mutually exclusive
  and a half-configured shape fails closed. Credentials are never copied
  into desired collector config output, facts, logs, metric labels, or
  docs (see `config_test.go:
  TestLoadClaimedRuntimeConfigRejectsMissingGitHubToken` and
  `extraction_prep_mirror_test.go:
  TestExtractionPrepClaimBoundaryRequiresClaimCapableInstance`).
- `go/cmd/collector-security-alerts/preflight.go`
  (`--preflight-provider-access`): credential-free-shape provider access
  check with bounded redacted errors; no facts emitted.

## 2. Collector libraries

`go/internal/collector/securityalerts/`:

| File(s) | Role |
|---|---|
| `envelope.go` | Single envelope constructor `NewGitHubDependabotAlertEnvelope`: stable keys, generation binding, contract-payload merge via `sdk/go/factschema/securityalert/v1`, URL sanitization (`sanitizeURL`) |
| `types.go` | `CollectorKind`, `EnvelopeContext`, GitHub Dependabot alert/advisory/vulnerability DTOs |
| `github_client.go` (+ `github_client_app_auth.go`, `github_client_error_body.go`) | Bounded Dependabot REST reads (repo + org endpoints, pagination, `repository_alert_limit` / `max_pages`); explicit repository allowlist before any request; `Authorization: Bearer` header transport, never a query parameter; per-call installation tokens, never cached; bounded redacted errors |
| `encode_helpers.go` | Contract-payload merge + pointer helpers shared by the envelope constructor |
| `read_state.go` | `TargetReadState` / `FanoutCursor` shapes for org generation read state |
| `credential_canary_test.go`, `dependabot_envelope_test.go` | Sentinel sweep (no over-redaction) + repository-alert field preservation |
| `doc.go` | Godoc package contract (source facts only, never canonical impact truth) |

`go/internal/collector/securityalerts/alertruntime/`:

| File(s) | Role |
|---|---|
| `source.go` | `ClaimedSource` / `NewClaimedSource`: claim-to-fact fan-out, shard partition of large org targets (#9019), retirement items, disabled targets skipped before client construction |
| `envelopes.go` | Per-claim envelope assembly: org fan-out allowlist filter, per-repo scope derivation, coverage annotation; refuses unsupported fact kinds |
| `target.go` | `TargetConfig` validation, auth-mode normalization, allowlist matching |
| `coverage.go` / `freshness.go` | Collection coverage + source-freshness hints carried on each envelope |
| `failure.go` | `ProviderFailure` classification (rate-limited, terminal, operation token) |
| `read_state.go` / `fanout_cursor.go` / `retire.go` | Generation-scoped org read state, fan-out position, retirement answers |
| `source_uri.go` | `safeSourceURI`: userinfo + sensitive-query stripping for source URIs |
| `metrics.go` | Bounded emission: provider requests, fetch duration, facts by kind, rate limits, truncated reads, org fan-out outcomes |
| `doc.go` | Godoc runtime contract (claim-driven, shard scopes, retirement) |

## 3. Durable source state (DDL + owner)

- No dedicated alert-fact table is owned here: alert facts commit through
  the shared ingestion path (`NewIngestionStore`). Collector-scoped
  durable state is the org read path: `230_security_alert_target_read_states`
  (per-generation read state + unread repositories, #8943),
  `232_security_alert_fanout_cursor` (fan-out position columns, #8946),
  `220_fact_records_security_alert_cve_id_idx` (CVE lookup index), and
  `233_workflow_work_items_security_alert_scope_updated_idx` (claim
  scheduling index). All generation-scoped rows cascade from
  `scope_generations`, so retention removes them with the generation.
  Durable claim/lease state lives in the shared workflow control store
  (`postgres.NewWorkflowControlStore`); admission and status reads stay
  with core (`postgres.NewIngestionStore`, instrumented status store).
- Ownership split: the `securityalerts` package owns target validation,
  normalization, envelope construction, and source-local failure
  classification only. Post-extraction the extracted collector keeps
  that source ownership via the core-issued grant path; core keeps
  admission, projection, persistence, claim mutation, fencing, and the
  status read.

## 4. Fact kinds + versions

One emitted kind lives in `go/internal/facts/security_alert.go` at
`SecurityAlertSchemaVersionV1 = "1.0.0"`:

1. `security_alert.repository_alert` (repository-scoped provider security
   alert evidence: provider alert id/number/state, dependency coordinates,
   advisory GHSA/CVE ids, vulnerable range, patched version, severity,
   CVSS, EPSS, CWE, timestamps, source URL, correlation anchors)

Family: live GitHub Dependabot metadata, `source_class` reported via
`SourceConfidenceReported`, `source_kind=security_alert`; lifecycle owner
`go/internal/facts`; reducer domain `security_alert_reconciliation`;
admission hook `facts.ValidateSchemaVersion`. Producer split:
`collector-security-alerts` (core-issued grant, the observed kind) vs
declared/GitOps evidence (core-owned, preferred when current; live facts
are fallback/validation). Planned repository:
`eshu-collector-security-alerts`, authority mode `core_issued_grant` (see
`producer-grant.manifest.DRAFT.yaml`).

## 5. Consumers + protocols

- Reducer: `go/internal/reducer/securityalert/` binds observed envelopes
  into reconciliation truth (`BuildSecurityAlertReconciliations`,
  `SecurityAlertReconciliationHandler`, Postgres writer); provider alert
  state is never promoted to impact truth there. Declared/applied/observed
  comparison stays in the reducer.
- Coordinator: `go/internal/coordinator/security_alert_service.go` +
  `security/alert/planner.go` plan `security_alert` workflow rows from
  desired instances; no collection logic there.
- Query surfaces read projected truth, never this package directly: HTTP
  `GET /api/v0/supply-chain/security-alerts/reconciliations` and the MCP
  security-alert read tools.
- Protocols: GitHub Dependabot REST (`Authorization: Bearer` header, never
  query parameter); facts schema `1.0.0` via
  `sdk/go/factschema/securityalert/v1` (physical relocation under Codex
  #8851 is a prerequisite, not this PREP). Supported oldest/newest core
  pairs: NOT_CHECKED until the first extracted release
  (`version-matrix.sh`).

## 6. Migration stage + prerequisites

- Stage: in-tree preparation (this PREP). Destination repository
  `eshu-hq/eshu-collector-security-alerts` exists and is empty; no
  standalone release; no deployment.
- Prerequisites to reuse: #6708 SDK publication, #6709 producer
  delegation, #6710 standalone template, #4047 gate implementation.
- Blockers: #8851 physical SDK relocation (Codex-owned; this drive
  consumes its published modules only), owner-designated QA EKS cutover
  rehearsal, independent review + stable exact-head CI at promotion.
- Mirror proof: `extraction_prep_mirror_test.go` in both packages
  (admission at 1.0.0, determinism, generation rebind, URL-sanitization
  sentinel, fail-closed hostile inputs, claim boundary, heartbeat guard,
  claim-ID namespace).
