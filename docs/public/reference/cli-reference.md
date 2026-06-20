# CLI Reference

Use this page as the short map for the public `eshu` CLI. For exact flags and
arguments, prefer the task pages linked below or run command help from the same
binary you are using.

```bash
eshu help
```

## Command Model

The `eshu` binary has three public command shapes:

- local commands start or attach to local Eshu runtimes
- API-backed commands call the HTTP API
- compatibility commands stay visible and return replacement guidance

CLI read commands use the HTTP API, not MCP. MCP is the assistant and IDE
integration surface.

## Root Flags

| Flag | Scope |
| --- | --- |
| `--database` | Temporarily sets `ESHU_RUNTIME_DB_TYPE` for this process. |
| `--visual`, `-V` | Requests visual output on supported local paths. |
| `--version`, `-v` | Prints the installed CLI version. |
| `--help`, `-h` | Prints help. |

`--workspace-root` is command-local on scan, watch, graph, MCP, and
workspace-watch paths. Release and installer builds report the injected version;
plain local source builds without a version override report `dev`.

## Command Families

| Family | Starts with | Use |
| --- | --- | --- |
| Local setup and runtime | `eshu graph`, `eshu mcp`, `eshu api`, `eshu serve`, `eshu install nornicdb` | [Local binaries](../run-locally/local-binaries.md), [Graph Backend Operations](graph-backend-operations.md), [Service Runtimes](../deployment/service-runtimes.md), and [MCP Guide](../guides/mcp-guide.md) |
| Guided onboarding | `eshu first-run`, `eshu first-run report` | Walks the smallest truthful path to one indexed repository, one readiness proof, and one bounded API answer. Detects the runtime shape (reachable API, local binaries, or Docker Compose), verifies it without destructive auto-start, waits for indexing completeness rather than process health, and reports success only when a bounded query returns. Use `--json` for the canonical envelope and `--no-start` for verify-only mode. Add `--report` for a redacted evidence summary, `--report-out <path>` (with `--report-format md\|json`) to write a redacted onboarding artifact, or `eshu first-run report --from <envelope>` to regenerate the artifact offline. See [First-Run Evidence](first-run-evidence.md). |
| Operator digest | `eshu report` | Renders a deterministic `operator_digest.v1` model for an explicit share-safe scope such as `repo:owner/name` or `service:name`. This CLI path is offline: it validates the model, emits unsupported sections and suggested follow-up routes, and does not call providers, write graph state, claim reducer work, or infer graph truth. Use `--json` for the contract model and `--artifact-out <path>` for a shareable `operator_digest_artifact.v1` JSON handoff. See [Operator Digest Contract](operator-digest.md). |
| Investigation evidence packets | `eshu investigation export` | Emits a portable `investigation_evidence_packet.v2` artifact (`--format json\|md\|html`, `--out <path>`) for a `--family` investigation scoped by repeatable `--subject key=value` keys. Families: `supply_chain_impact` (supply-chain explain route), `deployable_unit` (admission-decisions route, with accepted/ambiguous/rejected candidates explicit), and `drift` (cloud runtime-drift route). Each packet separates raw source facts, reducer decisions, graph/query truth, missing-evidence reasons, freshness, reproduce handles, and optional semantic observations. It is deterministic with no provider keys; an unknown family or unanswerable scope yields a valid refusal packet. See [Investigation Evidence Packet Contract](investigation-evidence-packet.md). |
| Onboarding benchmark | `eshu first-run-benchmark` | Scores an `eshu first-run --json` envelope against the first-five-minutes onboarding success criteria. Exits non-zero (rejects the run) when the "first answer" is health-only: no bounded query returned, missing truth metadata, missing source handle, incomplete indexing, or an error envelope. See [First five minutes benchmark](local-testing/first-five-minutes-benchmark.md). |
| Answer quality dogfood | `eshu answer-quality-scorecard` | Scores a redacted answer-quality evidence artifact across API, MCP, CLI, and hosted surfaces. Exits non-zero when family coverage, usefulness, truth honesty, citation coverage, boundedness, parity, follow-up usefulness, or publish safety fails. See [Answer Quality Scorecard](local-testing/answer-quality-scorecard.md). |
| Hosted onboarding | `eshu hosted-onboard` | Onboards a project team and repository set onto a *deployed* service. Takes `--team`, narrow repository rules (`--repo owner/name`, `--repo-pattern '^org/team-'`), and rejects an accidental whole-org glob unless `--confirm-broad` is set. Reuses the `hosted-setup` staged checks and emits a redacted onboarding artifact (`--out <path>`, `--format md\|json`) with the API/MCP URLs, the token source name (never the value), indexed repos, queue/completeness status, starter prompts, and structured `starter_playbooks[]` entries with playbook IDs, versions, ordered tools, and expected truth classes. It documents the current shared-token authorization limitation. See [Hosted Project Onboarding](../deployment/hosted-onboarding.md). |
| Indexing and workspace management | `eshu scan`, `eshu index`, `eshu watch`, `eshu workspace`, `eshu list`, `eshu stats`, `eshu index-status` | [CLI Indexing](cli-indexing.md) and [Index Repositories](../use/index-repositories.md) |
| Code search and analysis | `eshu find`, `eshu analyze`, `eshu query` | [CLI Analysis](cli-analysis.md), [Ask Code Questions](../use/code-questions.md), and [Language Query DSL](language-query-dsl.md) |
| Code-to-cloud tracing | `eshu trace service`, `eshu map` | [Trace Infrastructure](../use/trace-infrastructure.md) and [Relationship Mapping](relationship-mapping.md) |
| Security intelligence | `eshu vuln-scan repo`, `eshu vuln-scan provider-parity` | [Security Intelligence](security-intelligence.md) and [Vulnerability Parity Gate](vulnerability-parity-gate.md) |
| Admin and status | `eshu admin`, API-backed status reads | [HTTP API Status/Admin](http-api/status-admin.md), [Runtime Admin API](runtime-admin-api.md), and [CLI K.I.S.S.](cli-kiss.md) |
| Documentation truth | `eshu docs verify` | Local Markdown claim verification plus optional API-backed container-image truth checks. This is separate from Git collector ingestion of repo-hosted documentation facts. Use command help for flags. |
| Components | `eshu component` | Local component scaffolding plus package inspect, verify, install, fixture conformance, list, enable, disable, and uninstall. `component init collector` creates a minimal collector extension scaffold. `component inventory --limit <n>` and `component diagnostics <component-id>` read hosted API component-extension inventory and policy diagnostics with the canonical envelope. `component extraction-readiness [collector-family]` prints the advisory collector extraction readiness checklist (keep-in-tree / extraction-candidate / blocked / external-ready) from local static policy data, with `--verbose` for the per-criterion checklist. Each subcommand supports stable `--json` output; install and enable also support `--dry-run`. See [Component Package Manager](component-package-manager.md). |
| System and configuration | `eshu doctor`, `eshu config`, `eshu neo4j setup`, `eshu version` | [CLI System And Configuration](cli-system.md), [Configuration](configuration.md), and [Environment Variables](environment-variables.md) |
| Assistant guidance | `eshu assistant install`, `eshu assistant status`, `eshu assistant uninstall`, `eshu assistant hook preflight` | [Assistant Guidance Install](assistant-guidance.md) and [Assistant Fast-Path Hook Contract](assistant-fast-path-hooks.md) |
| Compatibility and shortcuts | old names such as `eshu clean`, `eshu delete`, `eshu add-package`, plus shortcuts such as `eshu i` and `eshu ls` | Compatibility stubs print replacement guidance. Prefer the command-family docs above for current workflows. |

