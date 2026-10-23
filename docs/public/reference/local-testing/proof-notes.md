# Verification proof notes

## Performance Evidence Gate

`scripts/verify-performance-evidence.sh` blocks a PR that touches hot Cypher,
graph writes, queues, workers, leases, batching, or runtime knobs unless the
same PR carries its own tracked evidence in a fresh `Performance Evidence:`
(or `Benchmark Evidence:`/`No-Regression Evidence:`) line plus a fresh
`Observability Evidence:`/`No-Observability-Change:` line, in one of the
gate's recognized evidence-file locations. See
[Performance Evidence Gate](performance-evidence-gate.md) for
the added-lines requirement, the full list of recognized locations, and the
fixture/vendor/generated exclusions.

## Tagged Build Sweep

`go build`, `go vet` and `go test` all skip a file whose first line is a
`//go:build` constraint, so nothing in the ordinary lane ever compiles it. A
helper it calls can be deleted elsewhere in the package and the file simply
stops compiling, silently, for as long as nobody runs it with its tag on. That
is not theoretical: a live NornicDB proof in `internal/query` lost a helper to
an unrelated refactor and stayed uncompilable through a `make pre-pr` run, a
push, a full CI run, and eight review rounds.

`scripts/verify-tagged-builds.sh` closes that with one `go vet` per distinct
build constraint. It reads the constraints out of the files rather than from a
list, so a tag added later is swept without anyone remembering to add it:

```bash
bash scripts/verify-tagged-builds.sh                  # ./internal/query
bash scripts/verify-tagged-builds.sh --all            # every tagged package
bash scripts/verify-tagged-builds.sh ./internal/query ./cmd/reducer
```

`make pre-pr` runs `--all` whenever the diff touches Go, through the selected
exactness gates rather than a step of its own: the gate is registered as
`tagged-builds` at tier `pre-pr`, so `run-selected-gates.sh` picks it for any
`go/**` diff. CI runs the same pair — mirror then gate — as the
`Verify tagged-builds gate` row of `static-contract-gates.yml`, registered as
`tagged-builds` in `specs/ci-gates.v1.yaml`. Its trigger is `go/**` on purpose:
the change that breaks a tagged file is almost never a change to that file, so
a narrower trigger would reproduce the defect. The sweep is compile-only and
needs no backend, which is what separates it from the tagged suites themselves
— running those usually needs a pinned container, so they stay manual.

The gate accepts one term (optionally negated), terms joined only by `&&`, or
terms joined only by `||`, with no parentheses. Anything outside that — a
parenthesised group, a mix of `&&` and `||`, an unreadable term, or an
alternation that splits into fewer alternatives than its `||` promises — is an
`ERROR` that fails the run, never a skip and never a pass. A `SKIP` there would
be a green run over a file nothing compiled, which is the failure class this
gate exists to remove: `!(tag_a || tag_b)` used to have its parentheses
flattened to spaces, binding the `!` to the first term only, and answered `SKIP`
plus `PASS` without compiling the file either time.

Two shapes are legitimately reported as `SKIP` rather than vetted, and the
summary line prints the skip count so a sweep that is covering less than it
looks like is visible:

- a GOOS or GOARCH term (`windows`, `linux`). The go command takes those from
  the build environment, and forcing one with `-tags` pulls two copies of the
  standard library's platform files into one build. Those files are compiled by
  the ordinary build on their own platform.
- a constraint with no selectable tag left, which is what a negated one
  (`!nolocalllm`) reduces to. The default build already compiles that file.

The self-test builds throwaway modules and asserts both directions, including a
break behind a `&&` constraint that a first-token-only tag reader would miss:

```bash
bash scripts/test-verify-tagged-builds.sh
```

## Remote Collector E2E Compose Proof

Use [Remote collector E2E](remote-collector-e2e.md) when changing
`docker-compose.remote-e2e.yaml` or hosted collector recovery.

Before accepting a remote collector E2E run, also run the hosted runtime-state
gate in [Remote E2E Runtime State](../remote-e2e-runtime-state.md). It verifies
the API, MCP server, ingester, resolution engine, workflow coordinator, hosted
collectors, and checkpointed queue-zero signal.

