# Scripts

This directory holds local verification and helper scripts for Eshu maintainers.
Most scripts assume they are run from a fresh checkout with Go, Docker,
Postgres client tools, and `rg` available.

Use `install-local-binaries.sh` when you need the full local binary set on
`PATH` with the same names Eshu expects at runtime: `eshu`, `eshu-api`,
`eshu-mcp-server`, `eshu-ingester`, `eshu-reducer`, and the supporting helper
binaries.

`install-local-binaries.sh` builds only the local owner `eshu` binary with
`ESHU_LOCAL_OWNER_BUILD_TAGS=nolocalllm` by default so local-authoritative mode
embeds NornicDB in the owner process. The service binaries are built plainly,
matching deployment mode. Set `ESHU_LOCAL_OWNER_BUILD_TAGS=` only when you
intentionally want a plain local owner for explicit process-mode testing.

Set `ESHU_VERSION=<version>` to embed a specific version string. The script
defaults to `dev`. Every installed Eshu binary accepts `--version` and `-v`;
service binaries answer before opening telemetry, Postgres, graph, queues, or
listeners, so the check is safe in local scripts and container probes.

The `verify_*_compose.sh` scripts are developer and DevOps proof lanes. They
start their own Compose project, choose ports, and tear the stack down unless
`ESHU_KEEP_COMPOSE_STACK=true` is set.

`verify-hosted-helm-rollout-proof.sh` creates the public-safe proof artifact for
hosted Helm install, upgrade, and rollback work. It renders, lints, runs a Helm
dry-run, summarizes required workloads and schema bootstrap, optionally captures
API/MCP readback, and fails upgrade or rollback modes when durable-state,
queue-state, Postgres restore, or graph rebuild assumptions are missing. The
mocked harness is `test-verify-hosted-helm-rollout-proof.sh`.

`verify-compose-helm-runtime-parity.sh` is the static hosted deployability
gate for Compose-to-Kubernetes runtime parity. It renders default Compose,
remote E2E Compose, profile-expanded remote collectors, observability remote
collectors, and Helm with ServiceMonitors enabled, then fails on missing
services, critical env wiring, health or metrics probes, Helm core workloads,
or collector ServiceMonitor template coverage. The mocked harness is
`test-verify-compose-helm-runtime-parity.sh`.

`verify-hosted-security-posture.sh` renders the Helm chart and fails closed on
inline credential-shaped env vars, missing API auth Secret refs, empty
credential Secret refs, public pprof binding, and public API docs without an
explicit verifier opt-in. Use `test-verify-hosted-security-posture.sh` for the
mutation harness.

`verify-hosted-network-policy-egress.sh` renders the Helm chart and verifies
hosted NetworkPolicy egress posture. It flags broad egress as a governance risk
and proves restricted mode for DNS, datastore, graph, internal-service,
collector-provider, semantic-provider, and extension destination classes. Use
`test-verify-hosted-network-policy-egress.sh` for the mutation harness.

`verify_remote_e2e_runtime_state.sh` is the post-start gate for the hosted
remote collector E2E stack. It does not start containers. Run it after the
remote Compose stack is up to prove the API, MCP server, ingester,
resolution engine, workflow coordinator, and hosted collectors are healthy, and
the checkpointed runtime state is acceptable, before treating the run as
deployable proof. Smoke and full-corpus modes check finite proof safety
separately from continuous polling; representative mode accepts only the scoped
terminal contract documented in the remote E2E guide so scheduled collectors do
not make the inner-loop proof nondeterministic. API probes use a bounded
max-time and keep bearer tokens out of process arguments. It also
prints aggregate package, advisory, impact, security-alert, SBOM, and image
identity counts when those probes are required. Use
`ESHU_REMOTE_E2E_COMPOSE_FILES` as a colon-separated Compose file list and
`ESHU_REMOTE_E2E_ENV_FILE` when the stack uses a private env file.

`verify-gitops-rendered-diff-preflight.sh` renders the Helm chart with Argo CD
overlay value files before a GitOps controller syncs them. It fails on
placeholder rendered values, unpinned image tags, chart-invalid exposure or
schema-bootstrap combinations, and claim-driven collectors without an active
workflow coordinator. The output is a redacted resource summary suitable for CI
evidence. Use `test-verify-gitops-rendered-diff-preflight.sh` for the focused
harness.

`verify-hosted-backup-restore-proof.sh` validates the public-safe hosted
backup, restore, and graph-rebuild proof packet. It fails closed on stale or
missing backup evidence, partial restore, graph rebuild without preserved
Postgres facts, parity drift, nonzero queue terminal state, missing API/MCP
readback, and private-data-shaped fields. It writes sanitized JSON and Markdown
summaries only. Use `test-verify-hosted-backup-restore-proof.sh` for the
mocked harness.

