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

This page is the operator runbook for issue
[#657](https://github.com/eshu-hq/eshu/issues/657). The detail behind each
sub-proof lives in:

- [Security Intelligence](security-intelligence.md)
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

The gate is intentionally:

- **Runbook + harness.** The harness emits a structured evidence document; the
  runbook tells operators which phases to run and what to capture.
- **Privacy-aware.** No private repository names, package names, alert URLs,
  CVE descriptions, or copied provider payloads are recorded. The harness
  rejects provider-comparison inputs that look like private data.
- **Image-cut neutral.** The gate produces evidence only. Image push and chart
  release stay with the normal Eshu release workflow once this gate is green.

## What the gate covers

| Acceptance criterion | Where it is proven |
| --- | --- |
| Clean-volume remote Compose proof with API, MCP, ingester, reducer, coordinator, package-registry, vulnerability-intelligence, SBOM-attestation, scanner-worker, security-alert path, and backing stores | `runtime` phase plus [Remote E2E Runtime State](remote-e2e-runtime-state.md) and [Remote Collector E2E](local-testing/remote-collector-e2e.md) |
| Preserved-volume restart proof and no stale queue, duplicate claim, dead-letter, or startup regression | `runtime` phase re-run after restarting data-plane services on the same volumes |
| API and MCP readback for supply-chain impact, readiness, explanation, advisory evidence, security-alert reconciliations, SBOM attachments, container-image identities, priority, suppression, and exports where applicable | `runtime` phase API readback against the documented endpoints |
| Vulnerability parity gate against synthetic fixtures and optional operator-local provider comparison with only aggregate/private-safe mismatch classes | `fixtures` phase plus optional `provider` phase |
| Pprof availability, effective env, queue counts, retries, dead letters, target counts, fact counts, wall times, CPU, memory, freshness states | `runtime` phase docker stats snapshot, `/api/v0/index-status` payload, and pprof reachability check |
| Kubernetes / EKS proof with pprof, logs, queue telemetry, and resource snapshots before declaring release readiness | `k8s` phase plus [Deploy To EKS](../deploy/eks/index.md) |
| Exact commit, image tag candidate, NornicDB tag/commit, clean-volume state, schema/bootstrap state, and pass/fail evidence | `state` phase, always offline |

## Harness

The harness lives at `scripts/security_intelligence_release_gate.sh`. It runs
the offline phases by default; the runtime, k8s, and provider phases are
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
| `runtime` | Wraps `scripts/verify_remote_e2e_runtime_state.sh`, calls the documented supply-chain endpoints with `limit=1`, captures `docker stats --no-stream` for the running services, and optionally probes pprof when `--pprof-base-url` is provided (pprof rides a separate listener on its own host port; without an explicit URL the gate records `pprof_status: unchecked`). Any endpoint readback error, a missing verifier script, or a verifier non-zero exit fails the phase. | A running remote Compose stack. `--api-base-url` is required; `--api-key` is required when the stack uses an explicit bearer token. |
| `k8s` | Captures `kubectl get pods -o json`, `kubectl top pods`, and `helm get values` for the target release. Any of those commands failing (auth, missing namespace, no metrics-server, missing helm) is recorded as a phase failure so the gate cannot report green without resource snapshots. | `--k8s-namespace`, `kubectl`, and `helm` (`helm` is required because rendered values are the documented resource snapshot). |
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
  --provider-compare ~/eshu/operator-only/parity-aggregate.json \
  --image-tag-candidate v0.0.3-pre-release-9
```

`$REMOTE_PPROF_BASE_URL` is the base of the separate pprof listener
(`ESHU_PPROF_ADDR`, typically a different host port than the API). Omit the
flag to mark `pprof_status: unchecked` rather than probe the API URL.

## Sequence operators follow

1. **Capture state offline.** From the release candidate commit, run the
   harness with the offline defaults plus `--image-tag-candidate`. Confirm the
   schema migration count and NornicDB digest match the values the deployment
   will run.
2. **Run the fixture parity gate.** The `fixtures` phase covers the
   synthetic suite. Mismatches must be classified before continuing.
3. **Bring up the remote Compose stack with a clean volume.** Follow
   [Remote Collector E2E](local-testing/remote-collector-e2e.md). Use the
   `.env.remote-e2e.example` defaults or your full-corpus profile.
4. **Run the `runtime` phase.** It calls
   [Remote E2E Runtime State](remote-e2e-runtime-state.md) plus the
   supply-chain readback. Record the resulting `evidence.json`,
   `runtime-readback/`, `docker-stats.json`, and pprof reachability.
5. **Run preserved-volume restart proof.** Stop the data-plane services
   without removing volumes, start them again, then re-run the `runtime`
   phase. Compare workflow run counts, queue counts, retries, dead letters,
   and `index-status` health between the two runs. Any new dead letter or
   stuck claim fails the gate.
6. **Run the optional `provider` phase.** When operator-local provider data
   is available, generate the aggregate-only comparison outside the repo and
   pass the file to `--provider-compare`. The harness records only the
   aggregate counts and the synthetic comparison id.
7. **Run the `k8s` phase against the staging cluster.** Follow
   [Deploy To EKS](../deploy/eks/index.md) for cluster setup. The harness
   captures pod, top, and Helm values snapshots; operators capture pprof and
   logs through a port-forward (never through the public service).
8. **Review `evidence.md` and `evidence.json` together.** The gate is green
   only when every enabled phase has `status: pass` (or `skipped: true`
   with an explicit reason) and `pass: true` at the top level.

## Privacy boundary

The harness intentionally records only public-safe data. In particular:

- Provider comparison inputs are aggregate-only. Package names, advisory URLs,
  repository names, installation ids, and credential prefixes are rejected
  before they reach the evidence document.
- API and MCP readback uses `limit=1` so response bodies stay small and
  diagnostic, not a dump of customer findings.
- Operator-local artefacts (private corpora paths, AWS account ids, GitHub
  installation ids) are not echoed by the gate; they live in the operator's
  own env file.

## Verification

`scripts/test-security_intelligence_release_gate.sh` proves the offline phases
against a synthesized repo: missing `Chart.yaml`, unknown phase names, private
data in provider comparison, valid aggregate-only provider comparison, the
captured state fields, and the markdown summary. Run it before changing the
harness.

```bash
scripts/test-security_intelligence_release_gate.sh
```

The fixture parity gate and the focused security-intelligence Go tests use
their existing per-package commands. The release gate harness wraps them so
the evidence stays in one place.
