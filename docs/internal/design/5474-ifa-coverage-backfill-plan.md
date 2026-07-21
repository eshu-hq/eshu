# Ifá Coverage Backfill Plan — Issue #5474 D3

This document defines the per-family backfill plan for Ifá (`ifa_fact_kind`)
coverage, part of epic #5470. It records the current baseline, assigns
ownership, sequences the work, and documents cross-epic coordination.

## Baseline (2026-07-20)

```
go run -C go ./cmd/ifa coverage ...
summary: 12 pass, 0 required-fail, 172 advisory-warn

== coverage (advisory) ==
  ifa_fact_kind             11/176 satisfied (6.25%)  uncovered=165
  ifa_narrowed_correlation   1/8   satisfied (12.50%)  uncovered=7
  TOTAL                     12/184 satisfied (6.52%)  gaps=172
```

11 of 176 fact_kind entries are covered by Ifá fixture-pack Odù entries today:

| Kind | Odù |
| --- | --- |
| `aws_resource` | `odu:aws-pack` |
| `aws_resource_policy_permission` | `odu:aws-pack` |
| `aws_security_group_rule` | `odu:aws-pack` |
| `aws_tag_observation` | `odu:aws-pack` |
| `aws_warning` | `odu:aws-pack` |
| `gcp_cloud_relationship` | `odu:demo-org-roundtrip` |
| `gcp_cloud_resource` | `odu:demo-org-roundtrip` |
| `gcp_collection_warning` | `odu:demo-org-roundtrip` |
| `gcp_dns_record` | `odu:demo-org-roundtrip` |
| `gcp_iam_policy_observation` | `odu:demo-org-roundtrip` |
| `repository` | `odu:repo-dependency-concurrency` |

The task spec's 22-kind reconstruction covers:

| Family | Kinds | Current Odù coverage |
| --- | --- | --- |
| `terraform_state` | 8 | 0/8 |
| `package_registry` | 9 | 0/9 |
| `kubernetes_live` | 3 | 0/3 |
| `scanner_worker` | 2 | 0/2 |
| **Total** | **22** | **0/22** |

Note: the 2026-07-20 collector-truth audit is not committed; this
reconstruction names the four families explicitly and marks the 22-kind
total as reconstructed, not audited.

## Per-family backfill sequencing

Epic #5470 requires framework proof before corpus proof. Each family's
backfill therefore follows the same ladder:

1. **Framework proof** — write one Odù for the family covering one
   representative kind; prove the Ifá gate goes from red to green for
   that kind alone. The framework proof exercise is the pattern for the
   remaining kinds in the same family.
2. **Corpus proof** — backfill the remaining kinds for the family using
   the same pattern. All kinds in the family should share one fixture
   pack, one replay scenario, and one cassette entry.

### terraform_state (8 kinds)

| Kind | Status |
| --- | --- |
| `terraform_state_module` | needs Odù |
| `terraform_state_output` | needs Odù |
| `terraform_state_resource` | needs Odù |
| `terraform_state_snapshot` | needs Odù |
| `terraform_state_tag_observation` | needs Odù |
| `terraform_state_candidate` | needs Odù |
| `terraform_state_provider_binding` | needs Odù |
| `terraform_state_warning` | needs Odù |

**Owner:** #5470 (epic) — assign to first available slot in the family
backfill workstream.

### package_registry (9 kinds)

| Kind | Status |
| --- | --- |
| `package_registry.package` | needs Odù |
| `package_registry.package_artifact` | needs Odù |
| `package_registry.package_dependency` | needs Odù |
| `package_registry.package_version` | needs Odù |
| `package_registry.registry_event` | needs Odù |
| `package_registry.repository_hosting` | needs Odù |
| `package_registry.source_hint` | needs Odù |
| `package_registry.vulnerability_hint` | needs Odù |
| `package_registry.warning` | needs Odù |

**Owner:** #5470 — see above.

### kubernetes_live (3 kinds)

| Kind | Status |
| --- | --- |
| `kubernetes_live.pod_template` | needs Odù |
| `kubernetes_live.relationship` | needs Odù |
| `kubernetes_live.warning` | needs Odù |

**Owner:** #5470 — see above.

### scanner_worker (2 kinds)

| Kind | Status |
| --- | --- |
| `scanner_worker.analysis` | needs Odù |
| `scanner_worker.warning` | needs Odù |

**Owner:** #5470 — see above.

## Cross-epic coordination

### vulnerability_intelligence (11 kinds — #5462 owns)

The `vulnerability_intelligence` family's 11 kinds belong to epic #5462
(vulnerability intelligence typed-payload migration and coverage).
Their Ifá coverage backfill must precede any behavior change on that
path to avoid regressing the existing post-migration guardrails.

This plan does NOT double-own those kinds. Issue #5462's own completion
criteria already include Ifá coverage backfill for the family's kinds.
When #5462's authoring task creates the Odùs, they follow the same
framework-proof-before-corpus ladder (#5470 rules).

### Workstream coordination

| Family | Owner epic | Framework proof | Corpus proof |
| --- | --- | --- | --- |
| `terraform_state` (8) | #5470 | TBD | TBD |
| `package_registry` (9) | #5470 | TBD | TBD |
| `kubernetes_live` (3) | #5470 | TBD | TBD |
| `scanner_worker` (2) | #5470 | TBD | TBD |
| `vulnerability_intelligence` (11) | #5462 | #5462 owns | #5462 owns |

## Baseline progress tracking

Starting point (2026-07-20): 12/184 total satisfied (6.52%).

| Metric | Start | After 22-kind backfill | After #5462 |
| --- | --- | --- | --- |
| `ifa_fact_kind` satisfied | 11/176 (6.25%) | 33/176 (18.75%) | 44/176 (25.0%) |
| `ifa_narrowed_correlation` | 1/8 (12.50%) | 1/8 (12.50%) | TBD |
| **TOTAL** | **12/184 (6.52%)** | **34/184 (18.48%)** | **45-52/184 (24.5-28.3%)** |

## Related documents

- `specs/ifa-coverage-manifest.v1.yaml` — the 12-row Ifá manifest.
- `go/cmd/ifa/` — the Ifá coverage CLI.
- `specs/fixture-packs/` — Odù definitions.
- [#5470](https://github.com/eshu-hq/eshu/issues/5470) — Spine epic.
- [#5462](https://github.com/eshu-hq/eshu/issues/5462) — Vulnerability intelligence migration.
- [#5474](https://github.com/eshu-hq/eshu/issues/5474) — This issue (gate extensions).
