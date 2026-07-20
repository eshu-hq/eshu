# All-Collector Readiness Proof Matrix

This page is the cross-collector proof gate for issue
[#3327](https://github.com/eshu-hq/eshu/issues/3327) (epic
[#3338](https://github.com/eshu-hq/eshu/issues/3338)). It records a current,
public-safe readiness classification for every collector lane across the five
readiness dimensions, plus the exact commands an operator runs to reproduce or
extend the proof.

It does **not** redefine readiness lanes or duplicate provider contracts. The
canonical lane vocabulary, per-collector contract, and reducer truth boundaries
live in [Collector And Reducer Readiness](collector-reducer-readiness.md); this
page cites that vocabulary and binds each lane to a proof outcome.

## Scope And Honesty Rules

- A collector is **not** promoted on binary presence, fixture tests, or
  explanation alone. Live promotion to `implemented` requires the
  [Promotion Proof](collector-reducer-readiness.md#promotion-proof) procedure.
- This matrix separates what was **proven live in this run** from what is
  **operator-gated** (needs cloud or provider credentials) and what is
  **foundation-only**. It never rounds a gated lane up to ready.
- Credential-backed lanes use operator-local configuration only. No secret
  values, tokens, account IDs, tenant IDs, registry hostnames, repository
  names, raw provider locators, or machine-specific paths appear here. Evidence
  is aggregate-only (counts, states, terminal queue depth).
- Cloud live-smoke promotion is tracked by dedicated operator-gated issues and
  is intentionally out of scope for this cross-collector gate:
  GCP [#1997](https://github.com/eshu-hq/eshu/issues/1997) /
  [#2644](https://github.com/eshu-hq/eshu/issues/2644), and
  Azure [#3066](https://github.com/eshu-hq/eshu/issues/3066).

## Five Dimensions

Each lane is judged across the five dimensions defined in
[Collector And Reducer Readiness → Five Dimensions Of A Lane](collector-reducer-readiness.md#five-dimensions-of-a-lane):

1. **Hosted runtime** — deployed collector binary plus chart/Compose path and
   operator-visible status.
2. **Reducer drain** — claimed work drains to zero with no dead letters.
3. **Graph truth** — the reducer materializes the intended graph/read-model
   shape.
4. **API/MCP truth** — read surfaces return materialized truth with the correct
   envelope and missing-evidence behavior.
5. **Console visibility** — the surface is represented without implying
   readiness it does not have.

## Proof Classes

Each lane row carries one proof class describing what this run established:

| Proof class | Meaning |
| --- | --- |
| `live-local` | Proven end to end on the default local Compose stack in this run, with cited commands and aggregate evidence. |
| `fixture-parity` | Fixture-to-runtime parity tests pass in this run; live provider proof is still required for promotion. |
| `operator-gated` | Runtime/reducer/readback code exists; live proof needs operator-local cloud or provider credentials. Reproduction command is listed. |
| `foundation-only` | Code structure exists; no hosted runtime, claim path, reducer projection, or chart yet. |
| `research-only` | Design or research only; no production code lane. |

A proof class is not a lane. The lane (the development-maturity claim) stays
governed by [Collector And Reducer Readiness](collector-reducer-readiness.md);
this column records only what this proof run could exercise without operator
secrets.

## Stack Provenance

Recorded for the live-local run below. Replace these values when you re-run the
proof; aggregate-only, no secrets or machine paths.

| Field | Value |
| --- | --- |
| Run date (UTC) | 2026-06-20 |
| Eshu commit | `701a4e51` (branch built as image tag `dev`) |
| Graph backend | NornicDB `v1.1.6` (`timothyswt/nornicdb-cpu-bge`, digest `sha256:e448ccf5…25692`) |
| Fact/queue store | `postgres:18-alpine` (`sha256:96d56f7f…db88`) |
| Stack | default `docker-compose.yaml` (NornicDB, Postgres, db-migrate, bootstrap-index, ingester, resolution-engine, API, MCP server) |
| Host toolchain | Go 1.26.4 darwin/arm64, Docker 29.4.0, OrbStack |
| Corpus | in-repo fixture ecosystems (`tests/fixtures/ecosystems`) |

## Recorded Local Run

The default Compose stack was built and started, the in-repo fixture corpus was
bootstrap-indexed, and the reducer drained to zero. This proves the **git lane**
end to end across all five dimensions on the hosted runtime path. Commands:

```bash
docker compose up -d --build
# bootstrap-index runs once over ESHU_FILESYSTEM_ROOT=/fixtures and exits 0
curl -fsS http://localhost:8080/healthz
curl -fsS http://localhost:8080/readyz
curl -fsS "http://localhost:8080/admin/status?format=json"            # public
curl -fsS -H "Authorization: Bearer $ESHU_API_KEY" \
  http://localhost:8080/api/v0/repositories                            # API readback
curl -fsS -H "Authorization: Bearer $ESHU_API_KEY" \
  http://localhost:8081/api/v0/repositories                            # MCP-server readback
# MCP JSON-RPC tool readback against the MCP server (POST /mcp/message)
```

`$ESHU_API_KEY` is the locally auto-generated, persisted key
(`ESHU_HOME/.env`); keep it out of committed artifacts.

| Dimension | Evidence (aggregate, redacted) | Result |
| --- | --- | --- |
| Hosted runtime | `bootstrap-index` exited `0`; API/MCP/resolution-engine healthchecks pass; `/healthz`+`/readyz` = `ok`; `admin/status` `health.state = healthy` ("no outstanding queue backlog"). | pass |
| Reducer drain | `fact_work_items`: 554 total, 554 `succeeded`, 0 `failed`, 0 `retrying`, 0 `dead_letter`, 0 outstanding; `graph_projection_phase_repair_queue` depth 0; projector stage `succeeded=45 dead_letter=0`. | pass |
| Graph truth | 45 repository scopes/generations active; reducer materialized 45 repositories from 4,723 committed `git` facts. | pass |
| API truth | `GET /api/v0/repositories` → 45 repositories; `GET /api/v0/index-status` → `repository_count: 45`, queue `succeeded=554` clean. | pass |
| MCP truth | MCP server `/healthz` ok; JSON-RPC `initialize` → `eshu-mcp-server`; `tools/call list_indexed_repositories` → `exact/fresh — Returned 45 result(s)`; MCP-server `/api/v0/repositories` → 45 (API parity). | pass |
| Console visibility | `admin/status.collector_promotion_proofs[git]`: `promotion_state: implemented`, `reducer_readback: available`, `claim_driven: false`, `observation_count: 4723`. | pass |

Run timestamp `admin/status.as_of`: `2026-06-20T20:08:04Z`. No coordinator was
started, so no claim-driven collector instance was present (expected for the git
ingester path); claim-driven lanes are exercised through the operator-gated
reproduction below.

## Fixture-Parity Ladder

Every collector package's fixture and parity tests were run in this proof:

```bash
cd go && go test ./internal/collector/... -count=1
```

Result: **470 packages `ok`, 0 `FAIL`** (1 package with no test files). This is
the `fixture-parity` evidence for every collector family below — it proves
parsing, normalization, redaction, and telemetry shaping against fixtures. Per
the canonical readiness contract, fixture parity is **not** a production-ready
claim; live provider proof is still required to promote a lane to `implemented`.

## Readiness Matrix

Lane = canonical development-maturity lane from
[Collector And Reducer Readiness](collector-reducer-readiness.md) (machine-checked
against the surface inventory). Proof class = what this run exercised without
operator secrets. "This-run result" summarizes the five dimensions
(runtime / reducer drain / graph / API+MCP / console). Credential gate names the
operator-local configuration required to take a lane to live promotion.

| Collector lane | Lane | Proof class | This-run result | Credential gate / reproduction |
| --- | --- | --- | --- | --- |
| git / repository | `implemented` | `live-local` | All five dimensions proven live (see [Recorded Local Run](#recorded-local-run)): runtime ok, drain clean (554/554), 45 repos graph+API+MCP, console `implemented`. | None. Default `docker compose up -d --build`. |
| documentation (Confluence) | `implemented` | `fixture-parity` | Fixture parity green; live runtime/readback need an Atlassian site. | `ESHU_CONFLUENCE_BASE_URL` + read-only `ESHU_CONFLUENCE_EMAIL`/`ESHU_CONFLUENCE_API_TOKEN` + bounded space selector. [Confluence smoke](local-testing/collector-live-smokes.md#confluence). |
| OCI registry | `implemented` | `fixture-parity` | Fixture parity green; live digest reads need a registry target. | Per-provider `ESHU_*_OCI_*` (ECR/JFrog need creds; GHCR/Docker Hub allow bounded anon). [OCI smokes](local-testing/collector-live-smokes.md#oci-registry-smokes). |
| Terraform state | `implemented` | `fixture-parity` | Fixture parity green; live collection needs a state object + redaction key. | `ESHU_TFSTATE_S3_*` or local state + `ESHU_TFSTATE_REDACTION_KEY`. remote-e2e `remote-e2e-terraform-state`. |
| AWS cloud | `implemented` | `operator-gated` | Fixture parity green; live collection needs a read-only AWS identity. | Read-only AWS workload identity; remote-e2e `remote-e2e-aws-cloud`. [Remote E2E](local-testing/remote-collector-e2e.md). |
| webhook / freshness | `implemented` | `fixture-parity` | Fixture parity green; listener runs locally, but trigger handoff proof needs a signed provider delivery. | `webhook-listener` profile + signed Git/AWS/PagerDuty/Jira sample. |
| package registry | `implemented` | `fixture-parity` | Fixture parity green; npm public endpoint reachable. A no-credential local proof is available via the [public-collector gate](local-testing/public-collector-proof.md). | None for public npm: `scripts/verify_local_public_collector_proof.sh`. JFrog feed needs `ESHU_JFROG_PACKAGE_*`. remote-e2e `remote-e2e-package-registry`. |
| SBOM / attestation | `implemented` | `fixture-parity` | Fixture parity green; live attachment needs a document target. | `ESHU_SBOM_ATTESTATION_DOCUMENT_URL` or remote-e2e fixture server `remote-e2e-sbom-attestation`. |
| vulnerability intelligence | `implemented` | `fixture-parity` | Fixture parity green. A live remote-E2E run is already recorded (2026-06-18, `promotion_state: implemented`) in [the canonical page](collector-reducer-readiness.md#vulnerability-intelligence-promotion-proof). KEV/EPSS/OSV also have a no-credential local proof via the [public-collector gate](local-testing/public-collector-proof.md). | KEV/EPSS/OSV public: `scripts/verify_local_public_collector_proof.sh`. NVD key-gated (`ESHU_NVD_API_KEY`). remote-e2e `remote-e2e-vulnerability-intelligence`. |
| provider security alerts | `implemented` | `operator-gated` | Fixture parity green; live needs a GitHub token + repo allowlist (preflight gates bad access). | `ESHU_SECURITY_ALERT_GITHUB_TOKEN` + `ESHU_SECURITY_ALERT_REPOSITORY`. remote-e2e `remote-e2e-security-alert`. |
| PagerDuty | `implemented` | `operator-gated` | Fixture parity green; live needs a PagerDuty token. | `ESHU_PAGERDUTY_LIVE=1` + token. [PagerDuty smoke](local-testing/collector-live-smokes.md#pagerduty). |
| Jira | `implemented` | `operator-gated` | Fixture parity green; live needs a Jira Cloud token. | `ESHU_JIRA_LIVE=1` + site/email/token/JQL. [Jira smoke](local-testing/collector-live-smokes.md#jira). |
| scanner worker | `implemented` | `fixture-parity` | Fixture parity green; live needs a configured analyzer target/mount. | Bounded `sbom_generation`/`os_package_extraction` target. remote-e2e `remote-e2e-scanner-worker-source`. |
| Grafana | `implemented` | `operator-gated` | Fixture parity green; live needs a Grafana URL + token. | `ESHU_GRAFANA_LIVE=1` + `ESHU_GRAFANA_BASE_URL`/`ESHU_GRAFANA_API_TOKEN`. [Grafana-stack smokes](local-testing/collector-live-smokes.md#grafana-stack-observability). |
| Prometheus / Mimir | `implemented` | `operator-gated` | Fixture parity green; live needs a Prometheus/Mimir URL. | `ESHU_PROMETHEUS_MIMIR_LIVE=1` + base URL (optional token/tenant). |
| Loki | `implemented` | `operator-gated` | Fixture parity green; live needs a Loki URL. | `ESHU_LOKI_LIVE=1` + base URL (optional token/tenant). |
| Tempo | `implemented` | `operator-gated` | Fixture parity green; live needs a Tempo URL. | `ESHU_TEMPO_LIVE=1` + base URL (optional token/tenant). |
| CI/CD runs | `partial` | `fixture-parity` | Fixture normalizer + reducer correlation green; hosted live target proof still pending (lane stays `partial`). | Bounded GitHub Actions allowlist (optional `GITHUB_TOKEN`). |
| GCP cloud | `gated` | `operator-gated` | Fixture parity green; sanitized live smoke pending. | Read-only GCP identity. Tracked by [#1997](https://github.com/eshu-hq/eshu/issues/1997) / security gate [#2644](https://github.com/eshu-hq/eshu/issues/2644). |
| Azure cloud | `gated` | `operator-gated` | Fixture parity green; sanitized live smoke pending. | Read-only Azure workload identity. Tracked by [#3066](https://github.com/eshu-hq/eshu/issues/3066). |
| Vault live (secrets/IAM) | `gated` | `operator-gated` | Fixture/parity green per package; live needs a read-only Vault. | `VAULT_ADDR` + read-only token. See [Vault read-only permissions](vault-secrets-iam-permissions.md). |
| semantic extraction | `gated` | `research-only` | No hosted provider lane; gated behind a provider profile. | `ESHU_SEMANTIC_PROVIDER_PROFILES_JSON` opt-in; not a deployed collector. |
| Kubernetes live | `foundation_only` | `foundation-only` | Lists a read-only core resource set + emits source facts; the reducer `kubernetes_correlation` domain and drift read model (`GET /api/v0/kubernetes/correlations`) have landed. Claim-driven runtime pending; the kubernetesLiveCollector Helm chart exists (off by default). | Correlation/drift read surface and the readiness-gated `RUNS_IMAGE` graph edge landed; claim-driven runtime pending. |

## Operator-Gated Reproduction

The lanes classified `operator-gated` above are reproduced with operator-local
credentials only. Keep all targets, tokens, account/tenant IDs, registry hosts,
repository names, and machine paths in a private env file outside the
repository. Public evidence records only aggregate counts, states, terminal
queue depth, and redaction spot-check results.

- **All-collector hosted Compose proof:** follow
  [Remote Collector E2E](local-testing/remote-collector-e2e.md). Bring up
  `docker-compose.remote-e2e.yaml` with the private env file, then run
  `scripts/verify_remote_e2e_runtime_state.sh` and the
  [Remote Compose Suite Harness](local-testing/remote-compose-suite-harness.md)
  to capture the aggregate, public-safe evidence manifest.
- **Per-provider live smokes:** use
  [Collector Live Smokes](local-testing/collector-live-smokes.md) for
  Confluence, Jira, PagerDuty, the Grafana stack (Grafana, Prometheus/Mimir,
  Loki, Tempo), OCI registry providers, and the JFrog package feed.
- **Security-intelligence proof matrix artifact:** build the public-safe
  aggregate matrix with
  `scripts/security_intelligence_release_gate.sh --phases proof-matrix --proof-matrix /secure/local/eshu/proof-matrix.json`
  against an operator-local `proof-matrix.json`, per
  [Security Intelligence Release Gate](security-intelligence-release-gate.md).

## Follow-Ups

This run found **no concrete broken collector path**: every collector package's
fixture/parity suite passed (470/470, 0 failures) and the git lane proved live
end to end. No promotion regression was observed.

The matrix surfaced one concrete coverage gap (an enhancement, not a break):
beyond git, no `implemented` lane had a no-credential, agent-runnable local live
proof, even though some lanes can collect against public endpoints. That gap
([#3347](https://github.com/eshu-hq/eshu/issues/3347)) is now closed by the
[No-Credential Public Collector Proof](local-testing/public-collector-proof.md):
`scripts/verify_local_public_collector_proof.sh` claim-drives the
workflow-coordinator against public, unauthenticated endpoints
(CISA KEV, FIRST EPSS, OSV, public npm) and asserts fact commit, reducer drain
to zero, and API/MCP readback with aggregate-only, public-safe output and no
operator credentials. NVD stays key-gated and is excluded.

Cloud live-smoke promotion remains operator-gated and tracked separately: GCP
[#1997](https://github.com/eshu-hq/eshu/issues/1997) /
[#2644](https://github.com/eshu-hq/eshu/issues/2644), Azure
[#3066](https://github.com/eshu-hq/eshu/issues/3066).

## Maintainer Details

Lane contracts and reducer truth boundaries live with the owning packages and
the canonical readiness page:

- [Collector And Reducer Readiness](collector-reducer-readiness.md)
- `go/internal/collector/README.md`
- `go/internal/workflow/README.md`
- `go/internal/reducer/README.md`
- `go/internal/query/README.md`
