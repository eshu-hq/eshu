# Security Intelligence Release Gate

The security intelligence release gate is the final proof an operator must
record before cutting the next prerelease image with vulnerability,
supply-chain impact, SBOM-attestation, scanner-worker, or provider-alert
reconciliation work. It does not cut or push an image. It captures the
runbook-style evidence that Eshu's security intelligence path is honest in the
same shapes users will run: hosted remote Compose, preserved-volume restart,
API and MCP readback, the fixture parity gate, optional operator-local
provider parity, and Kubernetes / EKS rollout proof with pprof, logs, queue
telemetry, and resource snapshots.

This page is the operator runbook for issues
[#657](https://github.com/eshu-hq/eshu/issues/657),
[#992](https://github.com/eshu-hq/eshu/issues/992), and
[#1019](https://github.com/eshu-hq/eshu/issues/1019). The detail behind each
sub-proof lives in:

- [Security Intelligence](security-intelligence.md)
- [Vulnerability Scanner Confidence Matrix](vulnerability-scanner-confidence.md)
- [Vulnerability Parity Gate](vulnerability-parity-gate.md)
- [Remote Collector E2E](local-testing/remote-collector-e2e.md)
- [Remote E2E Runtime State](remote-e2e-runtime-state.md)
- [Deploy To EKS](../deploy/eks/index.md)

## Why the gate exists

Prior releases taught us not to treat merged code, green pods, or service
names as proof of deployability. The gate forces the same kind of evidence
for security intelligence that other Eshu surfaces require: the exact commit,
image tag candidate, NornicDB pin, schema/bootstrap state, every queue and
fact count, and the API/MCP readback shape that users will actually see.
Operators should compare the release claim against the
[Vulnerability Scanner Confidence Matrix](vulnerability-scanner-confidence.md)
before cutting an image; a row marked `blocked`, `partial`, or `unsupported`
cannot be described as fully scanner-ready without narrower wording and linked
evidence.

The gate is intentionally:

- **Runbook + harness.** The harness emits a structured evidence document; the
  runbook tells operators which phases to run and what to capture.
- **Privacy-aware.** No private repository names, package names, alert URLs,
  CVE descriptions, copied provider payloads, machine paths, or tokens are
  recorded. The harness rejects provider-comparison and proof-matrix inputs
  that look like private data.
- **Image-cut neutral.** The gate produces evidence only. Image push and chart
  release stay with the normal Eshu release workflow once this gate is green.

## What the gate covers

| Acceptance criterion | Where it is proven |
| --- | --- |
| Standalone local proof that `eshu vuln-scan repo` uses the same reducer-owned finding/readiness envelope and does not fork a second vulnerability engine | Focused CLI tests plus an operator-recorded local run with JSON/terminal/export shape, cache freshness, scope counters, exit code, wall time, and zero retry/dead-letter evidence |
| Hosted E2E proof for the same vulnerability truth model through API, MCP, collectors, scanner-worker, reducer, Postgres, graph, and queue state | `runtime` phase plus remote Compose readback, preserved-volume restart, and `k8s` phase before a scanner-ready image claim |
| Clean-volume remote Compose proof with API, MCP, ingester, reducer, coordinator, package-registry, vulnerability-intelligence, SBOM-attestation, scanner-worker, security-alert path, and backing stores | `runtime` phase plus [Remote E2E Runtime State](remote-e2e-runtime-state.md) and [Remote Collector E2E](local-testing/remote-collector-e2e.md) |
| Preserved-volume restart proof and no stale queue, duplicate claim, dead-letter, or startup regression | `runtime` phase re-run after restarting data-plane services on the same volumes |
| API and MCP readback for supply-chain impact, readiness, explanation, advisory evidence, security-alert reconciliations, SBOM attachments, container-image identities, priority, suppression, and exports where applicable | `runtime` phase API readback against the documented endpoints |
| Vulnerability parity gate against synthetic fixtures and optional operator-local provider comparison with only aggregate/private-safe mismatch classes | `fixtures` phase plus optional `provider` phase |
| Representative 20-50 repository proof across npm, Go modules, PyPI, Maven/Gradle, Composer, RubyGems, Cargo, NuGet, Terraform/IaC, SBOM/image, deployment, readback counts, queue-zero counters, wall time, CPU/memory, logs, pprof, and follow-up issue references | Optional `proof-matrix` phase with an operator-local aggregate JSON file |
| Pprof availability, effective env, queue counts, retries, dead letters, target counts, fact counts, wall times, CPU, memory, freshness states | `runtime` phase docker stats snapshot, `/api/v0/index-status` payload, and pprof reachability check |
| Kubernetes / EKS proof with pprof, logs, queue telemetry, and resource snapshots before declaring release readiness | `k8s` phase plus [Deploy To EKS](../deploy/eks/index.md) |
| Exact commit, image tag candidate, NornicDB tag/commit, clean-volume state, schema/bootstrap state, and pass/fail evidence | `state` phase, always offline |

### Supply-chain impact path proof

Supply-chain impact findings must keep repository, workload, deployment, and
service hops separate. Repository dependency evidence can attach a workload
only when reducer-owned `reducer_workload_identity` facts exist for the same
repository scope. Deployment/environment hops still require CI/CD or runtime
evidence, and service ids still require service-catalog correlation evidence.
Missing service catalog data remains explicit `service evidence missing`
rather than a guessed service id.

No-Regression Evidence: issue #680 keeps the active fact walk bounded by the
existing repository follow-up filter. The only new active kind is
`reducer_workload_identity`, loaded through repository-scope predicates
(`scope_id`, `payload.scope_id`, `scope.source_key`, `scope.payload.repo_id`,
and `scope.payload.id`) inside the same fact-kind-gated branch as the existing
runtime correlation facts. Focused tests cover repository-only, workload-only,
deployment-plus-workload, service-attached, and stale/missing service evidence
without adding graph reads or queue claims to the impact handler.

Observability Evidence: no new telemetry series are required. The existing
`SupplyChainImpactHandler` counters, persisted `evidence_path`,
`evidence_fact_ids`, `workload_ids`, `service_ids`, `environments`, and
`missing_evidence` fields expose whether the reducer attached workload
identity, deployment evidence, service-catalog evidence, or left a hop
missing for API/MCP callers and release-gate readback.

## Harness

The harness lives at `scripts/security_intelligence_release_gate.sh`. It runs
the offline phases by default; the proof-matrix, runtime, k8s, and provider phases are
operator-opted into.

```bash
scripts/security_intelligence_release_gate.sh \
  --image-tag-candidate v0.0.3-pre-release-9
```

The default invocation runs `state`, `focused`, and `fixtures`. It works on a
checkout that does not have Docker, kubectl, AWS credentials, or any private
data. It writes:

- `evidence.json` with the structured envelope
- `evidence.md` with a human-readable summary
- per-phase log files under the same output directory

The harness fails closed when any enabled phase fails or when provider
comparison input looks like private data.

### Phases

| Phase | What it proves | Requirements |
| --- | --- | --- |
| `state` | Captures commit, branch, Helm chart and app version, image tag candidate, NornicDB image and digest, schema migration count and latest file, configured remote E2E services, and scanner-worker resource-limit env vars. | None. Always offline. |
| `focused` | Runs `go test` over `./internal/vulnerabilityparity`, `./internal/reducer`, `./internal/query`, `./internal/mcp`, `./internal/collector/vulnerabilityintelligence`, `./internal/collector/scannerworker`, `./cmd/scanner-worker`. | Go toolchain. |
| `fixtures` | Runs `go test ./internal/vulnerabilityparity` and `scripts/verify_vulnerability_parity_fixtures.sh`. | Go toolchain plus `jq` (already required by the verifier). |
| `proof-matrix` | Records an operator-local aggregate representative corpus proof. The phase requires every supported ecosystem to be covered or explicitly classified, requires Terraform/IaC, image/SBOM, and deployment evidence-family coverage, requires zero retrying/failed/dead-letter readback, and requires public issue refs for nonzero mismatch classes. It rejects private repository/package/provider/path/token-looking data. | A JSON file passed with `--proof-matrix` or `ESHU_RELEASE_GATE_PROOF_MATRIX`. |
| `runtime` | Wraps `scripts/verify_remote_e2e_runtime_state.sh`, calls the documented supply-chain endpoints with `limit=1`, captures `docker stats --no-stream` for the running services, and optionally probes pprof when `--pprof-base-url` is provided (pprof rides a separate listener on its own host port; without an explicit URL the gate records `pprof_status: unchecked`). Any endpoint readback error, a missing verifier script, or a verifier non-zero exit fails the phase. | A running remote Compose stack. `--api-base-url` is required; `--api-key` is required when the stack uses an explicit bearer token. |
| `k8s` | Captures public-safe pod/resource summaries, sanitized Helm values, sanitized logs for Eshu pods, `/admin/status` and `/api/v0/index-status` queue readback, and optional pprof reachability. Missing logs, missing queue retry/dead-letter readback, missing CPU/memory snapshots, unreachable provided pprof URL, or missing Helm values are recorded as phase failures. | `--k8s-namespace`, `--api-base-url`, `kubectl`, `curl`, and `helm`. Use `--pprof-base-url` when a private pprof port-forward is available; without it the phase records `pprof_status: unchecked`. |
| `provider` | Records an operator-supplied aggregate-only parity comparison JSON. The harness rejects anything that contains package names, alert URLs, repository names, installation ids, or known token prefixes (`ghp_`, `github_pat_`, `glpat-`). | A JSON file containing `comparison_id` and a non-empty `totals` map of classification class to count. |

### Selecting phases

```bash
# offline defaults
scripts/security_intelligence_release_gate.sh

# include the runtime phase against a remote Compose stack
scripts/security_intelligence_release_gate.sh \
  --phases state,focused,fixtures,runtime \
  --api-base-url "$REMOTE_API_BASE_URL" \
  --api-key "$REMOTE_API_KEY" \
  --pprof-base-url "$REMOTE_PPROF_BASE_URL" \
  --image-tag-candidate v0.0.3-pre-release-9

# everything
scripts/security_intelligence_release_gate.sh --phases all \
  --api-base-url "$REMOTE_API_BASE_URL" \
  --api-key "$REMOTE_API_KEY" \
  --pprof-base-url "$REMOTE_PPROF_BASE_URL" \
  --k8s-namespace eshu \
  --proof-matrix ~/eshu/operator-only/proof-matrix.json \
  --provider-compare ~/eshu/operator-only/parity-aggregate.json \
  --image-tag-candidate v0.0.3-pre-release-9
```

`$REMOTE_PPROF_BASE_URL` is the base of the separate pprof listener
(`ESHU_PPROF_ADDR`, typically a different host port than the API). Omit the
flag to mark `pprof_status: unchecked` rather than probe the API URL.
For Kubernetes, pass the local port-forward base URL for the API admin surface
with `--api-base-url`; the phase stores only sanitized summaries and does not
record the private URL in the Kubernetes evidence.

## Sequence operators follow

1. **Capture state offline.** From the release candidate commit, run the
   harness with the offline defaults plus `--image-tag-candidate`. Confirm the
   schema migration count and NornicDB digest match the values the deployment
   will run.
2. **Run the fixture parity gate.** The `fixtures` phase covers the
   synthetic suite. Mismatches must be classified before continuing.
3. **Run standalone local proof.** From the same commit, run
   `eshu vuln-scan repo` against a bounded fixture or representative local
   repository in fresh-cache and offline/repeat-cache shapes. Record the JSON
   report, terminal summary, export shape if relevant, readiness, cache
   freshness, scope counters, exit code, wall time, retry count, and
   dead-letter count. A clean or not-affected claim is invalid if the scan did
   not reach a ready state.
4. **Define the private representative corpus.** Keep the actual repository
   allowlist, local paths, provider targets, package coordinates, and tokens
   outside the public repo. The public release evidence may contain only a
   synthetic matrix id, ecosystem counts, evidence-family counts, readback
   counters, mismatch class totals, and public issue refs.
5. **Bring up the remote Compose stack with a clean volume.** Follow
   [Remote Collector E2E](local-testing/remote-collector-e2e.md). Use the
   `.env.remote-e2e.example` defaults or your full-corpus profile.
6. **Run the `runtime` phase.** It calls
   [Remote E2E Runtime State](remote-e2e-runtime-state.md) plus the
   supply-chain readback. Record the resulting `evidence.json`,
   `runtime-readback/`, `docker-stats.json`, and pprof reachability.
7. **Run the `proof-matrix` phase.** Build the operator-local matrix from the
   representative run and pass it with `--proof-matrix`. A nonzero mismatch
   class without a public issue ref fails the phase; a matrix that stores
   private repository names, package names, provider URLs, alert URLs, tokens,
   or machine paths is rejected before it reaches evidence.
8. **Run preserved-volume restart proof.** Stop the data-plane services
   without removing volumes, start them again, then re-run the `runtime`
   phase. Compare workflow run counts, queue counts, retries, dead letters,
   and `index-status` health between the two runs. Any new dead letter or
   stuck claim fails the gate.
9. **Run the optional `provider` phase.** When operator-local provider data
   is available, generate the aggregate-only comparison outside the repo and
   pass the file to `--provider-compare`. The harness records only the
   aggregate counts and the synthetic comparison id.
10. **Run the `k8s` phase against the staging cluster.** Follow
   [Deploy To EKS](../deploy/eks/index.md) for cluster setup. Use a private
   port-forward for `--api-base-url` so the harness can read
   `/admin/status?format=json` and `/api/v0/index-status`; add
   `--pprof-base-url` when a private pprof port-forward is available. The
   harness records sanitized pod/resource summaries, sanitized logs, sanitized
   Helm values, queue retry/dead-letter counts, terminal-status summary, and
   pprof reachability without storing private URLs, pod hostnames, IP
   addresses, provider URLs, repository names, package names, tokens, or
   machine-local paths.
11. **Review `evidence.md` and `evidence.json` together.** The gate is green
   only when `evidence.json` has `pass: true` at the top level and every
   enabled phase has `status` set to `pass` or `skipped`. Phases that fail
   (or that were enabled but produced an incomplete capture) record
   `status: fail` and an entry under `.failures[]`.

## Evidence schema

`evidence.json` carries `schema_version: 1`, `generated_at`, redacted
`repo_root`, `phases[]` (the list operators requested), `pass` (overall
boolean), and `failures[]` (per-phase failure messages). Each requested phase
populates a top-level key with at least `status` set to `pass`, `fail`, or
`skipped`. The runtime phase also surfaces `api_base_url` (the normalized value
the gate actually used; see below), `endpoints_failed`, and per-endpoint
readback rows keyed by the documented `/api/v0/...` paths. The `proof_matrix`
phase surfaces `matrix_id`, `mode`, `repository_count`, required ecosystem
coverage, evidence-family coverage, aggregate package/dependency/advisory and
finding counts, readiness state counts, retrying/failed/dead-letter counters,
wall time, CPU/memory snapshot status, pprof/log status, mismatch-class totals,
and follow-up issue counts. The Kubernetes phase
surfaces `pprof_status`, `logs_ok`, `queue_readback_ok`, `queue_terminal_ok`,
queue outstanding/pending/in-flight/retry/failed/dead-letter counters,
sanitized evidence file references, and resource snapshot status.

The `--api-base-url` value is normalized: a trailing `/` or `/api/v0` is
stripped so the same env value that `verify_remote_e2e_runtime_state.sh`
expects (`ESHU_REMOTE_E2E_API_BASE_URL`, often `…/api/v0`) does not
double-prefix the harness's hard-coded endpoint paths. Pass the base API URL
in either shape; `evidence.json.runtime.api_base_url` records what the gate
actually called.

The harness reads HTTP routes only. The MCP tools (`list_supply_chain_*`,
`list_advisory_evidence`, `explain_supply_chain_impact`,
`list_sbom_attestation_attachments`) read the same reducer-owned facts as
the HTTP routes the harness probes. When MCP-specific transcript evidence is
required, operators drive `eshu mcp` or their MCP client separately and
attach the transcript to the same evidence directory.

### Proof matrix input

The `proof-matrix` phase accepts an operator-local JSON file. Keep that file
outside the public repository with the private corpus manifest and env files.
The harness copies only aggregate fields into `evidence.json`.

Minimal shape:

```json
{
  "schema_version": 1,
  "matrix_id": "representative-security-intelligence-v1",
  "mode": "representative",
  "repository_count": 24,
  "required_repository_count": {"min": 20, "max": 50},
  "ecosystems": {
    "npm": {"repository_count": 3, "affected_rows": 1, "ready_zero_or_incomplete_rows": 1},
    "gomod": {"repository_count": 3, "affected_rows": 1, "ready_zero_or_incomplete_rows": 1},
    "pypi": {"repository_count": 3, "affected_rows": 1, "ready_zero_or_incomplete_rows": 1},
    "maven": {"repository_count": 3, "affected_rows": 1, "ready_zero_or_incomplete_rows": 1},
    "composer": {"repository_count": 2, "affected_rows": 1, "ready_zero_or_incomplete_rows": 1},
    "rubygems": {"repository_count": 2, "affected_rows": 1, "ready_zero_or_incomplete_rows": 1},
    "cargo": {"repository_count": 2, "affected_rows": 1, "ready_zero_or_incomplete_rows": 1},
    "nuget": {"repository_count": 1, "affected_rows": 1, "ready_zero_or_incomplete_rows": 1}
  },
  "evidence_families": {
    "terraform_iac": {"repository_count": 2, "evidence_rows": 5},
    "image_sbom": {"repository_count": 1, "evidence_rows": 2},
    "deployment": {"repository_count": 1, "evidence_rows": 1}
  },
  "readback": {
    "package_fact_count": 220,
    "dependency_fact_count": 190,
    "advisory_fact_count": 72,
    "finding_count": 17,
    "ready_state_counts": {"ready": 20, "evidence_incomplete": 4},
    "retrying": 0,
    "failed": 0,
    "dead_letters": 0,
    "wall_time_seconds": 840,
    "cpu_memory_snapshot": "captured",
    "pprof_status": "reachable",
    "logs_status": "captured"
  },
  "mismatch_classes": {
    "target_collection": 0,
    "advisory_ingestion": 1,
    "version_matching": 1,
    "unsupported_ecosystem": 0,
    "provider_only_behavior": 0,
    "stale_provider_alert": 0,
    "reducer_bug": 0
  },
  "follow_up_issues": [
    {"class": "advisory_ingestion", "issue_ref": "#1234"},
    {"class": "version_matching", "issue_ref": "#1235"}
  ]
}
```

If an ecosystem or evidence family cannot be represented in the current
corpus, keep the row and replace the row evidence with `gap_class` and
`issue_ref`. The accepted mismatch and gap classes are
`target_collection`, `advisory_ingestion`, `version_matching`,
`unsupported_ecosystem`, `provider_only_behavior`, `stale_provider_alert`, and
`reducer_bug`.

## Privacy boundary

The harness intentionally records only public-safe data. In particular:

- Provider comparison and proof-matrix inputs are aggregate-only. Package names,
  advisory URLs, repository names, installation ids, hostnames, machine paths,
  and credential prefixes are rejected before they reach the evidence document.
- API readback uses `limit=1` so response bodies stay small and diagnostic,
  not a dump of customer findings.
- Kubernetes evidence stores sanitized summaries only. Pod snapshots remove
  node names, pod names, IP addresses, and image references; logs and Helm
  values redact repository names, package names, provider URLs, tokens, common
  secret key values, authorization headers, ARNs, account ids, hostnames, IP
  addresses, and machine-local paths before writing evidence files.
- Operator-local artefacts (private corpora paths, AWS account ids, GitHub
  installation ids) are not echoed by the gate; they live in the operator's
  own env file.

## Verification

`scripts/test-security_intelligence_release_gate.sh` proves the offline phases
against a synthesized repo: missing `Chart.yaml`, unknown phase names, private
data in provider comparison, valid aggregate-only provider comparison, the
captured state fields, and the markdown summary. Run it before changing the
harness. `scripts/test-security_intelligence_release_gate_proof_matrix.sh`
proves valid matrix capture, required ecosystem coverage, private-data
rejection, and follow-up issue enforcement.

```bash
scripts/test-security_intelligence_release_gate.sh
scripts/test-security_intelligence_release_gate_proof_matrix.sh
```

`scripts/test-security_intelligence_release_gate_k8s.sh` proves the Kubernetes
evidence contract with fake `kubectl`, `helm`, and `curl` tools. It verifies
sanitized logs, pprof reachability, queue retry/dead-letter readback, CPU and
memory resource snapshots, and public-safe generated Kubernetes evidence.

The fixture parity gate and the focused security-intelligence Go tests use
their existing per-package commands. The release gate harness wraps them so
the evidence stays in one place.
