# #6693 mapping: directories the epic tree does not list

Part of the [storage/postgres target tree](../6693-postgres-target-tree.md).
The epic's approved tree in
[storage-collector-tree.md](../storage-collector-tree.md#target-tree-internalstoragepostgres)
names about 30 directories. The mapping adds the ones below, each for the
reason given. None of them replaces an epic directory; each holds files the
epic tree gave no home.

| directory | non-test files | why it exists |
| --- | ---: | --- |
| `admission/` | 3 | `AdmissionDecisionStore` and the `admission_decisions` table: write-eligibility decisions, no other family's store. |
| `cicd/` | 1 | `CICDRunWatermarkStore`, the per-source watermark for the CI/CD run collector. |
| `cloud/aws/drift/` | 9 | AWS runtime drift evidence, findings, fencing and readiness. The epic lists `cloud/aws`; drift is a distinct store family under it. |
| `code/divergence/` | 3 | Drifted-code evidence and findings stores; named after the `codedivergence` domain the query and reducer layers already use. |
| `code/flow/` | 8 | Value-flow and function-graph stores (fixpoint components, program inputs, refresh ack, function summaries). |
| `code/reachability/` | 3 | `CodeReachabilityStore`, call-graph reachability verdicts. |
| `code/taint/` | 2 | Projected taint node and interprocedural edge ledgers. |
| `collector/` | 4 | Collector evidence summaries and collector-generation dead letters, both collector-owned tables that status reads. |
| `decisions/` | 1 | `DecisionStore` over `projection_decisions`, a different table and type from `admission/`. |
| `facts/payload/` | 1 | JSON payload encode/decode used by facts, both queues, relationships and collector; a leaf so none of them import `facts/`. |
| `facts/schema/` | 7 | `fact_records` DDL constants, read only by tests; keeps `facts/` at 36. |
| `freshness/` and its `aws/`, `gcp/`, `incident/` children | 2 + 9 | Repository freshness, plus three freshness-trigger stores with one trigger/claim/reap shape, built by the coordinator and webhook listener (N4). |
| `vulnerability/` | 1 | Vulnerability-intelligence collector source state; not a freshness trigger store (N4). |
| `governance/audit/` | 2 | `GovernanceAuditStore`, the authorization-decision audit log used by the API, MCP server, coordinator and admin CLI; not a login flow. |
| `graph/` | 1 | `GraphEndpointPresenceStore`. |
| `graph/owner/` | 2 | `GraphNodeOwnerStore` and its backfill ledger (the per-uid owner critical section). |
| `iac/` | 1 | `IaCReachabilityStore`, infrastructure exposure reachability, distinct from code reachability. |
| `identity/github/`, `identity/oidc/` | 2, 3 | GitHub and OIDC login state stores. Neither is provider-config CRUD (`identity/provider/`) nor SAML. |
| `identity/signin/` | 3 | The epic's `identity/sign`, renamed: it holds the tenant sign-in policy, and bare "sign" reads as signatures (N1). |
| `identity/permission/` | 0 (new leaf) | Role-to-grant resolution shared by API tokens, OIDC, SAML and local login (decision D1). |
| `lock/` | 3 | Advisory-lock helpers shared across families: deferred maintenance, package-registry identity, platform graph. |
| `recovery/` | 1 | `RecoveryStore`, which spans dead-letter replay, collector-generation replay and refinalize. |
| `relationship/` | 6 | `RelationshipStore` and its methods (evidence, reference keys, resolved rows, accepted generations). |
| `scope/completion/` | 4 | Cross-scope completion queue, fan-out, producer readiness and quiescence, nested under the epic's `scope/`. |
| `service/` | 4 | Service catalog id resolution, service documentation and incident evidence, and service re-materialization, in one package (N3). |
| `status/` | 17 | The `StatusStore` read surface (decision D4). |
| `supply/chain/impact/` | 2 | Canonical-winner materialization and vulnerability suppression; mirrors `internal/query/supply/chain/impact`. |
| `fake/` | test support | Shared test fakes as a real package (decision D6). |
| `migrations/` (Go package) | 1 (new `embed.go`) | The directory exists; D2 adds a Go file that owns the `//go:embed`. |
| `cloud/aws/iam/target/` | 1 (moved) | The existing `iamcantargets/`, renamed: AWS ARN resolution for CAN_PERFORM targets (N2). |
