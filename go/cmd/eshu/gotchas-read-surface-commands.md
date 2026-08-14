# eshu CLI Gotchas — Read-Surface Commands

Split out of [`README.md`](README.md) for issue #6059 so that file stays under
the repository's 500-line cap. This page carries the command contracts for
`vuln-scan repo`, `vuln-scan provider-parity`, `trace service`, and
`docs verify` — the commands that read a bounded surface, classify how complete
the evidence behind it is, and carry their own exit codes for each verdict. The
root-command invariants and the pointer back to these pages stay in `README.md`.

## Invariants

- `eshu vuln-scan repo [path]` reuses `eshu scan` root resolution, bootstrap,
  and readiness proof before reading
  `/api/v0/supply-chain/impact/findings?repository_id=<id>&limit=<n>`.
  If a service URL is configured by flag, persisted config, or environment, the
  command uses that API. Without a configured service URL, it starts or attaches
  to the workspace-local authoritative owner, launches a short-lived loopback
  `eshu-api` process with that owner's Postgres and graph env, and passes the
  same owner env to `eshu-bootstrap-index` so writes and reads use the same
  local stores.
  `--repo-id` bypasses repository selector resolution when the caller already
  knows the exact repository id. The command exits fail-closed when the scan is
  submitted, partial, failed, or cannot resolve a repository; it does not query
  findings or print a clean zero-finding state until the target is ready.
  The command is an API-backed reader and must not open graph or Postgres
  connections directly.
  The default scope mode is `scoped`: the CLI derives observed-dependency
  facts, advisory facts, package-registry facts and freshness, source-snapshot
  diagnostics, and the envelope-aggregate freshness from the readiness
  envelope. The package metadata guard fires whenever a `ready_*` response is
  backed by missing or non-fresh `package.registry` evidence; the CLI
  downgrades to `evidence_incomplete` and records
  `package_registry_metadata`. The scoped advisory guard also fires when the
  envelope's aggregate `freshness` is `stale` and the server still returned a
  `ready_*` state; in that case the CLI records `advisory_cache_stale`.
  Per-source `source_snapshots[]` entries are surfaced for visibility while
  the CLI gates on the server-owned aggregate scoped freshness verdict.
  `--broad` skips the advisory scoped guard, records a warning that the
  wider mode bypassed it, and surfaces `data.scope_mode = "broad"` so
  operators can tell the modes apart in JSON output; it still fails closed on
  stale or missing package-registry metadata. The `*_facts` fields are counts
  of source facts (the same `evidence_sources[].fact_count` the server
  reports); `package_registry_facts` counts metadata only for the requested
  package or for packages already tied to the requested repository by
  consumption evidence. When dependency facts require package-registry
  metadata and no scoped registry facts are present, the scope plan and
  performance block report `package_registry_freshness = "missing"`.
  Every run attaches a `data.scan_performance` block with started_at,
  completed_at, wall_time_ms, repository_size_bytes, repository_file_count,
  observed_dependency_facts, advisory_facts, package_registry_facts,
  package_registry_freshness, package_registry_complete, cache_freshness,
  scope_mode, and stop_threshold
  so the local one-shot scan ships its own performance evidence without a
  separate measurement step. JSON output also includes
  `data.report.schema_version = "eshu.vulnerability_report.v1"` with the
  scanner summary, readiness, freshness, unsupported targets, target/package
  context, manifest/source paths with line anchors when the API provides them,
  image/SBOM subjects, evidence handles, remediation metadata, scope plan, and
  performance block. Scoped mode treats stale or unknown aggregate freshness as
  `evidence_incomplete`. The command exits `0` for ready-zero, `3` for
  findings, `4` for non-ready evidence, `5` for unsupported target evidence,
  and `1` for runtime or transport failures before readiness is classified.
  Terminal summaries print the same exit code and reason as the JSON report,
  then show readiness, missing evidence, scope counters, and performance.
  `--export sarif` writes SARIF v2.1.0 to stdout from the same scanner report:
  reducer-owned findings become SARIF results, source paths become locations
  only when the API provided them, and run properties preserve readiness,
  missing-evidence, unsupported-target, scope-mode, and exit-code context.
  `--export vex` writes VEX-style JSON statements from the same report:
  `affected_exact` and `affected_derived` become `affected`,
  `not_affected_known_fixed` becomes `not_affected`, and
  `possibly_affected` or `unknown_impact` stay `under_investigation`.
  Non-ready scanner states such as `evidence_incomplete`, `unsupported`, and
  `readiness_unavailable` preserve readiness metadata without inventing
  `not_affected` statements. `--json` and `--export` are mutually exclusive
  output contracts.
- `eshu vuln-scan provider-parity` is the private-safe provider alert proof
  wrapper. It reads an operator-local allowlist file, optionally reads a local
  generic provider summary file, or fetches GitHub Dependabot alert summaries
  using a token from the named environment variable. It calls only the bounded
  Eshu supply-chain impact API and returns aggregate class counts in the
  provider-parity reason set: `matched`, `provider_only`, `stale`,
  `unsupported_ecosystem`, `missing_advisory_ingestion`,
  `version_matching_gap`, `target_collection_gap`, `reducer_bug`, and
  `unclassified`. The JSON output also rolls up readiness and freshness states
  across allowlisted repositories. The command must not print repository names,
  repository ids, package names, package ids, advisory ids, CVE ids, alert URLs,
  tokens, provider payloads, or Eshu finding rows. Provider lifecycle state is
  evidence, not active-impact truth: fixed/closed and dismissed/suppressed
  provider rows do not become reducer bug candidates unless Eshu has a
  conflicting row, and stale readiness evidence is treated as missing evidence
  for parity.
- `eshu trace service <name>` is a read-only CLI consumer of
  `/api/v0/services/{service_name}/story`. It asks the API for
  `application/eshu.envelope+json`, passes supported selectors through as
  `repo`, `environment`, and `service_id` query parameters, renders the service
  identity, repository, materialization status, code-to-runtime evidence
  segments, deployment-lane count, runtime-instance count, upstream/downstream
  counts, coverage, and limitations, and preserves the full canonical envelope
  with `--json`.
  Ambiguous names print the candidate service ids and exit `3`; stale or
  building truth freshness exits `4`; partial code-to-runtime traces exit `5`
  while still printing the usable evidence. The CLI must not open graph or
  Postgres connections directly for this path.
- `eshu docs verify [path]` is a local documentation-truth verifier. It scans
  Markdown-family files with `--limit` and `--max-bytes`, extracts explicit
  Eshu CLI command claims, HTTP endpoint claims, `ESHU_*` environment-variable
  claims, explicit local repo path claims, tagged or digested container image
  refs, Terraform block addresses, and known unsupported shell-command claims,
  then generates documentation finding and evidence-packet fact envelopes in
  memory. Local path and Terraform address claims are checked against the nearest
  Git worktree root or current working directory. Missing files are contradicted
  findings, and missing Terraform blocks are contradicted only when the local
  Terraform truth scan completes cleanly; invalid, oversized, or incomplete
  Terraform truth is reported as missing evidence. Without `--persist`, it does
  not open Postgres or graph connections. With
  `--persist`, it opens the shared Postgres fact-store DSN, writes a
  documentation-source scope generation, and skips re-verification when the
  current pending or active generation has the same document fingerprint while
  still returning persisted findings for `--fail-on` evaluation.
