<!-- SPDX-License-Identifier: MIT -->
<!-- Copyright (c) 2025-2026 eshu-hq -->

# Evidence: Atlantis project node materialization (governance MVP)

## Change

Adds first-class support for the Atlantis Terraform PR-automation tool. A
repo-level `atlantis.yaml` is parsed (new `go/internal/parser/yaml/atlantis.go`,
dispatched by filename) into one `AtlantisProject` content entity per project,
carrying its governance fields (dir, workspace, terraform_version, autoplan,
apply/plan/import requirements, depends_on, execution_order_group, workflow,
repo_locks_mode). The label is registered in the content-shape bucket tables, the
three projector label allowlists, and the graph schema (uniqueness constraint +
`infra_search_index` membership).

## Performance

No-Regression Evidence: This change adds one new entity label that flows through
the existing content-materialization and canonical-node-projection paths; it adds
no new query, graph-write loop, or per-node fan-out. The parser work is a single
extra filename check (`isAtlantisConfig`) per YAML file plus a bounded one-pass
extraction over the `projects` list of an `atlantis.yaml` (typically a handful of
entries), reusing the same per-bucket materialization the other ~60 entity labels
already use. The added graph-schema statements are one uniqueness constraint and
one label added to the existing infra fulltext index, applied once at schema
init. The B-7 golden-corpus gate drains the full corpus (drain → maintenance →
drain) in ~38s with `fact_work_items_residual=0`, unchanged from before, and now
additionally materializes `AtlantisProject` (count=2) and `AtlantisWorkflow`
(count=1) nodes plus the `MANAGES` (`rc-5`, count=2), `ATLANTIS_DEPENDS_ON`
(`rc-6`, count=1), and `USES_WORKFLOW` (`rc-7`, count=1) governance edges, with no
measurable change to drain time.

The three governance edges run in the canonical structural-edge phase
(`atlantisEdgeStatements`), guarded so they emit nothing for non-Atlantis repos.
They are resolved in Go from the AtlantisProject/AtlantisWorkflow entity metadata
and matched by canonical key (`AtlantisProject.uid` / `Directory.path` /
`AtlantisWorkflow.uid` — the same UNWIND/MERGE pattern as the existing IMPORTS
edge), avoiding bound-variable property matching; fan-out is one row per project
(MANAGES), one per `depends_on` entry (ATLANTIS_DEPENDS_ON), and one per workflow
reference (USES_WORKFLOW), bounded by the number of projects in one
`atlantis.yaml`. Workflow parsing captures per-stage step kinds only — run-step
command bodies are deliberately not stored as node properties.

## Observability

No-Observability-Change: `AtlantisProject` entities flow through the same
content-materialization and canonical-node-writer instrumentation as every other
entity label (the existing `eshu_dp_*` projection/materialization counters and
spans already cover the per-label node write phase). No new metric, span, or log
key is introduced; an operator sees Atlantis node materialization through the
same per-generation projection signals as Terraform, ArgoCD, and CloudFormation
entities.
