# Security Intelligence Release Gate

The security intelligence release gate is the final proof an operator must
record before cutting the next prerelease image with vulnerability,
supply-chain impact, SBOM-attestation, scanner-worker, or provider-alert
reconciliation work. It does not cut or push an image. It captures the
runbook-style evidence that Eshu's security intelligence path is honest in the
same shapes users will run: hosted remote Compose, preserved-volume restart,
API, MCP, and CLI readback, the fixture parity gate, optional operator-local
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
fact count, and the API/MCP/CLI readback shape that users will actually see.
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
  recorded. The harness rejects provider-comparison, proof-matrix, and
  readback-proof inputs that look like private data.
- **Image-cut neutral.** The gate produces evidence only. Image push and chart
  release stay with the normal Eshu release workflow once this gate is green.

## What the gate covers

| Acceptance criterion | Where it is proven |
| --- | --- |
| Standalone local proof that `eshu vuln-scan repo` uses the same reducer-owned finding/readiness envelope and does not fork a second vulnerability engine | Focused CLI tests plus an operator-recorded local run with JSON/terminal/export shape, cache freshness, scope counters, exit code, wall time, and zero retry/dead-letter evidence |
| Hosted E2E proof for the same vulnerability truth model through API, MCP, collectors, scanner-worker, reducer, Postgres, graph, and queue state | `runtime` phase plus remote Compose readback, preserved-volume restart, and `k8s` phase before a scanner-ready image claim |
| Clean-volume remote Compose proof with API, MCP, ingester, reducer, coordinator, package-registry, vulnerability-intelligence, SBOM-attestation, scanner-worker, security-alert path, and backing stores | `runtime` phase plus [Remote E2E Runtime State](remote-e2e-runtime-state.md) and [Remote Collector E2E](local-testing/remote-collector-e2e.md) |
| Preserved-volume restart proof and no stale queue, duplicate claim, dead-letter, or startup regression | `runtime` phase re-run after restarting data-plane services on the same volumes |
| API, MCP, and CLI readback for supply-chain impact, readiness, explanation, advisory evidence, security-alert reconciliations, SBOM attachments, container-image identities, priority, suppression, and exports where applicable | `runtime` API readback plus `readback-proof` aggregate API/MCP/CLI transcript proof |
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
the offline phases by default; the proof-matrix, runtime, readback-proof, k8s,
and provider phases are operator-opted into.

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

The harness fails closed when any enabled phase fails, when the image tag
candidate is missing from the state phase, or when operator-supplied aggregate
inputs look like private data.

### Phases

| Phase | What it proves | Requirements |
| --- | --- | --- |
| `state` | Captures commit, branch, Helm chart and app version, image tag candidate, NornicDB image and digest, schema migration count and latest file, configured remote E2E services, and scanner-worker resource-limit env vars. | `--image-tag-candidate` is required for release proof. |
| `focused` | Runs `go test` over `./internal/vulnerabilityparity`, `./internal/reducer`, `./internal/query`, `./internal/mcp`, `./internal/collector/vulnerabilityintelligence`, `./internal/collector/scannerworker`, `./cmd/scanner-worker`. | Go toolchain. |
| `fixtures` | Runs `go test ./internal/vulnerabilityparity` and `scripts/verify_vulnerability_parity_fixtures.sh`. | Go toolchain plus `jq` (already required by the verifier). |
| `proof-matrix` | Records an operator-local aggregate representative corpus proof. The phase requires every supported ecosystem to be covered or explicitly classified, requires Terraform/IaC, image/SBOM, and deployment evidence-family coverage, requires zero retrying/failed/dead-letter readback, captured CPU/memory, reachable pprof, captured logs, and public issue refs for nonzero mismatch classes. It rejects private repository/package/provider/path/token-looking data. | A JSON file passed with `--proof-matrix` or `ESHU_RELEASE_GATE_PROOF_MATRIX`. |
| `runtime` | Wraps `scripts/verify_remote_e2e_runtime_state.sh`, records whether the proof is the clean-volume run or preserved-volume restart, validates aggregate clean/preserved Compose volume proof, calls the documented supply-chain endpoints with `limit=1`, captures normalized `/api/v0/index-status` queue fields, captures valid `docker stats --no-stream` CPU/memory evidence, and requires reachable pprof. Any endpoint readback error, non-terminal queue readback, missing verifier script, verifier non-zero exit, missing or invalid Docker CPU/memory evidence, missing pprof URL, unreachable pprof, missing volume proof, or preserved run without a prior clean evidence file fails the phase. | A running remote Compose stack. `--runtime-run-kind clean` or `--runtime-run-kind preserved`, `--runtime-volume-proof`, `--api-base-url`, and `--pprof-base-url` are required. Preserved runs also require `--previous-runtime-evidence`. `--api-key` is required when the stack uses an explicit bearer token. |
| `readback-proof` | Records operator-local aggregate API, MCP, and CLI readback proof. Raw transcripts stay outside the public repo; the gate copies only surface status/counts, truncated/unsupported/missing/ambiguous counters, and queue-zero counters. Missing API/MCP/CLI, failed checks, nonzero retry/failed/dead-letter counts, or private-looking transcript content fails the phase. | A JSON file passed with `--readback-proof` or `ESHU_RELEASE_GATE_READBACK_PROOF`. Use `scripts/e2e_readback_parity.sh` to build it from bounded local summaries. |
| `k8s` | Captures public-safe pod/resource summaries, sanitized Helm values, sanitized logs for Eshu pods, `/admin/status` and `/api/v0/index-status` queue readback, and required pprof reachability. Missing logs, missing queue retry/dead-letter readback, missing CPU/memory snapshots, missing or unreachable pprof, or missing Helm values are recorded as phase failures. | `--k8s-namespace`, `--api-base-url`, `--pprof-base-url`, `kubectl`, `curl`, and `helm`. |
| `provider` | Records an operator-supplied aggregate-only parity comparison JSON. The harness rejects anything that contains package names, alert URLs, repository names, installation ids, or known token prefixes (`ghp_`, `github_pat_`, `glpat-`). | A JSON file containing `comparison_id` and a non-empty `totals` map of classification class to count. |