## API Target Resolution

Commands that accept remote flags use those flag values first:

- `--service-url`
- `--api-key`
- `--profile`

When a value is not passed by flag, the CLI resolves API settings in this order:

1. persisted `eshu config` values, including profile-specific keys
2. process environment
3. `http://localhost:8080`

Persisted keys are:

- `ESHU_SERVICE_URL`
- `ESHU_API_KEY`
- `ESHU_SERVICE_PROFILE`
- `ESHU_REMOTE_TIMEOUT_SECONDS`

Profile-specific persisted keys follow the patterns `ESHU_SERVICE_URL_<PROFILE>`
and `ESHU_API_KEY_<PROFILE>`. The profile name is uppercased before lookup.

Some API-backed commands do not register per-command remote flags yet. Use
[CLI K.I.S.S.](cli-kiss.md) for the current split between remote-flag commands
and API-backed commands that rely on config, environment, or the localhost
default.

Runtime-only environment variables such as
`ESHU_SEMANTIC_PROVIDER_PROFILES_JSON` and
`ESHU_SEMANTIC_EXTRACTION_POLICY_JSON`, plus the explicit local
`ESHU_SEMANTIC_SEARCH_LOCAL_EMBEDDER` retrieval switch, are documented in
[Environment Variables](environment-variables.md). They are read by API/MCP
runtimes, not by CLI target resolution, and must not carry provider keys,
credential values, prompts, or provider responses.

