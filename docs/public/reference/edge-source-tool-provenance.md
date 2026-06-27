# Edge Source-Tool Provenance

This is the authoritative, evidence-backed map of **which graph edges can be
labeled by the source tool that produced them, how, and which cannot** — and it
defines the canonical `source_tool` vocabulary the rest of
[epic #3997](https://github.com/eshu-hq/eshu/issues/3997) targets.

It began as a **design baseline** (the agreed enum + classification). As of
[#3999](https://github.com/eshu-hq/eshu/issues/3999) the normalized `source_tool`
token is **stamped at write time** on every Tier-2 (shared-verb) cross-repo edge
(`go/internal/reducer/cross_repo_evidence_type.go` derives it; the canonical edge
writers in `go/internal/storage/cypher` persist it). Surfacing it in the
API/console is [#4000](https://github.com/eshu-hq/eshu/issues/4000)/[#4001](https://github.com/eshu-hq/eshu/issues/4001).
The golden-corpus gate enforces it: each evidence-narrowed Tier-2 correlation
(rc-29…rc-34) asserts `source_tool` is present and equals its canonical token, so
a regression fails CI.

Two questions this doc answers for any edge:

1. **What tool produced this edge?** (the `source_tool` axis — this document)
2. **What language/format is this file?** (the `language`/`source_type` axis,
   carried on `File`/code-entity *nodes*, not edges — see
   [#4003](https://github.com/eshu-hq/eshu/issues/4003)). The two are
   complementary: a `.yaml` file's `language` is `yaml`, but the *tool* of an
   edge derived from it could be `kubernetes`, `ansible`, `helm`, or `argocd`.

## The three provenance tiers

A graph sweep of the B-7 golden corpus plus a read of every emitter
(`go/internal/storage/cypher/*`, `go/internal/reducer/*`) classifies every
materialized edge type into one of three tiers:

- **Tier 1 — self-labeling by edge TYPE.** The verb *is* the tool or the
  semantic. `ATLANTIS_DEPENDS_ON` is Atlantis; `DEPENDS_ON_PACKAGE` is the
  package registry; `RUNS_IMAGE` is the Kubernetes live↔resolved correlation.
  No per-edge property is needed — the type carries the attribution.
- **Tier 2 — shared verbs, tool carried in `evidence_kinds`/`evidence_type`.**
  A handful of verbs (`DEPENDS_ON`, `DEPLOYS_FROM`, `USES_MODULE`,
  `READS_CONFIG_FROM`, `PROVISIONS_DEPENDENCY_FOR`, `DISCOVERS_CONFIG_IN`,
  `RUNS_ON`) are emitted by *several* tools through one shared edge type. The
  tool is recoverable per edge from the evidence properties, but the vocabulary
  is **not normalized** (UPPER `EvidenceKind` constant vs lower sub-kind token
  vs ecosystem family) and is **not surfaced**. This is the tier #3999 fixes.
- **Tier 3 — no edge-level tool.** Code edges (`CALLS`, `REFERENCES`,
  `INHERITS`, `INSTANTIATES`, `USES_METACLASS`, …) derive their "tool" from the
  **language**, which already lives on the nodes; they carry `resolution_method`
  (the resolution mechanism), not a tool. Structural edges (`CONTAINS`,
  `REPO_CONTAINS`, `DEFINES`, `INSTANCE_OF`, `EXPOSES_ENDPOINT`,
  `DEPLOYMENT_SOURCE`, `HAS_DEPLOYMENT_EVIDENCE`,
  `EVIDENCES_REPOSITORY_RELATIONSHIP`, …) have no tool concept at all. Their
  lack of a `source_tool` is **intentional**, not a coverage gap.

## The three edge properties (and why `evidence_source` is not the tool)

Every Tier-2 resolver edge carries three distinct provenance properties. They
are easy to confuse; only the first two encode the tool.

| Property | Holds | Shape | Set at | Tool? |
| --- | --- | --- | --- | --- |
| `evidence_kinds` | UPPERCASE `EvidenceKind` enum strings (e.g. `KUSTOMIZE_RESOURCE_REFERENCE`, `ARGOCD_APPLICATION_SOURCE`) | list (an edge can carry several) | resolver aggregates the set (`go/internal/relationships/resolver.go`), written to the edge in `go/internal/storage/cypher/canonical_relationships.go` | **yes** (raw form) |
| `evidence_type` | lowercase_snake single token, derived from the *first* evidence kind (e.g. `kustomize_resource_reference`) | scalar | `go/internal/reducer/cross_repo_evidence_type.go:12-46` map; written in `canonical_relationships.go` | **yes** (sub-kind, not yet collapsed to the tool) |
| `evidence_source` | the producing **STAGE** (`resolver/cross-repo`, `projector/canonical`, `finalization/workloads`, `parser/code-calls`, `reducer/runs-in`, …) | scalar | many `*EvidenceSource` consts across `go/internal/reducer/*` and `go/internal/storage/cypher/*` | **no** |

`evidence_source` answers "which pipeline stage wrote this edge", not "which
tool". A `terraform` edge and a `helm` edge can both carry
`evidence_source="resolver/cross-repo"`. It is **not** a substitute for a
source-tool tag.

Note also that `evidence_type` today is the *sub-kind* token
(`terraform_app_repo`, `terraform_config_path`, …), **not** the tool: six
distinct Terraform evidence kinds all map to six different `evidence_type`
values, all of which are the tool `terraform`. Normalizing those down to a
single `source_tool` token is exactly the work of #3999; the mapping below is
the target.

## Canonical `source_tool` vocabulary

A closed, bounded, lowercase-snake token set. New tools extend it by a reviewed
addition, never by free-form values.

| Token | Surfaced by | Origin tier |
| --- | --- | --- |
| `terraform` | `TERRAFORM_*` evidence kinds | Tier 2 |
| `terragrunt` | `TERRAGRUNT_*` evidence kinds | Tier 2 |
| `helm` | `HELM_*` evidence kinds | Tier 2 |
| `kustomize` | `KUSTOMIZE_*` evidence kinds | Tier 2 |
| `argocd` | `ARGOCD_*` evidence kinds | Tier 2 |
| `ansible` | `ANSIBLE_ROLE_REFERENCE` | Tier 2 |
| `puppet` | `PUPPET_MODULE_REFERENCE` | Tier 2 |
| `chef` | `CHEF_COOKBOOK_DEPENDENCY` | Tier 2 |
| `jenkins` | `JENKINS_*` evidence kinds | Tier 2 |
| `github_actions` | `GITHUB_ACTIONS_*` evidence kinds | Tier 2 |
| `docker` | `DOCKERFILE_SOURCE_LABEL` | Tier 2 |
| `docker_compose` | `DOCKER_COMPOSE_*` evidence kinds | Tier 2 |
| `gcp` | `GCP_CLOUD_RELATIONSHIP`; GCP cloud writers | Tier 2 / Tier 1 |
| `atlantis` | `MANAGES`, `ATLANTIS_DEPENDS_ON`, `USES_WORKFLOW` edge types | Tier 1 |
| `gitlab` | `DEFINES_JOB`, `NEEDS` edge types | Tier 1 |
| `gomod` | `HAS_VERSION`/`DECLARES_DEPENDENCY`/`DEPENDS_ON_PACKAGE` (Go ecosystem) | Tier 1 |
| `npm` | package-registry edges (npm ecosystem) | Tier 1 |
| `pip` | package-registry edges (PyPI ecosystem) | Tier 1 |
| `maven` | package-registry edges (Maven ecosystem) | Tier 1 |
| `cargo` | package-registry edges (crates.io ecosystem) | Tier 1 |
| `aws` | AWS cloud-resource writers / SDK-call analysis | Tier 1 |
| `azure` | Azure cloud-resource writers | Tier 1 |
| `kubernetes` | Kubernetes correlation / live workload writers | Tier 1 |
| `unknown` | explicit fallback when no tool is provable | — |

**`unknown` rule.** An edge whose tool cannot be proven from its evidence gets
the explicit `unknown` token, never a guess. `resolvedRelationshipSourceTool`
(`go/internal/reducer/cross_repo_evidence_type.go`) emits `unknown` for a
present-but-unmapped primary evidence kind — so a new tool that ships an evidence
kind without a `source_tool` classification surfaces as a visible gap (and fails
the #4002 drift gate) rather than a silent passthrough. An edge with no evidence
kind at all is left unstamped (absent), distinct from the explicit `unknown`.

Generated/runtime evidence kinds that are not named constants — the Terraform
schema extractor synthesizes per-resource kinds like `TERRAFORM_ECS_SERVICE` and
`TERRAFORM_WAFV2_WEB_ACL` at runtime — are classified by their family prefix
(`TERRAFORM_*`→`terraform`, etc.) after the named-constant lookup, so a real
Terraform edge is labeled `terraform` rather than `unknown`. The named-constant
map is consulted first, so the `TERRAGRUNT_*` split is preserved.

The package-ecosystem tokens (`gomod`, `npm`, `pip`, `maven`, `cargo`) and the
cloud tokens (`aws`, `azure`, `kubernetes`) are **not** carried by any
`EvidenceKind` constant — they are Tier-1 self-labeling edge types whose tool is
derived from the edge type plus the package/cloud ecosystem on the endpoint
node. They are in the vocabulary because consumers (#4000–#4009) filter on the
same enum across both axes.

## `EvidenceKind` → `source_tool` mapping

The 33 persisted `EvidenceKind` constants (`go/internal/relationships/models.go:20-91`)
collapse to 13 tools. This is the family→tool map #3999 implements (distinct
from the existing `evidenceKindToType` sub-kind map, which keeps each kind
separate).

| `EvidenceKind` (string value) | `source_tool` |
| --- | --- |
| `TERRAFORM_APP_REPO`, `TERRAFORM_APP_NAME`, `TERRAFORM_GITHUB_REPOSITORY`, `TERRAFORM_GITHUB_ACTIONS_REPOSITORY`, `TERRAFORM_CONFIG_PATH`, `TERRAFORM_IAM_PERMISSION` | `terraform` |
| `TERRAFORM_MODULE_SOURCE` | `terraform` ¹ |
| `TERRAGRUNT_DEPENDENCY_CONFIG_PATH`, `TERRAGRUNT_CONFIG_ASSET_PATH` | `terragrunt` |
| `HELM_CHART_REFERENCE`, `HELM_VALUES_REFERENCE` | `helm` |
| `KUSTOMIZE_RESOURCE_REFERENCE`, `KUSTOMIZE_HELM_CHART_REFERENCE`, `KUSTOMIZE_IMAGE_REFERENCE` | `kustomize` |
| `ARGOCD_APPLICATION_SOURCE`, `ARGOCD_APPLICATIONSET_DISCOVERY`, `ARGOCD_APPLICATIONSET_DEPLOY_SOURCE`, `ARGOCD_DESTINATION_PLATFORM` | `argocd` |
| `GITHUB_ACTIONS_REUSABLE_WORKFLOW`, `GITHUB_ACTIONS_LOCAL_REUSABLE_WORKFLOW`, `GITHUB_ACTIONS_CHECKOUT_REPOSITORY`, `GITHUB_ACTIONS_WORKFLOW_INPUT_REPOSITORY`, `GITHUB_ACTIONS_ACTION_REPOSITORY` | `github_actions` |
| `JENKINS_SHARED_LIBRARY`, `JENKINS_GITHUB_REPOSITORY` | `jenkins` |
| `DOCKER_COMPOSE_BUILD_CONTEXT`, `DOCKER_COMPOSE_IMAGE`, `DOCKER_COMPOSE_DEPENDS_ON` | `docker_compose` |
| `DOCKERFILE_SOURCE_LABEL` | `docker` |
| `ANSIBLE_ROLE_REFERENCE` | `ansible` |
| `PUPPET_MODULE_REFERENCE` | `puppet` |
| `CHEF_COOKBOOK_DEPENDENCY` | `chef` |
| `GCP_CLOUD_RELATIONSHIP` | `gcp` |

¹ **`TERRAFORM_MODULE_SOURCE` is the one ambiguous kind** — its doc comment in
`models.go` reads "Terraform *or Terragrunt* module source reference". The kind
alone cannot distinguish the two tools. #3999 must either (a) default it to
`terraform` and accept that Terragrunt module sources are labeled `terraform`,
or (b) carry a second discriminator (e.g. the source file extension / config
kind) to split them. Pending that decision, treat `USES_MODULE` /
`TERRAFORM_MODULE_SOURCE` as `terraform` and track the precision gap.

## Per-edge-type coverage matrix

Edge types are enumerated from the registry `go/internal/graph/edgetype/edgetype.go`
(`registered` slice, 75 types). The table below classifies each by tier; only
Tier-1/Tier-2 carry a tool.

### Tier 1 — self-labeling by edge type

| Edge type | `source_tool` | Emitter |
| --- | --- | --- |
| `MANAGES` | `atlantis` | `storage/cypher/canonical_atlantis_edges.go:23` |
| `ATLANTIS_DEPENDS_ON` | `atlantis` | `canonical_atlantis_edges.go:35` |
| `USES_WORKFLOW` | `atlantis` | `canonical_atlantis_edges.go:46` |
| `DEFINES_JOB` | `gitlab` | `canonical_gitlab_edges.go:22` |
| `NEEDS` | `gitlab` | `canonical_gitlab_edges.go:34` |
| `HAS_VERSION` | package ecosystem (`gomod`/`npm`/`pip`/`maven`/`cargo`) | `package_registry_edge_writer.go:25` |
| `DECLARES_DEPENDENCY` | package ecosystem | `package_registry_edge_writer.go:33` |
| `DEPENDS_ON_PACKAGE` | package ecosystem | `package_registry_edge_writer.go:36` |
| `PROVISIONS_PLATFORM` | `terraform`/IaC | `reducer/infrastructure_platform_materializer.go:147` |
| `INVOKES_CLOUD_ACTION` | `aws` | `canonical_invokes_cloud_action_edges.go:22` |
| `HANDLES_ROUTE` | language/framework | `canonical_handles_route_edges.go:19` |
| `RUNS_IN` | runtime (reducer workload binding) | `canonical_runs_in_edges.go:27` |
| `RUNS_IMAGE` | `kubernetes` | `kubernetes_correlation_edge_writer.go:54` |
| `CORRELATES_DEPLOYABLE_UNIT` | hybrid ² | `canonical_deployable_unit_edges.go:9` |

The cloud/IAM/security-group/secrets reducer edges (`CAN_PERFORM`,
`CAN_ASSUME`, `CAN_ESCALATE_TO`, `USES_PROFILE`, `HAS_ROLE`, `GRANTS_ACCESS_TO`,
`LOGS_TO`, `ALLOWS_INGRESS`/`ALLOWS_EGRESS`, `SECRETS_IAM_*`, `EXECUTES_SHELL`,
`TAINT_FLOWS_TO`, incident `HAS_*_ROUTING`) are also Tier 1: their tool is the
cloud/runtime that produced them (`aws`/`gcp`/`azure`/`kubernetes`), derivable
from the edge type and endpoint node, and they are out of scope for the Tier-2
`source_tool` stamping in #3999.

² `CORRELATES_DEPLOYABLE_UNIT` is a hybrid: a Tier-1 type that also carries a
**non-`EvidenceKind`** provenance vocabulary in its `evidence_kinds` property
(`repository_identity`, `deployable_unit_key`, `deployment_repo`, plus
per-artifact tokens like `argocd`/`dockerfile`) —
`go/internal/reducer/deployable_unit_correlation.go:306-392`. Do not conflate
these tokens with the `models.go` enum.

### Tier 2 — shared verbs; tool in `evidence_kinds`/`evidence_type`

All emitted by the cross-repo resolver (`reducer/cross_repo_resolution.go:441-509`)
and written through the canonical relationship upserts
(`storage/cypher/canonical_relationships.go`, dispatched in
`storage/cypher/edge_writer.go:275-320`).

| Edge type | Tools observed (via evidence kinds) | Emitter |
| --- | --- | --- |
| `DEPENDS_ON` | `ansible`, `puppet`, `chef`, `docker_compose`, `github_actions`, `jenkins`, `gcp`, … | `canonical.go:78` (repo), `:93` (workload) |
| `DEPLOYS_FROM` | `helm`, `kustomize`, `argocd`, `docker`, `docker_compose`, `github_actions` | `canonical_relationships.go:37` |
| `DISCOVERS_CONFIG_IN` | `terragrunt`, `argocd`, `jenkins` | `canonical_relationships.go:56` |
| `PROVISIONS_DEPENDENCY_FOR` | `terraform`, `terragrunt` | `canonical_relationships.go:75` |
| `USES_MODULE` | `terraform` (¹ terragrunt) | `canonical_relationships.go:94` |
| `READS_CONFIG_FROM` | `terraform` | `canonical_relationships.go:113` |
| `RUNS_ON` | `argocd`, `terraform` | `canonical_relationships.go:267` |

### Tier 3 — no edge-level tool (intentional)

**Code edges** — tool is the node `language`; they carry `resolution_method`:
`CALLS`, `REFERENCES`, `INHERITS`, `INSTANTIATES`, `USES_METACLASS`,
`IMPLEMENTS`, `ALIASES`, `OVERRIDES`, `IMPORTS`, and the SQL parser edges
(`HAS_COLUMN`, `REFERENCES_TABLE`, `READS_FROM`, `INDEXES`, `TRIGGERS_ON`,
`QUERIES_TABLE`, `EXECUTES`, `TRIGGERS`).

**Structural edges** — no tool concept: `REPO_CONTAINS`, `CONTAINS`, `DEFINES`,
`INSTANCE_OF`, `EXPOSES_ENDPOINT`, `DEPLOYMENT_SOURCE`,
`HAS_DEPLOYMENT_EVIDENCE`, `EVIDENCES_REPOSITORY_RELATIONSHIP`,
`TARGETS_ENVIRONMENT`, `HAS_PARAMETER`, `DOCUMENTS`, `EXPLAINS`, `USES`
(workload→cloud-resource), `SATISFIED_BY`.

**Registered but not currently materialized:** `MAPS_TO_TABLE` and `MIGRATES`
appear in the edge-type registry and are read by `query/impact.go`, but no
emitter MERGEs them in the current code. They carry no `source_tool` until a
materializer exists.

## Re-auditing coverage

The classification above is a snapshot of the emitters. To re-audit against a
live graph (e.g. after adding a tool), run the sweep below and diff the result
against this doc. Against NornicDB (the default backend), use the transactional
HTTP endpoint inside the container (mirrors the profiling method in
[Cypher Performance](cypher-performance.md)):

```bash
# container name from `docker ps`; port = the NornicDB Bolt-HTTP port (7474)
docker exec <nornicdb_container> wget -q -O - \
  --header='Content-Type: application/json' \
  --post-data='{"statements":[{"statement":
    "MATCH ()-[r]->() RETURN type(r) AS edge_type, collect(DISTINCT r.evidence_kinds) AS evidence_kinds, collect(DISTINCT r.evidence_type) AS evidence_type, collect(DISTINCT r.evidence_source) AS evidence_source, count(r) AS edge_count ORDER BY edge_type"}]}' \
  http://localhost:7474/db/nornic/tx/commit
```

`MATCH ()-[r]->()` with anonymous endpoints is answered by the
relationship-type index and is near-instant. `evidence_kinds` is itself a list,
so `collect(DISTINCT r.evidence_kinds)` returns a list-of-lists — flatten in
post-processing. A new tool that appears as a Tier-2 evidence kind but is
missing from the `EvidenceKind` → `source_tool` table above is a coverage gap to
close in #3999 (and a future drift gate, #4002, fails on exactly that).

## Related

- [Relationship Evidence And Resolution](relationship-mapping-evidence.md) — the
  full Tier-2 evidence catalogue per tool.
- [Relationship Mapping](relationship-mapping.md) — the edge-type catalogue.
- [Cypher Performance](cypher-performance.md) — the sweep/profiling method.