### Selecting phases

```bash
# offline defaults
scripts/security_intelligence_release_gate.sh \
  --image-tag-candidate v0.0.3-pre-release-9

# include the runtime phase against a remote Compose stack
scripts/security_intelligence_release_gate.sh \
  --phases state,focused,fixtures,runtime \
  --runtime-run-kind clean \
  --runtime-volume-proof ~/eshu/operator-only/clean-volume-proof.json \
  --api-base-url "$REMOTE_API_BASE_URL" \
  --api-key "$REMOTE_API_KEY" \
  --pprof-base-url "$REMOTE_PPROF_BASE_URL" \
  --image-tag-candidate v0.0.3-pre-release-9

# everything
scripts/security_intelligence_release_gate.sh --phases all \
  --runtime-run-kind preserved \
  --previous-runtime-evidence ~/eshu/operator-only/clean-runtime-evidence.json \
  --runtime-volume-proof ~/eshu/operator-only/preserved-volume-proof.json \
  --api-base-url "$REMOTE_API_BASE_URL" \
  --api-key "$REMOTE_API_KEY" \
  --pprof-base-url "$REMOTE_PPROF_BASE_URL" \
  --k8s-namespace eshu \
  --proof-matrix ~/eshu/operator-only/proof-matrix.json \
  --readback-proof ~/eshu/operator-only/readback-proof.json \
  --provider-compare ~/eshu/operator-only/parity-aggregate.json \
  --image-tag-candidate v0.0.3-pre-release-9
```

`$REMOTE_PPROF_BASE_URL` is the base of the separate pprof listener
(`ESHU_PPROF_ADDR`, typically a different host port than the API). Pass it for
runtime and Kubernetes release proof; missing pprof access fails those phases.
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
6. **Run the clean `runtime` phase.** It calls
   [Remote E2E Runtime State](remote-e2e-runtime-state.md) plus the
   supply-chain readback. Record the resulting `evidence.json`,
   `runtime-readback/`, `docker-stats.json`, normalized queue fields, Docker
   CPU/memory snapshot status, and pprof reachability. The command must set
   `--runtime-run-kind clean`, `--runtime-volume-proof`, and
   `--pprof-base-url`; missing Docker stats, pprof, volume proof, or terminal
   queue readback fails the phase because #1019 requires those evidence types.
7. **Run the `proof-matrix` phase.** Build the operator-local matrix from the
   representative run and pass it with `--proof-matrix`. A nonzero mismatch
   class without a public issue ref fails the phase; a matrix that stores
   private repository names, package names, provider URLs, alert URLs, tokens,
   or machine paths is rejected before it reaches evidence.
