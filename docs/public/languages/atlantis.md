# Atlantis Parser

This page tracks the checked-in Go parser contract in the current repository state.
Canonical implementation: `go/internal/parser/registry.go` (the `yaml` parser) plus
the Atlantis-specific extraction and tests listed below.

Atlantis governs how Terraform is planned and applied from pull requests. Its
repo-level `atlantis.yaml` is plain YAML (no `apiVersion`/`kind`), so it is
dispatched by filename inside the YAML parser, the same way `terragrunt.hcl` is
special-cased inside the HCL parser.

## Parser Contract
- Language: `yaml`
- Family: `iac`
- Parser: `DefaultEngine (yaml)`
- Entrypoint: `go/internal/parser/yaml/atlantis.go`
- Fixture: `tests/fixtures/ecosystems/terraform_comprehensive/atlantis.yaml`
- Unit test suite: `go/internal/parser/yaml/atlantis_test.go` and
  `go/internal/parser/yaml/atlantis_combined_test.go` (the latter pins the
  combined projects + workflows output of the single-unmarshal parse path)
- Integration validation: B-7 golden-corpus gate asserts the `AtlantisProject`
  and `AtlantisWorkflow` nodes (required node labels) plus the `MANAGES` (`rc-5`),
  `ATLANTIS_DEPENDS_ON` (`rc-6`), and `USES_WORKFLOW` (`rc-7`) governance edges
  (`scripts/verify-golden-corpus-gate.sh`)

## Capability Checklist
| Capability | ID | Status | Extracted Bucket/Key | Required Fields | Graph Surface | Unit Coverage | Integration Coverage | Rationale |
|-----------|----|--------|------------------------|-----------------|---------------|---------------|----------------------|-----------|
| Repo-level projects (`projects[]`) | `atlantis-projects` | supported | `atlantis_projects` | `name, line_number, dir` | `node:AtlantisProject` | `go/internal/parser/yaml/atlantis_test.go::TestParseAtlantisConfigEmitsProjectRows` | B-7 golden-corpus gate (`node_present_AtlantisProject`) | One row per project; name falls back to dir when omitted. |
| Project directory | `atlantis-project-dir` | supported | `atlantis_projects` | `name, line_number, dir` | `property:AtlantisProject.dir` | `go/internal/parser/yaml/atlantis_test.go::TestParseAtlantisConfigEmitsProjectRows` | B-7 golden-corpus gate | The Terraform directory the project plans/applies; the MANAGES edge target (rc-5). |
| Project workspace | `atlantis-project-workspace` | supported | `atlantis_projects` | `workspace` | `property:AtlantisProject.workspace` | `go/internal/parser/yaml/atlantis_test.go::TestParseAtlantisConfigEmitsProjectRows` | B-7 golden-corpus gate | Defaults to `default` when omitted. |
| Terraform version / distribution | `atlantis-terraform-version` | supported | `atlantis_projects` | `terraform_version, terraform_distribution` | `property:AtlantisProject.terraform_version` | `go/internal/parser/yaml/atlantis_test.go::TestParseAtlantisConfigEmitsProjectRows` | B-7 golden-corpus gate | - |
| Autoplan | `atlantis-autoplan` | supported | `atlantis_projects` | `autoplan_enabled, autoplan_when_modified` | `property:AtlantisProject.autoplan_enabled` | `go/internal/parser/yaml/atlantis_test.go::TestParseAtlantisConfigEmitsProjectRows` | B-7 golden-corpus gate | `enabled` defaults to true when the autoplan block is present. |
| Apply / plan / import requirements | `atlantis-apply-requirements` | supported | `atlantis_projects` | `apply_requirements, plan_requirements, import_requirements` | `property:AtlantisProject.apply_requirements` | `go/internal/parser/yaml/atlantis_test.go::TestParseAtlantisConfigEmitsProjectRows` | B-7 golden-corpus gate | The governance posture (e.g. `approved`, `mergeable`). |
| Project dependencies | `atlantis-depends-on` | supported | `atlantis_projects` | `depends_on, execution_order_group` | `property:AtlantisProject.depends_on` | `go/internal/parser/yaml/atlantis_test.go::TestParseAtlantisConfigEmitsProjectRows` | B-7 golden-corpus gate | The ATLANTIS_DEPENDS_ON edge source (rc-6). |
| Custom workflow reference | `atlantis-workflow` | supported | `atlantis_projects` | `workflow` | `property:AtlantisProject.workflow` | `go/internal/parser/yaml/atlantis_test.go::TestParseAtlantisConfigEmitsProjectRows` | B-7 golden-corpus gate | The named custom workflow; drives the USES_WORKFLOW edge (rc-7). |
| Repo locks mode | `atlantis-repo-locks` | supported | `atlantis_projects` | `repo_locks_mode` | `property:AtlantisProject.repo_locks_mode` | `go/internal/parser/yaml/atlantis_test.go::TestParseAtlantisConfigEmitsProjectRows` | B-7 golden-corpus gate | - |
| Custom workflows (`workflows:`) | `atlantis-workflows` | supported | `atlantis_workflows` | `name, line_number, source` | `node:AtlantisWorkflow` | `go/internal/parser/yaml/atlantis_test.go::TestParseAtlantisConfigEmitsWorkflowRows` | B-7 golden-corpus gate (`node_present_AtlantisWorkflow`) | One node per named workflow; `source=defined` (in-file) or `referenced` (defined server-side). Per-stage ordered step kinds (`<stage>_step_kinds`); run-step command bodies are intentionally not captured. |
| USES_WORKFLOW edge (project → workflow) | `atlantis-uses-workflow-edge` | supported | `atlantis_projects` + `atlantis_workflows` | `workflow` | `relationship:USES_WORKFLOW` | `go/internal/storage/cypher/canonical_atlantis_edges_test.go::TestAtlantisEdgeStatementsResolvesUsesWorkflow` | B-7 golden-corpus gate (`rc-7`) | `(AtlantisProject)-[:USES_WORKFLOW]->(AtlantisWorkflow)`; resolved within the same atlantis.yaml by uid. |
| MANAGES edge (project → Terraform dir) | `atlantis-manages-edge` | supported | `atlantis_projects` | `dir` | `relationship:MANAGES` | `go/internal/storage/cypher/canonical_atlantis_edges_test.go::TestAtlantisEdgeStatementsResolvesManagesAndDependsOn` | B-7 golden-corpus gate (`rc-5`) | `(AtlantisProject)-[:MANAGES]->(Directory)`; resolved in Go, matched by `AtlantisProject.uid` / `Directory.path`. |
| DEPENDS_ON edge (project → project) | `atlantis-depends-on-edge` | supported | `atlantis_projects` | `depends_on` | `relationship:ATLANTIS_DEPENDS_ON` | `go/internal/storage/cypher/canonical_atlantis_edges_test.go::TestAtlantisEdgeStatementsResolvesManagesAndDependsOn` | B-7 golden-corpus gate (`rc-6`) | `(AtlantisProject)-[:ATLANTIS_DEPENDS_ON]->(AtlantisProject)`; resolved within the same atlantis.yaml. |

