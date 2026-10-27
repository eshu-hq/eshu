# Grant-scoped unanchored security-alert reads (#8837)

**A scoped token's unanchored security-alert empty answer is now classified
over the caller's granted repositories, not every configured target. A
granted repository no target covers reports
`repository_outside_target_set`.**

## Problem

When an empty security-alert answer carried no `repository_id`, the
collection-state check evaluated every configured collector target, not the
repositories the caller may read. If every configured target was healthy,
the answer was `complete` even when none of the caller's granted
repositories was inside a target. The reasons it returned also reflected the
health of targets the caller cannot read.

## Change

- `querycontract.ContentStore.MatchRepositoriesByIDs` is the one bounded
  catalog read the classifier maps grants with: one statement however many
  grants the caller holds, matching a canonical repository id or an
  ingestion scope id on repository-kind `ingestion_scopes` rows.
- `chain.securityAlertGrantSlugs` maps the grant set to normalized
  owner/name slugs. Repository grants and git repository scope grants (via
  `CanonicalRepositoryIDForScopeID`) must each map to a slug; a grant that
  maps to nothing is unmapped. Other scope grants name no repository and are
  ignored. Past 5,000 ids the read fails closed with the bound error.
- `chain.readSecurityAlertGrantVerdict` classifies a scoped unanchored read
  over the granted slugs only. Each slug takes its best covering target; the
  answer takes the worst slug. The per-slug state is narrowed to that slug,
  so the #8838 uncollected-generation downgrade and the #8943 unread reasons
  apply per repository. Reasons union across slugs.
- Unscoped unanchored reads still evaluate every configured target.

## Proof

- `TestSecurityAlertGrantScopeCoverage` (30 subtests over the list, count,
  and inventory routes): in-target, scope-grant, out-of-target, mixed,
  collecting-plus-outside, catalog-unknown, mapped-plus-unknown,
  #8838-added-after-generation, and #8943 unread-isolation cases failed
  first and pass. Two deliberate mutants (dropping the unmapped poison,
  dropping the per-slug unread narrowing) each fail the case that pins
  them.
- `TestSecurityAlertGrantSlugs` pins the mapping edges; the bound case
  proves fail-closed past 5,000 ids.
- `TestMatchRepositoriesByIDsLive` (PostgreSQL 18.6, isolated bootstrapped
  schema) proves the real query maps id and scope-id grants and excludes a
  non-repository-kind row carrying the same repo_id.

## Grant-size measurement (acceptance criterion)

`TestMatchRepositoriesByIDsPlanLive` runs the shipped query text over a
10,000-repository catalog (plus one organization-kind decoy), `VACUUM
ANALYZE`d, on PostgreSQL 18.6. Grant ids alternate repository and scope ids.
Third warm run figures from the test log:

| Grant size | Plan | Buffers | Execution |
|---|---|---|---|
| 1 | Seq Scan, Sort | 264 shared hit | 1.229 ms |
| 100 | Seq Scan, Sort | 264 shared hit | 1.710 ms |
| 5,000 | Index Scan on ingestion_scopes_pkey, no sort | 2,202 shared hit | 3.082 ms |

At 5,000 ids the planner switches to the primary-key probe for the
`scope_id = ANY` disjunct and drops the sort. All three fit the 1 s
collection-state probe with two orders of magnitude of headroom, so no new
index is warranted: the id expression (`coalesce(payload->>...)`) has no
index by design, and the catalog holds one row per repository. The read
returns exactly the granted rows at every size.

No-Observability-Change: the grant path emits the existing
`security_alert_collection_state` stage event with the same closed
`coverage_state`, `coverage_reason`, and `collector_state` tokens
(`repository_outside_target_set` already exists), and counts through the
existing `eshu_dp_query_security_alert_empty_answers_total` counter. No new
reason token, so the OpenAPI enum is unchanged.