8. **Run API/MCP/CLI readback proof.** Drive the API, MCP tools, and CLI from
   the same release candidate and store the raw transcript outside the public
   repo. Use `scripts/e2e_readback_parity.sh --input <summary.json> --output
   <readback-proof.json>` to compare API/MCP/CLI truth, readiness, count,
   truncation, missing-evidence, unsupported, and ambiguity summaries for the
   same bounded checks. Pass the generated public-safe aggregate summary with
   `--readback-proof`. The gate requires API, MCP, and CLI surfaces to have
   `status: "pass"`, `checked > 0`, `failed: 0`,
   `transcript_status: "captured"`, and zero retry/failed/dead-letter counters.
9. **Run preserved-volume restart proof.** Stop the data-plane services
   without removing volumes, start them again, then re-run the `runtime`
   phase with `--runtime-run-kind preserved` and
   `--previous-runtime-evidence` pointing at the clean run's `evidence.json`.
   Also pass a preserved `--runtime-volume-proof` showing
   `restart_without_prune: true` and `same_as_clean: true` for each backing
   store. The harness records a public-safe summary of the prior clean proof
   and normalizes the preserved run's queue counts, retries, dead letters,
   Docker CPU/memory snapshot status, volume proof, and `index-status` health.
   Any new dead letter, stuck claim, missing stats snapshot, missing pprof,
   invalid volume proof, or invalid prior clean evidence fails the gate.
10. **Run the optional `provider` phase.** When operator-local provider data
   is available, generate the aggregate-only comparison outside the repo and
   pass the file to `--provider-compare`. The harness records only the
   aggregate counts and the synthetic comparison id.
11. **Run the `k8s` phase against the staging cluster.** Follow
   [Deploy To EKS](../deploy/eks/index.md) for cluster setup. Use a private
   port-forward for `--api-base-url` so the harness can read
   `/admin/status?format=json` and `/api/v0/index-status`; pass
   `--pprof-base-url` for the private pprof port-forward. The
   harness records sanitized pod/resource summaries, sanitized logs, sanitized
   Helm values, queue retry/dead-letter counts, terminal-status summary, and
   pprof reachability without storing private URLs, pod hostnames, IP
   addresses, provider URLs, repository names, package names, tokens, or
   machine-local paths.
12. **Review `evidence.md` and `evidence.json` together.** The gate is green
   only when `evidence.json` has `pass: true` at the top level and every
   enabled phase has `status` set to `pass` or `skipped`. Phases that fail
   (or that were enabled but produced an incomplete capture) record
   `status: fail` and an entry under `.failures[]`.

## Evidence schema

`evidence.json` carries `schema_version: 1`, `generated_at`, redacted
`repo_root`, `phases[]` (the list operators requested), `pass` (overall
boolean), and `failures[]` (per-phase failure messages). Each requested phase
populates a top-level key with at least `status` set to `pass`, `fail`, or
`skipped`. The runtime phase also surfaces `run_kind`, `previous_runtime` for
preserved proofs, `api_base_url` (the normalized value the gate actually used;
see below), `endpoints_failed`, normalized `index_status.queue` fields,
`queue_terminal_ok`, `docker_stats_status`, `volume_proof`, and per-endpoint
readback rows keyed by the documented `/api/v0/...` paths. The
`readback_proof` phase surfaces public-safe API/MCP/CLI surface counts,
transcript status, and queue counters. The `proof_matrix`
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

Runtime endpoint bodies are sanitized before they are written under
`runtime-readback/`, and `evidence.json` stores relative evidence references
rather than machine-local paths. Bearer tokens are passed to `curl` through a
short-lived mode-600 config file outside the evidence directory, not as raw
process arguments.

### Full E2E manifest input

The broader E2E suite uses a shared manifest contract that can feed the remote
Compose harness, readback parity gate, and Kubernetes gate. Validate it with:

```bash
scripts/verify_e2e_evidence_manifest.sh /secure/local/eshu/e2e-manifest.json
```

The manifest is not a raw transcript. It stores schema version `1`, run
identity, clean or preserved run kind, commit, image tag candidate, backend
identity, corpus mode, repository count, required ecosystem coverage, required
evidence-family coverage, runtime status, collector summaries, reducer
summaries, API/MCP/CLI readback counters, queue counters, pprof/log/resource
snapshot state, privacy status, and public follow-up issue refs.

