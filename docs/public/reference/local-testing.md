# Local Testing Reference

This page is the verification map for engineers and agents changing Eshu. For
first-time setup, use [Run Locally](../run-locally/index.md). For operator
checks, use [Operate Eshu](../operate/index.md) and
[Health Checks](../operate/health-checks.md).

Use the smallest gate that proves the touched behavior, then run the hygiene
checks required by the files you changed. Do not call work ready without citing
the commands you actually ran.

## Before You Push

Run the one-command local mirror of the CI build gate before opening or updating
a PR, so lint/build/test failures are caught in a single local pass instead of
across multiple CI rounds:

```bash
make pre-pr            # or: bash scripts/dev/pre-pr.sh
```

It runs gofumpt and golangci-lint over the **whole** module (catching
cross-package consequences a changed-package run misses, such as code that
becomes unused when a sibling package changes), `go build` and `go vet` over the
whole module, `go test` on the packages changed versus `origin/main`, plus the
500-line file cap and package-docs gates. Integration suites that need Postgres
or NornicDB are not run here — use the focused Compose gates below for those.

## Common Compose Environment

When running commands directly against the default local Compose stack:

```bash
export ESHU_GRAPH_BACKEND=nornicdb
export NEO4J_URI=bolt://localhost:7687
export NEO4J_USERNAME=neo4j
export NEO4J_PASSWORD=change-me
export DEFAULT_DATABASE=nornic
export ESHU_NEO4J_DATABASE=nornic
export ESHU_CONTENT_STORE_DSN=postgresql://eshu:change-me@localhost:15432/eshu
export ESHU_POSTGRES_DSN=postgresql://eshu:change-me@localhost:15432/eshu
```

For `docker-compose.neo4j.yml`, use `ESHU_GRAPH_BACKEND=neo4j` and database
`neo4j` instead.

## What To Run

| Change area | Use this page |
| --- | --- |
| Onboarding first-answer dogfood proof | [First five minutes benchmark](local-testing/first-five-minutes-benchmark.md) |
| Cross-surface answer-quality dogfood proof | [Answer Quality Scorecard](local-testing/answer-quality-scorecard.md) |
| Ask Eshu API + SSE + guardrail local proof | [Ask Eshu Local Proof](local-testing/ask-eshu-local-proof.md) |
| Remote all-collector Compose proof | [Remote collector E2E](local-testing/remote-collector-e2e.md) |
| Confluence, Jira, vulnerability source, and live registry smokes | [Collector live smokes](local-testing/collector-live-smokes.md) |
| Normal package, Compose, graph, Terraform-state, webhook, and docs gates | [Verification gates](local-testing/verification-gates.md) |
| Discovery report loop for noisy repositories | [Discovery advisory playbook](local-testing/discovery-advisory.md) |
| Worker knobs, pprof, and phase CPU profile capture | [Profiling and concurrency](local-testing/profiling-and-concurrency.md) |

## Quick Verification Matrix