`verify-hosted-ops-alert-pack.sh` validates the hosted operations dashboard and
alert pack. It checks Grafana dashboard JSON, required panels, standalone and
Prometheus Operator alert parity, required runbook annotations, bounded label
usage, and Helm ServiceMonitor render shape. Use
`test-verify-hosted-ops-alert-pack.sh` for the mutation harness.

`remote-e2e-corpus-preflight.sh` is the one-shot corpus guard used by
`docker-compose.remote-e2e.yaml`. Smoke mode is fixture-friendly, representative
mode is the 20-50 repository inner-loop gate, and full mode is the release
gate. `test-remote-e2e-corpus-preflight.sh` covers those bounds without
requiring Docker.

`remote-e2e-scanner-sbom-preflight.sh` validates the scanner-worker SBOM mount
before `workflow-coordinator` can plan scanner claims. Its harness is
`test-remote-e2e-scanner-sbom-preflight.sh`.

`e2e_remote_compose_suite.sh` builds the public-safe remote Compose evidence
manifest from live aggregate collector, reducer, readback, runtime, pprof, log,
and volume-proof inputs. It fails closed when reducer rows have source and
reducer counts but no API/MCP readback proof. The mocked harness tests are
`test-e2e-remote-compose-suite.sh` and
`test-e2e-remote-compose-reducer-manifest.sh`.

`verify-performance-evidence.sh` is the CI tripwire for hot-path runtime
changes. It inspects the actual PR diff, including brand-new collector
packages, and fails when changed Go code introduces Cypher, graph writes,
worker claims, leases, batching, or concurrency behavior, or when Compose/Helm
runtime config changes touch graph backend, worker, batching, timeout, pprof,
or NornicDB knobs without a tracked docs/reference/package note containing both
benchmark evidence and observability evidence markers.

`verify-package-docs.sh` is the CI tripwire for package-local documentation.
Any changed Go package under `go/internal` or `go/cmd` must already have
`doc.go`, `README.md`, and `AGENTS.md`; new collectors and runtime packages
cannot land without the code-level context future agents and reviewers need.

`verify-okta-saml-live-proof.sh` turns an operator-local Okta SAML proof
manifest into public-safe JSON and markdown summaries. The manifest can name
only provider source classes, public SAML API paths, aggregate login/denial
counts, role names, decision families, timing classes, and required pass/fail
proof steps. Raw Okta org URLs, app IDs, metadata XML, certificates, users,
group values, SAML assertions, attributes, cookies, provider responses, and
audit bodies must remain outside the repository and public GitHub text. The
mocked harness test is `test-verify-okta-saml-live-proof.sh`.

`verify-okta-oidc-live-proof.sh` turns an operator-local Okta OIDC proof
manifest into public-safe JSON and markdown summaries. The manifest can name
only provider source classes, public OIDC API paths, aggregate login, denial,
refresh, and revocation counts, role names, decision families, timing classes,
and required pass/fail proof steps. Raw Okta org URLs, app IDs, client secrets,
users, groups, OIDC tokens, cookies, provider responses, and audit bodies must
remain outside the repository and public GitHub text. The mocked harness test
is `test-verify-okta-oidc-live-proof.sh`.

`security_intelligence_release_gate.sh` aggregates the proofs required before
cutting the next prerelease image with vulnerability or security-intelligence
work. By default it runs the offline phases (state capture, focused Go tests,
synthetic parity fixtures) and emits a single `evidence.json` plus markdown
summary. The `proof-matrix`, `runtime`, `readback-proof`, `k8s`, and `provider` phases are
operator-opted into and never persist private data. The proof-matrix phase
accepts only aggregate representative-corpus coverage, readback counters, and
public follow-up issue refs, and requires captured CPU/memory, logs, and pprof.
The runtime phase requires an explicit clean or preserved run kind, sanitized
readback, pprof, CPU/memory, queue-terminal state, and aggregate volume proof;
preserved runs also point at the prior clean evidence packet. The
`readback-proof` phase accepts only aggregate API/MCP/CLI status and queue
counters. Use `e2e_readback_parity.sh` to turn operator-local bounded API, MCP,
and CLI result summaries into that public-safe proof; raw transcripts stay
outside the repository. The companion test harnesses
`test-e2e-readback-parity.sh`,
`test-security_intelligence_release_gate.sh` and
`test-security_intelligence_release_gate_proof_matrix.sh`,
`test-security_intelligence_release_gate_runtime.sh`, and
`test-security_intelligence_release_gate_k8s.sh` cover the offline phases and
mocked runtime/k8s evidence against synthesized repos. The Kubernetes fixture
also covers ServiceMonitor, NetworkPolicy, PodDisruptionBudget,
schema-bootstrap Job, Helm manifest, pprof, queue, log, and resource snapshot
evidence. The runbook lives at
`docs/public/reference/security-intelligence-release-gate.md`.
