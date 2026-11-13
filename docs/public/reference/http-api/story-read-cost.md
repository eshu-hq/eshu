# Story read cost (#7126)

Story routes keep their Postgres reads bounded as corpora grow; this page
records the read shapes, row caps, and measured costs. Route contracts stay on
[Story routes](story-routes.md).

Three story-route reads were the size- or corpus-scaled Postgres cost of the
repository and service stories; none is graph work.

- Documentation target facts (both stories) are read as two bounded branches
  joined by `UNION ALL`: the mention and claim kinds, which the partial GIN
  `fact_records_documentation_target_refs_idx` covers, and the
  `semantic.documentation_observation` kind on its own, which migration 124's
  partial GIN `fact_records_documentation_semantic_target_refs_idx` covers (same
  `jsonb_path_ops` expression, restricted to that kind and non-tombstoned
  facts). The single earlier statement listed all three kinds, which the index
  predicate does not cover, so the planner never used the index and filtered
  every documentation fact. Rows, ordering, and limit are unchanged.
- Repository coverage derives the entity total, newest `indexed_at`, and type
  distribution from one grouped `content_entities` pass instead of three scans.
- Repository story lists the repository's files once (the semantic overview
  stage) and shares that list with the infrastructure, deployment, narrative,
  and CI/CD stages. The `content_files` stage log is gone; `semantic_overview`
  now reports `file_count`.

The semantic overview and file list stay capped at 5,000 rows and read one
sentinel row more: only then does the story add
`repository_semantic_read_truncated_at_5000` to `limitations` and set
`answer_metadata.truncated`. With it, every file-derived stage is a lower bound.

Deployment artifacts (#7951): each repository hydrates its first 50
config-artifact candidates (all if fewer) plus a share of a 12,000 budget, drawn
anchor first, then in repository-id order; each related list reads a sentinel row.
`deployment_artifacts_candidates_budget_reached` (budget spent, candidates left
unread) and `deployment_artifacts_related_file_list_truncated_at_5000` go to
`limitations` (story, service story; both then set `truncated`) or
`partial_reasons` (context); service routes add
`deployment_artifacts_anchor_file_list_truncated_at_5000`. Counts:
[logs](../telemetry/logs.md).

Measurements, the migration 122 to 124 write-cost figures and proof commands:
`docs/internal/evidence/7126-story-read-cost.md`.