| If you touched | Minimum verification |
| --- | --- |
| Answer-quality scorecard criteria, CLI, or docs | `cd go && go test ./internal/answerquality -count=1`, `cd go && go test ./cmd/eshu -run 'TestAnswerQualityScorecardCommand' -count=1`, and the docs build |
| Ask Eshu answer path, guardrail, or local proof | `scripts/test-verify-ask-eshu-local-proof.sh`, `scripts/verify-ask-eshu-local-proof.sh`, and the docs build (see [Ask Eshu Local Proof](local-testing/ask-eshu-local-proof.md)) |
| Competitive parity gate criteria, CLI, or docs | `cd go && go test ./internal/competitiveparity -count=1`, `cd go && go test ./cmd/eshu -run 'TestCompetitiveParity|TestRootCommandIncludesCompetitiveParity' -count=1`, `cd go && go run ./cmd/eshu competitive-parity validate --repo-root .. --json`, and the docs build |
| Portable evidence bundle schema, CLI, or docs | `cd go && go test ./internal/evidencebundle -count=1`, `cd go && go test ./cmd/eshu -run 'TestEvidenceBundle|TestRootCommandIncludesEvidenceBundle' -count=1`, and the docs build |
| Remote remediation benchmark wrapper | `bash scripts/test-verify-remote-e2e-remediation-benchmark.sh` |
| Docs, `CLAUDE.md`, `AGENTS.md`, or README files | `cd go && go run ./cmd/capability-inventory -mode docs` and `uv run --with mkdocs --with mkdocs-material --with pymdown-extensions mkdocs build --strict --clean --config-file docs/mkdocs.yml` |
| GitHub workflow or CodeQL setup guidance | `scripts/test-verify-codeql-setup.sh` and `scripts/verify-codeql-setup.sh` |
| CLI/runtime wiring | `cd go && go test ./cmd/eshu ./cmd/api ./cmd/mcp-server -count=1` |
| Pre-change impact or developer change plan API/MCP/CLI surface | `cd go && go test ./internal/query ./internal/mcp ./cmd/eshu -run 'TestDeveloperChangePlan|TestPreChangeImpact|TestChangePlan|TestFetchChangePlan|TestRunChangePlan|TestResolveRouteMapsDeveloperChangePlan|TestOpenAPIDeveloperChangePlan' -count=1` |
| Status/admin or completeness contract | `cd go && go test ./internal/status ./internal/query ./cmd/api -count=1` and `cd go && go vet ./internal/status ./internal/query ./cmd/api` |
| Replatforming plan, ownership-packet, or rollup API/MCP surface | `cd go && go test ./internal/mcp -run TestReplatforming -count=1` (see [Verification gates → Replatforming API/MCP parity proof](local-testing/verification-gates.md#replatforming-apimcp-parity-proof)) |
| Parser platform or collector snapshot flow | `cd go && go test ./internal/parser ./internal/collector/discovery ./internal/collector -count=1` |
| Terraform provider-schema evidence or relationship extraction | `cd go && go test ./internal/terraformschema ./internal/relationships ./internal/storage/postgres -count=1` |
| Parser, language-query, dead-code maturity, or relationship contribution docs | `scripts/verify-parser-relationship-kit.sh` plus the focused parser, query, relationship, or docs gate for the touched surface. |
| Compose, Helm, or deployable runtime shape | `cd go && go test ./cmd/api ./cmd/bootstrap-index ./cmd/ingester ./cmd/reducer -count=1` and `helm lint deploy/helm/eshu` |
| Argo CD or GitOps overlay rendered shape | `scripts/test-verify-gitops-rendered-diff-preflight.sh` and `scripts/verify-gitops-rendered-diff-preflight.sh --overlay deploy/argocd/overlays/aws --values values.private.yaml` |
| Hosted Helm install, upgrade, or rollback proof | `scripts/test-verify-hosted-helm-rollout-proof.sh`, `scripts/verify-hosted-helm-rollout-proof.sh --out-dir .proof/helm-install`, and `helm lint deploy/helm/eshu` |
| Hosted API/MCP auth, secret, or exposure posture | `scripts/test-verify-hosted-security-posture.sh`, `scripts/verify-hosted-security-posture.sh -f values.eshu.yaml`, and `helm lint deploy/helm/eshu -f values.eshu.yaml` |
| Hosted NetworkPolicy egress posture | `scripts/test-verify-hosted-network-policy-egress.sh`, `scripts/verify-hosted-network-policy-egress.sh -f values.eshu.yaml`, and `helm lint deploy/helm/eshu -f values.eshu.yaml` |
| Hosted governance local proof posture | `scripts/test-verify-hosted-governance-proof.sh`, `scripts/verify-hosted-governance-proof.sh`, and the docs build. This includes local no-policy governance status, no-provider semantic status, and no-provider semantic queue planning. |
| Hosted governance remote Compose proof | `scripts/test-verify-hosted-governance-remote-compose-proof.sh`, `scripts/verify-hosted-governance-remote-compose-proof.sh`, and `scripts/verify-hosted-governance-remote-compose-proof.sh --runtime` after the remote Compose stack is running |
| Hosted governance proof artifact | `scripts/test-verify-hosted-governance-proof-artifact.sh` and `scripts/verify-hosted-governance-proof-artifact.sh --input governance-proof.json --output-json governance-proof.summary.json --output-markdown governance-proof.summary.md` |
| Hosted governance Helm proof | `scripts/test-verify-hosted-governance-helm-proof.sh`, `scripts/verify-hosted-governance-helm-proof.sh --out-dir .proof/governance-helm --values values.eshu.yaml`, and `helm lint deploy/helm/eshu -f values.eshu.yaml` |
| Hosted governance negative leakage proof | `scripts/test-verify-hosted-governance-negative-leakage-proof.sh` and `scripts/verify-hosted-governance-negative-leakage-proof.sh --manifest leakage-proof.json --output-json leakage-proof.summary.json --output-markdown leakage-proof.summary.md` |
| Hosted auth audit and revocation proof | `scripts/test-verify-hosted-auth-audit-proof.sh` and `scripts/verify-hosted-auth-audit-proof.sh --input auth-audit-proof.json --output-json auth-audit-proof.summary.json --output-markdown auth-audit-proof.summary.md` |
| Hosted governance retention-state proof | `scripts/test-verify-hosted-governance-retention-proof.sh` and `scripts/verify-hosted-governance-retention-proof.sh --input retention-proof.json --output-json retention-proof.summary.json --output-markdown retention-proof.summary.md` |
| Hosted-growth Postgres fact and queue proof | `scripts/test-verify-hosted-growth-postgres-proof.sh` and `scripts/verify-hosted-growth-postgres-proof.sh --input hosted-growth-proof.json --output-json hosted-growth-proof.summary.json --output-markdown hosted-growth-proof.summary.md` |
| Hosted backup, restore, or graph-rebuild proof | `scripts/test-verify-hosted-backup-restore-proof.sh` and `scripts/verify-hosted-backup-restore-proof.sh --input restore-proof.json --output-json restore-proof.summary.json --output-markdown restore-proof.summary.md` |
| Compose-to-Kubernetes runtime parity | `scripts/test-verify-compose-helm-runtime-parity.sh`, `scripts/verify-compose-helm-runtime-parity.sh`, and `helm lint deploy/helm/eshu` |
| Hosted ops dashboard or alert pack | `scripts/test-verify-hosted-ops-alert-pack.sh`, `scripts/verify-hosted-ops-alert-pack.sh`, and `helm lint deploy/helm/eshu` |
| Accuracy golden gate (complexity, resolvers, correlation), or per-language cyclomatic complexity, cross-repo call resolvers, or correlation precision | `scripts/verify_accuracy_golden_gate.sh` (or `cd go && go test ./internal/accuracygate -count=1`); update `go/internal/accuracygate/testdata/baseline.json` only to raise a floor after a measured improvement (see [Accuracy Golden Gate](accuracy-golden-gate.md)) |
| Facts-first indexing, queue, or resolution flow | `cd go && go test ./internal/projector ./internal/reducer ./internal/storage/postgres -count=1` |
| Recovery, replay, or repair controls | `cd go && go test ./internal/recovery ./internal/runtime ./internal/status -count=1` |
| Hot-path Cypher, graph writes, queues, workers, leases, batching, or runtime knobs | `scripts/test-verify-performance-evidence.sh`, `scripts/verify-performance-evidence.sh`, `scripts/test-verify-query-plan-regression.sh`, and `scripts/verify-query-plan-regression.sh` |
| Graph backend query-plan fixture contract | `scripts/test-verify-query-plan-regression.sh` and `scripts/verify-query-plan-regression.sh` |
| Scale-lab representative corpus, privacy, metric, or threshold contract | `bash scripts/test-verify-scale-corpus-suite.sh` and `bash scripts/verify-scale-corpus-suite.sh` |
| Scale benchmark artifact, threshold, backend, commit, or before/after proof contract | `bash scripts/test-verify-scale-benchmark-artifact.sh` and `bash scripts/verify-scale-benchmark-artifact.sh` |
| B-7 golden end-to-end corpus gate (any pipeline phase: collector/parser/projector/reducer/query/storage, the B-10 cassettes, or the B-12 snapshot) | `cd go && go test ./cmd/golden-corpus-gate -count=1` and `bash scripts/test-verify-golden-corpus-gate.sh` (unit + static contract); `bash scripts/verify-golden-corpus-gate.sh` for the full live run (needs Docker; runs bootstrap + B-10 cassettes + reducer drain and diffs the B-12 snapshot — see [Golden Corpus Gate](local-testing/golden-corpus-gate.md)) |
| New collector family, provider, scanner, or hosted collector runtime | `scripts/test-verify-collector-authoring-gate.sh` and `scripts/verify-collector-authoring-gate.sh` |
| Generated collector entrypoint manifest or generated collector command files | `scripts/test-verify-collector-entrypoints-generated.sh` and `scripts/verify-collector-entrypoints-generated.sh` |
| Skillgen roundtrip baseline (`skill-fragments/`, `expected/`, `go/cmd/skillgen/`, `go/internal/extensions/skillgen/`, `specs/surface-inventory.v1.yaml`, or the gate script itself) | `cd go && go build ./cmd/skillgen/...` and `bash scripts/test-verify-skill-roundtrip.sh` plus `bash scripts/verify-skill-roundtrip.sh` (and the docs build when the matrix or skillgen doc changes) |
| Evidence-continuity matrix, evidence-centric capability rows, or API/MCP proof coverage | `scripts/test-verify-evidence-continuity.sh`, `scripts/verify-evidence-continuity.sh`, and `cd go && go test ./internal/evidencecontinuity -count=1` |
| New or changed Go package under `go/internal` or `go/cmd` | `scripts/test-verify-package-docs.sh` and `scripts/verify-package-docs.sh` |
| New or changed telemetry registration, pipeline stage, or `docs/public/observability/telemetry-coverage.md` row (the X2 coverage gate) | `scripts/test-verify-telemetry-coverage.sh` and `scripts/verify-telemetry-coverage.sh` (and the docs build when the X1 doc changes) |
| New or changed operator dashboard, dashboard panel, or `docs/public/observability/dashboards/eshu-operator-overview.json` (the X4 dashboard generator) | `scripts/test-generate-operator-dashboard.sh` and `scripts/generate-operator-dashboard.sh` (re-generates the committed dashboard JSON; the test mirror asserts the committed artifact matches) |
| Go source, comments, package contracts, or generated docs | `cd go && golangci-lint run ./...` |
| Root marketing site (Cloudflare Pages) | `npm test` (unit) plus `npm run site:review` (desktop + mobile browser gate documented in the repo-root `CLOUDFLARE_PAGES.md`) |
| Repo hygiene gates | `git diff --check` |

## Remote Collector E2E Compose Proof

Use [Remote collector E2E](local-testing/remote-collector-e2e.md) when changing
`docker-compose.remote-e2e.yaml` or hosted collector recovery.

Before accepting a remote collector E2E run, also run the hosted runtime-state
gate in [Remote E2E Runtime State](remote-e2e-runtime-state.md). It verifies
the API, MCP server, ingester, resolution engine, workflow coordinator, hosted
collectors, and checkpointed queue-zero signal.

Use [Remote Remediation Benchmark](local-testing/remote-remediation-benchmark.md)
to rerun the known CVE/package to owner/remediation packet proof and capture
public-safe wall time, queue, fact-count, graph-write, and API/MCP parity
artifacts.

## Secrets/IAM Activation Proof

No-Regression Evidence: issue #2430 remote-validation proof on 2026-06-16
against NornicDB with data-plane schema bootstrapped first. Baseline live
writer conformance failed scoped retract with
`SecretsIAMServiceAccount survived retract: count = 1`. After changing
secrets/IAM graph retracts from list/`UNWIND` mutation predicates to one
scalar `scope_id` cleanup statement per label/scope and executing retracts
sequentially, `ESHU_SECRETS_IAM_GRAPH_LIVE=1 ESHU_GRAPH_BACKEND=nornicdb go
test ./internal/storage/cypher -run '^TestSecretsIAMGraphWriterLiveConformance$'
-count=1 -v` passed in 0.066s. Sanitized readback counted all four
`SecretsIAM*` node families and all five `SECRETS_IAM_*` relationship families
at one before retract, and the sensitive-property spot check reported
`suspicious_values=0`. The same target passed the focused reducer, cypher
storage, and reducer command packages, and the flag-on reducer startup emitted
the `secrets/IAM graph projection ENABLED` warning and reached the normal worker
startup log after the projection truth contract moved from output-only
`canonical_asset` to source evidence `observed_resource`.

No-Observability-Change: the activation fix adds no metric name, metric label,
worker, queue domain, runtime knob, backend branch, or graph-write route.
Secrets/IAM graph writes and retracts still flow through
`SecretsIAMGraphWriter` statement metadata with `phase=secrets_iam_graph`,
entity labels, existing executor error wrapping, existing graph-write
spans/metrics, and the existing reducer flag-on warning.

## Helm Workspace Setup PVC Retry Proof

No-Regression Evidence: baseline chart rendering ran `workspace-setup` as root
with `drop: ALL`, copied `.eshuignore` directly to the final PVC path, and then
ran `chown`, which failed on the default persistent-volume smoke and was not
retry-safe after the final file existed. The fixed render runs as UID/GID
`10001`, keeps `drop: ALL` with no added capabilities, creates `/data/.eshu`
and `/data/repos`, and replaces `.eshuignore` through a temp file on the same
data mount before `mv -f`. Verification covered
`go test ./internal/runtime -run TestHelmWorkspaceSetupInitIsPersistentVolumeRetrySafe -count=1`,
`go test ./internal/runtime -count=1`, `helm lint ./deploy/helm/eshu`,
Compose/Helm runtime parity, and a remote Docker proof on linux/amd64 using the
runtime base image with `--cap-drop ALL`, `--read-only`, UID/GID `10001`, and
the same persisted data/config/tmp mount shape; both first and retry setup
reported `ok`. Terminal queue and row counts are not applicable because this
change runs before any Eshu process starts.

No-Observability-Change: the setup change adds no metric, span, structured log,
status field, queue, graph write, worker, lease, batch, or runtime data
contract. Operators diagnose it through Kubernetes init-container state, pod
events, and the existing ingester probes after startup.

## Two-Team K8s Governance Proof

`scripts/run-k8s-two-team-governance-proof.sh` deploys the Helm chart to a local
Kubernetes cluster (OrbStack), provisions two teams' scoped tokens via a mounted
read-only Secret, and asserts cross-scope isolation live through the API and MCP:
each team sees only its own repositories, the other team's repository is absent,
out-of-grant single-repo selectors return 403, unauthenticated requests return
401, and the restricted NetworkPolicy egress is applied. `helm uninstall` plus
namespace delete run on success and failure. `scripts/verify-hosted-governance-proof.sh`
runs the verifier self-test (good plus bad fixtures) as part of the aggregate
gate.

The chart hooks that enable this proof — `api.extraVolumes` /
`api.extraVolumeMounts` and the matching `mcpServer.*` values — are additive and
default to `[]`, so an operator that does not opt in renders a byte-identical
runtime. `deploy/helm/eshu/ci/governance-two-team-k8s.values.yaml` is test-only
and is not part of a shipped runtime profile.

No-Regression Evidence: the chart hooks are opt-in, empty-by-default Pod volume
mounts; they add no Cypher, graph write, worker claim, lease, batch, queue, or
concurrency knob and do not change the default-rendered Deployment runtime. Live
proof on OrbStack Kubernetes v1.34.8 (single node): two-team scoped reads stay
isolated (each team count=1, other team's repo absent, API/MCP parity),
out-of-grant selector 403, unauthenticated 401, NetworkPolicy restricted egress
applied; all pods reached Ready and the namespace was torn down clean. The
scoped-token authorization itself is the unchanged graph/SQL already exercised by
the merged scoped-read suites.

No-Observability-Change: the proof reads existing spans, metrics, status, and the
documented `/api/v0/repositories` and MCP responses; no telemetry, metric label,
span, or status field is added or altered by the chart hooks.

No-Regression Evidence: bundled NornicDB Helm render proof on Kubernetes 1.32
showed the Deployment preserves the pinned image entrypoint, sets the
`NORNICDB_ADDRESS` wildcard bind address, and exposes the charted HTTP and Bolt
ports through the Service. A Linux amd64 Docker proof with the same pinned
backend image and entrypoint-preserving environment reached HTTP health and
accepted a Bolt TCP connection through published ports. This changes only the
Kubernetes startup contract for the bundled graph backend; it does not change
Eshu queue workers, graph query text, reducer batching, or API/MCP read paths.

No-Observability-Change: the bundled NornicDB chart fix keeps the existing HTTP
health probes, named `http` and `bolt` container ports, and Service targetPorts.
Operators still diagnose the path through pod readiness, container logs, Service
endpoints, and the existing graph-backed Eshu readiness checks.

## Discovery Advisory Playbook

Use [Discovery advisory](local-testing/discovery-advisory.md) when a repository
is slow, unexpectedly large, or timeout-heavy. This is diagnostic evidence, not
a stable API contract.

## Process Profiling

Use [Profiling and concurrency](local-testing/profiling-and-concurrency.md)
for `ESHU_PPROF_ADDR`, concurrency knobs, and phase CPU capture.

## Docs And Hygiene

Docs, `CLAUDE.md`, `AGENTS.md`, and README changes require:

```bash
uv run --with mkdocs --with mkdocs-material --with pymdown-extensions \
  mkdocs build --strict --clean --config-file docs/mkdocs.yml
git diff --check
```
