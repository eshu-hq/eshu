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
- Unit test suite: `go/internal/parser/yaml/atlantis_test.go`
- Integration validation: B-7 golden-corpus gate asserts the `AtlantisProject`
  node (required node label) plus the `MANAGES` (`rc-5`) and `ATLANTIS_DEPENDS_ON`
  (`rc-6`) governance edges (`scripts/verify-golden-corpus-gate.sh`)

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
| Custom workflow reference | `atlantis-workflow` | supported | `atlantis_projects` | `workflow` | `property:AtlantisProject.workflow` | `go/internal/parser/yaml/atlantis_test.go::TestParseAtlantisConfigEmitsProjectRows` | B-7 golden-corpus gate | The named custom workflow; workflow bodies are modeled in phase 2. |
| Repo locks mode | `atlantis-repo-locks` | supported | `atlantis_projects` | `repo_locks_mode` | `property:AtlantisProject.repo_locks_mode` | `go/internal/parser/yaml/atlantis_test.go::TestParseAtlantisConfigEmitsProjectRows` | B-7 golden-corpus gate | - |
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

## Custom workflows (phase 2)

Modeling `atlantis.yaml` `workflows:` (custom plan/apply/import/policy_check
stages) as `AtlantisWorkflow` nodes with `USES_WORKFLOW` edges is tracked
separately.
