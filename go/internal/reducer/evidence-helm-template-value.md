# Evidence: Helm template-value HELM_VALUE_REFERENCE edge (rc-35, HELM_TEMPLATE_VALUE_REFERENCE)

This note covers the net-new Helm template-variable extraction that links a
chart template `{{ .Values.<dotted.path> }}` usage to the matching leaf key in
the same chart's `values.yaml`, materialized as a dedicated
`HELM_VALUE_REFERENCE` edge carrying the `HELM_TEMPLATE_VALUE_REFERENCE` evidence
kind. It is the evidence anchor for
the hot-path-by-location files this change touches
(`go/internal/reducer/cross_repo_evidence_type.go`,
`go/internal/storage/cypher/canonical_helm_template_value_edges.go`,
`go/internal/projector/canonical.go`, `go/internal/collector/git_snapshot_native.go`,
`go/internal/graph/schema_tables.go`).

## Model

- Parser (`go/internal/parser/yaml/helm_template_values.go`): scans
  `templates/*.yaml` line-by-line for `{{ .Values.<dotted.path> }}` (regex, not a
  YAML decode — templates carry Go-template control syntax) and flattens
  `values.yaml` leaf keys with their source lines.
- Two new content-entity buckets become two new node labels via the proven
  parser-bucket -> shape-table -> projector-label -> dynamic node-writer path
  (same mechanism that materializes `SqlTable`, `AtlantisProject`,
  `GitlabPipeline`): `HelmValueDefinition` (a `values.yaml` leaf) and
  `HelmTemplateValueUsage` (a `.Values` usage in a template).
- The usage -> definition edge is resolved in Go in the projector
  structural-edge phase (mirroring the Atlantis `MANAGES` and GitLab
  `DEFINES_JOB` edges), matched by uid, scoped per chart, and written as
  `(HelmTemplateValueUsage)-[:HELM_VALUE_REFERENCE {evidence_kinds:["HELM_TEMPLATE_VALUE_REFERENCE"], source_tool:"helm"}]->(HelmValueDefinition)`.
  A dedicated `HELM_VALUE_REFERENCE` type is used rather than the shared
  `REFERENCES` type. This is a performance requirement (#4464, #4476): on NornicDB
  a relationship DELETE scales with the relationship-TYPE index size, so a
  full-refresh retract against the shared `REFERENCES` type (which also holds
  every code-symbol call, ~52k at repo scale) is O(all-REFERENCES) per delete and
  blows the canonical-write budget. A dedicated type keeps the retract's
  delete-index tiny. The `evidence_kinds` property, normalized `source_tool`, and
  the `helm_template_value_reference` `call_kind` remain for evidence isolation.

## No-Regression Evidence

No-Regression Evidence:

- The edge is produced in the existing `structural_edges` projection phase
  alongside the Atlantis/GitLab structural edges; it adds at most one UNWIND
  MERGE (grouped) and one generation-guarded retract per chart, both bounded by
  the `.Values` usage count in a single chart's templates. The retract runs as a
  standalone autocommit statement (Drain-marked) rather than inside the grouped
  ExecuteWrite transaction: an UNWIND relationship DELETE inside an explicit
  transaction silently no-ops on commit (#4476), so autocommit is required for
  the deletes to persist. No new worker, lease, batch, or concurrency knob is
  introduced.
- The builder (`helmTemplateValueEdgeStatements`) returns `nil` for any
  materialization with no `HelmTemplateValueUsage`/`HelmValueDefinition`
  entities, so non-Helm repos pay only a single map/slice scan already performed
  over `mat.Entities` for the sibling Atlantis/GitLab builders — no measurable
  cost on the dominant (non-Helm) corpus shape.
- The retract targets the dedicated `HELM_VALUE_REFERENCE` type scoped by the
  `HelmTemplateValueUsage` source label with a generation guard, so it only ever
  touches the small Helm value edge population — never the large shared
  code-symbol `REFERENCES` index; re-projection is idempotent (MERGE re-writes
  the current edge after the stale prior-generation edge is dropped).
- The `source_tool` write is one additional scalar `SET` on the same bounded
  `UNWIND` row used for `evidence_kinds`; it does not add a match, merge,
  traversal, batch, or transaction. The C-4 snapshot now asserts this property
  for rc-35, so a missing or non-canonical token fails the same graph proof that
  already checks the Helm template-value `HELM_VALUE_REFERENCE` edge.
- Baseline vs after: the B-7 golden corpus gate
  (`scripts/verify-golden-corpus-gate.sh`) over the 18-repo corpus on the
  NornicDB backend stays within its existing 900s baseline / 1800s ceiling
  wall-time budget with rc-35 added; the new edge adds two entity labels and one
  edge between two surviving nodes for the single `helm-template-chart` fixture,
  which is negligible against the corpus node/edge totals.

## No-Observability-Change

No-Observability-Change:

This change adds no new metric, span, or status field. The new entities and edge
flow through the same projector entity-count and structural-edge-count telemetry
that already reports Atlantis/GitLab/SQL structural materialization (the
runtime-stage entity + structural-edge counters), so the new `HelmValueDefinition`
/ `HelmTemplateValueUsage` node writes and the `HELM_VALUE_REFERENCE` edge write are
already visible to an operator via the existing projection counters with no
additional instrumentation required.
