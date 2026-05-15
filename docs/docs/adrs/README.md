# ADR Status Tracker

This folder keeps the decision history for Eshu. The ADRs are intentionally
more detailed than the public docs because they record evidence, rejected
hypotheses, and trade-offs that would slow down a reader who only wants to run
the platform.

Use this page as the starting point when deciding what is complete and what
still needs an owner.

## Completed Or Closed

| ADR | Current state | Notes |
| --- | --- | --- |
| `2026-04-17-neo4j-deadlock-elimination-batch-isolation.md` | Complete | Readiness, repair, and bounded write contracts are implemented. |
| `2026-04-17-semantic-entity-materialization-bottleneck.md` | Implemented; follow-up moved | Acceptance-unit semantic work landed. Remaining semantic throughput work moved to the reducer/NornicDB ADR. |
| `2026-04-18-bootstrap-relationship-backfill-quadratic-cost.md` | Implemented with follow-up | Quadratic bootstrap backfill fix is closed; only automatic replay for the narrow reopen-straggler window remains. |
| `2026-04-18-e2e-validation-atomic-writes-deferred-backfill.md` | Historical validation | The implementation landed; newer full-corpus proof supersedes this acceptance run. |
| `2026-04-18-reducer-full-convergence-optimization.md` | Superseded | Replaced by the broader reducer/NornicDB throughput workstream. |
| `2026-04-20-pre-merge-validation-local-mcp-and-iac-chart-parity.md` | Superseded | Old pre-merge gate for a prior branch; current public checks live in CI and local-testing docs. |
| `2026-04-28-reducer-throughput-and-nornicdb-concurrency-plan.md` | Implemented with follow-up | PR #129 workstream is closed against the 896-repo NornicDB proof; release/pin and maintainability follow-ups continue separately. |
| `2026-05-03-compose-telemetry-overlay-and-documentation-standards.md` | Accepted | Default Compose is runtime-only; telemetry is opt-in through the overlay. |

## Accepted With Follow-Up