## Graph edges

The governance edges that connect `AtlantisProject` to what it governs are
projector structural edges resolved in Go and matched by canonical key
(`AtlantisProject.uid` / `Directory.path`, the same UNWIND/MERGE pattern as the
IMPORTS edge), written in the canonical structural-edge phase after the nodes
commit:

- `(AtlantisProject)-[:MANAGES]->(Directory)` — the Terraform directory the
  project plans/applies (from `dir`); asserted by the B-7 gate (`rc-5`).
- `(AtlantisProject)-[:ATLANTIS_DEPENDS_ON]->(AtlantisProject)` — project ordering from
  `depends_on`; asserted by the B-7 gate (`rc-6`).

An `AtlantisProject` node is also connected to its repository through the
standard containment chain (Repository → Directory → File → AtlantisProject).

## Custom workflows

`atlantis.yaml` `workflows:` (custom `plan`/`apply`/`import`/`policy_check`
stages) materialize as **`AtlantisWorkflow`** nodes, and each project that names
a workflow links to it via `(AtlantisProject)-[:USES_WORKFLOW]->(AtlantisWorkflow)`
(`rc-7`):

- A workflow defined in the file has `source: defined` and records each defined
  stage plus its ordered step **kinds** (e.g. `plan_step_kinds: "init,run,plan"`).
- A workflow named by a project but defined **server-side** (no in-file body —
  the common real-world case) materializes as a `source: referenced` stub so the
  `USES_WORKFLOW` edge still resolves.

The opaque body of a `run`/`env` step is intentionally **not** captured as a node
property — recording the step kind (`run`) is truthful without fabricating
semantics for arbitrary shell. Both the workflow node and the `USES_WORKFLOW`
edge are matched by canonical key (uid), like the other Atlantis edges.

## Query surfacing

`AtlantisProject`/`AtlantisWorkflow` and the `MANAGES`/`ATLANTIS_DEPENDS_ON`/
`USES_WORKFLOW` edges above are graph-written and retrievable through two
surfaces:

- **Generic entity-context surface**: `resolve_entity` returns a node's id by
  name and `get_entity_context` returns that node together with its outgoing
  edges (`OPTIONAL MATCH (e)-[rel]->(target)`), so an
  `AtlantisProject`/`AtlantisWorkflow` and its
  `MANAGES`/`ATLANTIS_DEPENDS_ON`/`USES_WORKFLOW` edges can be read back this
  way. `atlantis_project`/`atlantis_workflow` are registered for entity-context
  resolution in `go/internal/query/entity_content_types.go`
  (`resolveContentBackedEntityTypes` for the content-store fallback,
  `graphResolvableNotLanguageQueryableEntityTypes` for graph-label filtering).
  They are deliberately **not** language-queryable — Atlantis entities carry
  language `yaml`, which `language-query` does not accept — so they are absent
  from the `language-query` `entity_type` enum.
- **Typed relationships catalog**: `MANAGES`, `ATLANTIS_DEPENDS_ON`, and
  `USES_WORKFLOW` are registered verbs in the fixed relationships catalog
  (`go/internal/query/relationships_catalog_cypher.go`, `layer: infra`), so
  they are browsable and countable through `POST /api/v0/relationships/catalog`,
  `POST /api/v0/relationships/edges`, and the `list_relationship_edges` MCP
  tool -- the same surface `CALLS`, `IMPORTS`, and the other Terraform-family
  verbs (`PROVISIONS_DEPENDENCY_FOR`, `USES_MODULE`, `DISCOVERS_CONFIG_IN`)
  use. `MANAGES` targets a `Directory` node, which has no `id`/`uid`, only a
  canonical `path`; its edge-slice `target_id` resolves to that path (not the
  directory basename) so two same-named directories in different repositories
  stay distinguishable.

These edges do not carry a `source_tool` (Atlantis governance edges are
self-labeling, not cross-tool-correlated), so they do not appear in the
catalog's per-verb `source_tools` breakdown. This closes the query-surface gap
tracked in [#5369](https://github.com/eshu-hq/eshu/issues/5369).