`eshu vuln-scan repo [path]` is the local-scan exception to the localhost
fallback. If no service URL is configured by flag, persisted config, or
`ESHU_SERVICE_URL`, it starts or attaches to the workspace-local authoritative
service and launches a short-lived loopback API reader for the scan. Passing
`--service-url` keeps the command on that explicit API and does not start local
services.

The command runs in scoped mode by default: the scope plan is derived from
the readiness envelope of `/api/v0/supply-chain/impact/findings` for the
selected repository, and the CLI downgrades a `ready_*` verdict to
`evidence_incomplete` when the envelope's aggregate `freshness` is `stale`
(`advisory_cache_stale`) or unknown
(`advisory_cache_freshness_unknown`). Per-source entries in
`readiness.source_snapshots[]` are surfaced for operator visibility while the
CLI gates on the server-owned aggregate scoped freshness verdict. Pass `--broad`
to skip that guard and accept advisory/package coverage beyond observed
dependencies; the JSON envelope reports the active mode under
`data.scope_mode` and the bounded plan under `data.scope_plan` regardless of
mode. Local performance evidence is attached as `data.scan_performance`
with wall-time, repository size/file count, observed-dependency fact count,
advisory fact count, package-registry fact count, cache freshness, scope
mode, and the readiness state the scan stopped at. The
`*_facts` fields are counts of source facts (the same
`evidence_sources[].fact_count` the server reports), not unique packages or
advisory sources. `package_registry_facts` counts scoped registry metadata for
the requested package, or for packages tied to the requested repository by
consumption evidence. When dependency facts require registry metadata and no
scoped registry facts are present, JSON output reports
`package_registry_freshness = "missing"` instead of omitting the freshness
field.

With `--json`, `eshu vuln-scan repo` returns the normal Eshu envelope and a
stable scanner report at `data.report`. The report schema version is
`eshu.vulnerability_report.v1`; it includes a summary, readiness state,
freshness, missing evidence, unsupported targets, evidence-source counts,
sanitized finding rows, target/package/image/SBOM context, evidence fact
handles, remediation metadata, the scope plan, and scan-performance evidence.
The raw reducer-owned findings remain in `data.findings` so automation can
keep using the API/MCP envelope directly.

With `--export sarif`, the command writes SARIF v2.1.0 to stdout instead of the
text summary or JSON envelope. Findings become SARIF results with vulnerability
identity, package and image target context, severity, remediation metadata,
evidence fact ids, and source locations only when the API supplied a real path.
Run properties preserve the scanner report schema, readiness state, freshness,
scope mode, exit code, missing evidence, and unsupported targets. A non-ready
scan such as `evidence_incomplete` or `unsupported` emits a location-free SARIF
status result, so CI does not treat missing evidence as a clean zero-finding
scan.

With `--export vex`, the command writes a compact
`eshu.vex_statements.v1` JSON document for tools that need VEX-style status
statements rather than the full scanner report. Eshu maps only reducer-owned
impact statuses into statements: `affected_exact` and `affected_derived`
become `affected`, `not_affected_known_fixed` becomes `not_affected`, and
`possibly_affected` or `unknown_impact` remain `under_investigation`.
`evidence_incomplete`, `unsupported`, `target_incomplete`, and
`readiness_unavailable` readiness states do not create `not_affected`
statements. The VEX document still carries readiness freshness, missing
evidence, unsupported targets, evidence fact handles, and sanitized
remediation fields when the reducer supplied them.

Use `--json` instead of `--export vex` when automation needs the complete
scanner envelope: raw reducer finding rows, scope-plan counters, package
metadata freshness, source snapshots, scan-performance evidence, target
diagnostics, or unsupported/missing evidence that is not attached to a VEX
statement. VEX is an exchange artifact for defensible affected/not-affected
claims; the JSON report is the audit artifact for deciding whether Eshu had
enough evidence to make those claims. Use the process exit code as the scanner
verdict: exports are artifacts, not replacements for the readiness contract.
`--json` and `--export` cannot be combined.

Exit codes are part of the scanner contract:

| Code | Meaning |
| --- | --- |
| `0` | `ready_zero_findings`: required evidence is ready and no reducer-owned findings were returned. |
| `3` | `ready_with_findings`: reducer-owned findings are present. |
| `4` | Evidence is not cleanly ready: `not_configured`, `target_incomplete`, `evidence_incomplete`, or `readiness_unavailable`. |
| `5` | `unsupported`: Eshu observed target evidence that the matcher cannot resolve. |
| `1` | Runtime or transport failure before Eshu can classify readiness. |