| ADR | Current state | What remains |
| --- | --- | --- |
| `2026-04-18-query-time-service-enrichment-gap.md` | Accepted with follow-up | Finish full service-query parity and deployment-mapping response shape. |
| `2026-04-19-ci-cd-relationship-parity-across-delivery-families.md` | Accepted with follow-up | Finish partial delivery-family parity and controller-driven service-story integration. |
| `2026-04-19-deployable-unit-correlation-and-materialization-framework.md` | Accepted with follow-up | Continue full admission/materialization across multi-source runtime evidence. |
| `2026-04-20-aws-cloud-scanner-collector.md` | Design accepted; runtime not implemented | Build the AWS collector runtime, fact emission, claim loop, telemetry, and docs. |
| `2026-04-20-embedded-local-backends-desktop-mode.md` | Accepted with follow-up | Local backend path is shipped with embedded NornicDB as the default local mode; latest-main process installs remain explicit while release packaging, conformance, and host coverage close out. |
| `2026-04-20-multi-source-reducer-and-consumer-contract.md` | Architecture accepted; implementation partial | Build collector-backed projectors and consumer MCP/HTTP tools. |
| `2026-04-20-terraform-state-collector.md` | Accepted; runtime implemented, security review signed off | Reader stack, streaming parser, redaction, conditional S3 reads, DynamoDB lock metadata, and no-plaintext persistence proof have landed (PRs #84, #147, #148, #150). Operator runbook, Grafana dashboard, Prometheus alerts, and the 2026-05-10 Security Review section close out issue #46. Optional follow-up: a formal 100 MB peak-memory benchmark gate. |
| `2026-04-20-workflow-coordinator-and-multi-collector-runtime-contract.md` | Accepted; Git claim proof path implemented | Keep Helm claims dark until remote full-corpus validation and future collector-family gates are ready. |
| `2026-04-20-workflow-coordinator-claiming-fencing-and-convergence.md` | Accepted; guarded proof path implemented | Work items carry the full phase-state tuple, reconciliation uses exact downstream truth, release/fairness/Git claim primitives exist, and deployment promotion is blocked behind proof. |
| `2026-04-22-nornicdb-graph-backend-candidate.md` | Accepted with conditions | Latest-main policy is explicit; backend conformance and profile gates have NornicDB evidence; Neo4j parity research is now complete and recorded in the accepted 2026-05-04 ADR; finish install trust and broader host coverage. |
| `2026-05-09-documentation-truth-collectors-and-actuators.md` | Accepted; implementation not started | Add documentation-source collectors and read-only drift findings to Eshu; keep LLM diff generation, approvals, and publishing in external updater actuators. |
| `2026-05-09-optional-component-boundary.md` | Accepted; manifest/package-manager follow-up | Git stays built-in and default. Terraform state and AWS may incubate as first-party optional component candidates but must stay disabled unless explicitly configured. Future manifest loading and package management remain separate follow-up work. |

## In Progress

| ADR | Current state | What remains |
| --- | --- | --- |
| `2026-04-19-multi-source-correlation-dsl-and-collector-readiness.md` | In progress | DSL/rule-pack substrate and source-kind contracts exist; AWS, Terraform-state, webhook, and full cloud/runtime joins remain. |
| `2026-04-20-embedded-local-backends-implementation-plan.md` | In progress | Local host, embedded local-authoritative runtime, backend conformance harness, profile matrix, and latest-main NornicDB proof are in place; release packaging, host envelope, and plugin work still open. |
| `2026-04-24-iac-usage-reachability-and-refactor-impact.md` | In progress | Dead-IaC reachability and pagination are proven; shared neighborhood, impact, integrity, and remaining IaC-family coverage remain. |
| `2026-05-07-dead-code-root-model-and-language-reachability.md` | Proposed | Freeze the Eshu dogfood false positives, add language-scoped maturity metadata, materialize root reachability, and promote exactness only after per-language gates pass. |
| `2026-05-04-neo4j-parity-optimization-plan.md` | Accepted | Records the schema-first Neo4j proof, the shared writer cleanup, and the resulting support posture. |
| `2026-05-09-iac-replatforming-planner.md` | Proposed | Define the read-only IaC management-status and re-platforming planner capability on top of Git, Terraform state, and cloud scanner evidence. |
| `2026-05-10-tag-taxonomy-correlation-dsl-addendum.md` | Accepted | Freezes tag alias packs, overrides, source precedence, negative evidence, and the `aws_tag_distribution` admin learning loop. |
| `2026-05-11-module-aware-drift-joining.md` | Accepted; implemented in PR #202 | Adopts Option B (loader-side module-call join) for issue #169 so `module.<name>.<type>.<address>` state addresses can join with config-side `terraform_resources` facts instead of surfacing as `added_in_state`. Parser stays per-file; `PostgresDriftEvidenceLoader` learns a callee-directory prefix map. Local-filesystem `source` paths in scope; registry, Git, archive, cross-repo sources fall back to `added_in_state` with a new `eshu_dp_drift_unresolved_module_calls_total{reason}` counter. |
| `2026-05-12-webhook-triggered-repository-refresh.md` | Accepted | Defines a separate public EKS `webhook-listener` runtime for GitHub/GitLab default-branch events. Webhook payloads are trigger evidence only; Git sync and the normal repository generation path remain authoritative for graph/query truth. |
| `2026-05-12-package-registry-collector.md` | Proposed | Defines issue #24 package-registry collection as a separate source-truth boundary from OCI image collection. Phase 1 validates ECR through the OCI lane and JFrog through both OCI and package-feed lanes, then expands to public ecosystem fixtures, GitHub, GitLab, Google, Azure, Nexus, and CodeArtifact. |
| `2026-05-13-eshu-console-read-only-product-surface.md` | Proposed | Defines the first private read-only console: `apps/console`, role-neutral search, entity-centered workspaces, demo/private mode separation, and contract-first envelope handling. |
| `2026-05-15-nornicdb-semantic-retrieval-evaluation.md` | Proposed | Defines issue #396's evaluation-first path for using NornicDB vector, hybrid search, decay, and link-prediction capabilities as a truth-labeled read-side context layer without changing reducer-owned canonical graph admission. |

## Discussion Shortlist

The ADRs that need active planning next are:

1. Workflow coordinator production claim ownership.
2. Neo4j parity support-posture cleanup and residual host-trust work.
3. NornicDB latest-main default and conformance closure.
4. AWS cloud scanner and Terraform state collector implementation.
5. IaC impact/integrity beyond dead-IaC reachability.
6. Multi-source correlation beyond the current Git/config rule packs.
7. Dead-code exactness by language family, starting with the Eshu dogfood Go
   false positives.
8. IaC re-platforming planner and unmanaged-resource evidence workflows.
9. Kubernetes live collector implementation after Terraform state and AWS
   collector evidence are stable.
10. OCI registry collector implementation for digest-anchored image and
    supply-chain evidence.
11. Package registry collector implementation for package/version/artifact and
    package-source correlation evidence.
12. Tag taxonomy implementation in the reducer-owned normalizer and admin
   status payload.
13. Webhook-triggered repository refresh runtime for hosted freshness.
14. NornicDB semantic retrieval evaluation for MCP/API recall, freshness
   scoring, and candidate relationship discovery.