Use [Remote Remediation Benchmark](remote-remediation-benchmark.md)
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

`scripts/run-k8s-two-team-governance-proof.sh` deploys the Helm chart to a local Kubernetes cluster,
provisions two teams' scoped tokens through a mounted read-only Secret, and tests cross-scope isolation
through the live API and MCP. Each team sees only its own repositories; the other team's repository is
absent; out-of-grant selectors return matching non-disclosing `403 permission_denied` or
`404 not_found` responses; unauthenticated requests return 401; and restricted NetworkPolicy egress is
applied. Cleanup runs on success and failure. `scripts/verify-hosted-governance-proof.sh` runs the good
and hostile fixture self-tests as part of the aggregate gate.

The chart hooks that enable this proof — `api.extraVolumes` /
`api.extraVolumeMounts` and the matching `mcpServer.*` values — are additive and
default to `[]`, so an operator that does not opt in renders a byte-identical
runtime. `deploy/helm/eshu/ci/governance-two-team-k8s.values.yaml` is test-only
and is not part of a shipped runtime profile.

No-Regression Evidence: the chart hooks are opt-in, empty-by-default Pod volume mounts. They add no
Cypher, graph write, worker claim, lease, batch, queue, or concurrency knob and do not change the
default-rendered Deployment runtime. A disposable single-node linux/amd64 Minikube v1.39.0 / Kubernetes
v1.37.0 proof kept the two team views isolated with API/MCP parity, returned 404 for out-of-grant
selectors and 401 for unauthenticated reads, applied four restricted NetworkPolicies, reached Ready,
and removed the namespace. This proves rendered policy resources and bounded application behavior in
that topology, not packet-level, managed-cluster, multi-node, arm64, or upgrade behavior. Scoped-token
authorization remains the unchanged graph/SQL exercised by the merged scoped-read suites.

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

Use [Discovery advisory](discovery-advisory.md) when a repository
is slow, unexpectedly large, or timeout-heavy. This is diagnostic evidence, not
a stable API contract.

## Demo Compose Stack Proof

Use `scripts/verify-demo-compose-answers.sh` to prove the credential-free demo
stack (`docker-compose.demo.yaml`) converges the corpus and answers the five
`specs/demo-first-answers.v1.yaml` questions over HTTP. It is failing-test-first
(red before the overlay exists, green after): it boots the stack with zero
credential env, asserts each question over HTTP with no `Authorization` header,
then runs `docker compose down -v --remove-orphans` and asserts zero leftover
containers, volumes, or networks. Two grep gates run before the boot: no
`:?`-required env var in any demo compose file, and no `*_TOKEN` /
external-provider `*_API_KEY` / cloud-credential env anywhere in the demo path.

No-Regression Evidence: the demo overlay changes no runtime behavior of the
existing services. It replays the same fixture corpus, cassette collectors, and
`bootstrap-index`/reducer/projector binaries the B-7 golden-corpus gate already
proves on every PR (`scripts/verify-golden-corpus-gate.sh`), on the pinned
NornicDB backend `timothyswt/nornicdb-cpu-bge:v1.1.9`. Baseline: the golden
gate's ~900s wall-clock budget over its 20-repo/17-collector corpus. The demo
runs a smaller manifest-declared subset (6 repos, 9 cassette collectors), and
the proof script drains every queue to terminal — its final maintenance pass
asserts zero `fact_work_items` residual and zero `shared_projection_intents`
non-terminal rows — before asserting the five answers. The concurrency knobs the
one-shot orchestrator sets (`ESHU_LISTEN_ADDR`/`ESHU_METRICS_ADDR` ephemeral
ports so its concurrent reducer and projector do not collide, plus a per-drain
settle) match the golden gate's own settings and affect only the orchestrator
container; no default-stack behavior changes.

No-Observability-Change: the demo overlay adds no new metrics, spans, or log
fields. It reuses the existing bootstrap-index, reducer, projector, API, and MCP
telemetry; orchestrator drain progress is plain stdout, and queue-drain state is
read from the existing `fact_work_items` and `shared_projection_intents` tables.

## Process Profiling

Use [Profiling and concurrency](profiling-and-concurrency.md)
for `ESHU_PPROF_ADDR`, concurrency knobs, and phase CPU capture.