Passing reducer rows must include `source_facts`, `reducer_facts`, and
per-row API/MCP readback pass evidence. Terraform/IaC relationship reducer
counts come from the reducer-owned relationship evidence tables, while
vulnerability matching uses the implemented
`reducer_supply_chain_impact_finding` fact kind unless a future dedicated
matching fact lands.

`status: "pass"` is reserved for clean evidence: every component row is
`pass`, API/MCP/CLI failures are zero, retrying/failed/dead-letter queue
counters are zero, pprof is reachable, and logs plus resource snapshots were
captured. Use `status: "partial"` or `status: "fail"` for unsupported,
skipped, or failed rows. Those classified rows must include a reason, which
keeps explicit gaps from being mistaken for clean coverage.

The validator rejects private-looking keys and values, including repository or
package fields, provider URLs, hostnames, machine paths, tokens, account ids,
raw transcripts, copied requests/responses, and provider payloads.

### Runtime volume proof input

The `runtime` phase accepts an operator-local JSON file with public-safe volume
state. Keep raw Docker commands, host paths, and volume ids outside the public
repo. The clean run proves the backing stores were reset before the run:

```json
{
  "schema_version": 1,
  "proof_id": "clean-volume-proof-v1",
  "run_kind": "clean",
  "clean_volume_state": "reset_before_run",
  "backing_stores": {
    "nornicdb_data": {"status": "pass", "before": "absent", "after": "present"},
    "postgres_data": {"status": "pass", "before": "absent", "after": "present"},
    "eshu_data": {"status": "pass", "before": "absent", "after": "present"}
  }
}
```

The preserved run proves the same backing stores survived restart without
`down -v` or volume pruning:

```json
{
  "schema_version": 1,
  "proof_id": "preserved-volume-proof-v1",
  "run_kind": "preserved",
  "previous_run_kind": "clean",
  "restart_without_prune": true,
  "backing_stores": {
    "nornicdb_data": {"status": "pass", "same_as_clean": true},
    "postgres_data": {"status": "pass", "same_as_clean": true},
    "eshu_data": {"status": "pass", "same_as_clean": true}
  }
}
```

### Readback proof input

The `readback-proof` phase accepts a public-safe aggregate of API, MCP, and CLI
readback. Raw transcripts must stay operator-local. Prefer generating this file
with `scripts/e2e_readback_parity.sh`, which rejects private-looking keys or
values, missing limits/timeouts, missing API/MCP/CLI surfaces, parity drift,
and empty results without a readiness state.

```json
{
  "schema_version": 1,
  "proof_id": "security-readback-proof-v1",
  "surfaces": {
    "api": {"status": "pass", "checked": 11, "failed": 0, "truncated": 1, "unsupported": 1, "missing_evidence": 1, "ambiguous": 1},
    "mcp": {"status": "pass", "checked": 11, "failed": 0, "truncated": 1, "unsupported": 1, "missing_evidence": 1, "ambiguous": 1},
    "cli": {"status": "pass", "checked": 11, "failed": 0, "truncated": 1, "unsupported": 1, "missing_evidence": 1, "ambiguous": 1}
  },
  "queue": {"retrying": 0, "failed": 0, "dead_letters": 0},
  "transcript_status": "captured"
}
```

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

- Provider comparison, proof-matrix, readback-proof, and runtime-volume-proof
  inputs are aggregate-only. Package names, advisory URLs, repository names,
  installation ids, hostnames, machine paths, raw transcripts, volume ids, and
  credential prefixes are rejected before they reach the evidence document.
- API readback uses `limit=1`; runtime bodies are sanitized before persistence
  so diagnostic files do not dump customer findings.
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
scripts/test-e2e-evidence-manifest.sh
```

`scripts/test-security_intelligence_release_gate_runtime.sh` proves
clean/preserved runtime identity, pprof, CPU/memory, queue-terminal, and
runtime-volume-proof handling. `scripts/test-security_intelligence_release_gate_k8s.sh`
proves the Kubernetes evidence contract with fake `kubectl`, `helm`, and
`curl` tools. It verifies sanitized logs, required pprof reachability, queue
retry/dead-letter readback, CPU and memory resource snapshots, and public-safe
generated Kubernetes evidence.

The fixture parity gate and the focused security-intelligence Go tests use
their existing per-package commands. The release gate harness wraps them so
the evidence stays in one place.