Sanitized terminal summary example:

```text
Vulnerability scan (scoped): evidence_incomplete
Repository: repo-synthetic-local
Findings: 0
Exit: code=4 reason=evidence_incomplete
Readiness: state=evidence_incomplete freshness=unknown
Missing evidence: advisory_cache_freshness_unknown
Scope: observed_dependency_facts=2 advisory_facts=80 package_registry_facts=0 freshness=unknown
Performance: wall_time_ms=1042 repo_files=24 repo_bytes=8192 stop=evidence_incomplete
```

Sanitized JSON report excerpt:

```json
{
  "data": {
    "readiness_state": "ready_with_findings",
    "report": {
      "schema_version": "eshu.vulnerability_report.v1",
      "summary": {
        "total_findings": 1,
        "exit_code": 3,
        "exit_reason": "findings_present",
        "readiness_state": "ready_with_findings"
      },
      "readiness": {
        "state": "ready_with_findings",
        "freshness": "fresh",
        "unsupported_targets": []
      },
      "findings": [
        {
          "finding_id": "finding-synthetic-1",
          "cve_id": "CVE-2026-SYNTHETIC-0001",
          "target": {
            "repository_id": "repo-synthetic-local",
            "subject_digest": "sha256:1111111111111111111111111111111111111111111111111111111111111111",
            "image_ref": "registry.example.test/team/api@sha256:1111111111111111111111111111111111111111111111111111111111111111",
            "runtime_reachability": "image_sbom"
          },
          "package": {
            "ecosystem": "npm",
            "package_id": "npm://registry.npmjs.org/synthetic-runtime-lib",
            "package_name": "synthetic-runtime-lib",
            "purl": "pkg:npm/synthetic-runtime-lib@2.3.4"
          },
          "affected": {
            "status": "possibly_affected",
            "observed_version": "2.3.4",
            "requested_range": "^2.3.0",
            "vulnerable_range": "<2.3.5",
            "fixed_version": "2.3.5",
            "match_reason": "range_only_manifest"
          },
          "remediation": {
            "fixed_version_source": "ghsa",
            "match_reason": "range_only_manifest",
            "first_patched_version": "2.3.5",
            "confidence": "partial",
            "reason": "direct_upgrade_allowed"
          },
          "evidence_handles": [
            {"kind": "fact", "id": "fact-package-synthetic"}
          ]
        }
      ]
    }
  },
  "error": null
}
```

Sanitized VEX-style example:

```json
{
  "schema_version": "eshu.vex_statements.v1",
  "scope": {
    "kind": "repository",
    "repository_id": "repo-synthetic"
  },
  "readiness": {
    "state": "ready_with_findings",
    "freshness": "fresh"
  },
  "statements": [
    {
      "statement_id": "eshu-vex-finding-1",
      "finding_id": "finding-1",
      "status": "affected",
      "impact_status": "affected_exact",
      "vulnerability": {
        "cve_id": "CVE-2026-0001",
        "advisory_id": "GHSA-xxxx-yyyy-zzzz"
      },
      "product": {
        "repository_id": "repo-synthetic",
        "package_id": "npm://registry.npmjs.org/example-lib",
        "ecosystem": "npm"
      },
      "evidence_handles": [
        {
          "kind": "fact",
          "id": "fact-package-synthetic"
        }
      ],
      "remediation": {
        "fixed_version": "1.2.3",
        "fixed_version_source": "ghsa",
        "match_reason": "npm_semver_affected_range",
        "first_patched_version": "1.2.3",
        "confidence": "exact"
      }
    }
  ]
}
```

`eshu vuln-scan provider-parity` is API-backed and operator-local. It requires
`--allowlist-file`, reads provider credentials only from the named local
environment variable, and emits aggregate provider/Eshu parity counts without
printing repositories, packages, advisory ids, alert URLs, tokens, or payloads.
The JSON output includes repository count, provider alert count, Eshu finding
count, approved mismatch class counts, truncation, readiness state, and
freshness state.

## Version Probes

The direct service binaries listed in
[Service Runtimes](../deployment/service-runtimes.md) accept `--version` and
`-v` as a single argument. They print their build-time version and exit before
telemetry, Postgres, graph, queue, or HTTP startup.

## Related Docs

- [CLI Indexing](cli-indexing.md)
- [CLI Analysis](cli-analysis.md)
- [CLI System And Configuration](cli-system.md)
- [Configuration](configuration.md)
- [Environment Variables](environment-variables.md)
- [HTTP API](http-api.md)
- [MCP Reference](mcp-reference.md)
