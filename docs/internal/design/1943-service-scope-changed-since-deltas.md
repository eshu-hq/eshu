# Service-Scope Changed-Since Deltas

Issue: #1943
Parent: #1797
Follow-up to: #1799 (merged repository-scope changed-since)

Status: **Design-only proposal. No runtime surface shipped.** Service-scope
deltas need a service-generation-lineage architecture decision before any
correct, bounded delta surface can be built. This document records the
investigation, names the owning store/read-model contract, explains precisely
why the #1799 model does not transfer, and recommends a contract for the
follow-up work.

## Summary

#1799 shipped bounded **repository-scope** changed-since deltas
(`GET /api/v0/freshness/changed-since`, the `get_changed_since` MCP tool, and
`eshu freshness changed-since`). That surface diffs a prior generation's fact
set against the current active generation's fact set for **one ingestion scope**,
keyed by `(scope_id, generation_id, stable_fact_key)`, into per-category
added/updated/unchanged/retired/superseded counts with bounded sample handles.

#1943 asks to extend changed-since to **service-scope** evidence families:
deployment, runtime, dependencies, docs, incidents, vulnerabilities, and
ownership. After investigating the service read-model and storage contracts,
**no service evidence family supports a correct, bounded, deterministic
`since -> current` delta today** without first introducing a service-scope
generation lineage. Per the repository rule "do not optimize or ship behavior
that has not been proven correct," all seven families are **deferred**. This
document is the design output and the recommended contract for the architecture
work that must land first.

## What a "service" is in Eshu

A repository is a first-class **ingestion scope**: one row in `ingestion_scopes`
with `scope_kind = 'repository'`, an `active_generation_id` pointer, and a
`scope_generations` history. Every ingest of that repository produces a new
generation under the same scope, so "prior generation -> current active
generation" is a well-defined, single-lineage diff. That is the foundation
#1799 stands on.

A **service** is not an ingestion scope. There is no
`ingestion_scopes` row with `scope_kind = 'service'`. The string `"service"`
appears only as:

- a node label / search-retrieval dimension
  (`go/internal/searchretrieval/retrieval.go`, `ScopeKindService`), and
- a payload attribute (`service_id`, `service_name`) on reducer-owned and
  source facts that live under **other** scopes.

A service is a **reducer-materialized correlation**: the
`service_catalog_correlation` reducer
(`go/internal/reducer/service_catalog_correlation.go`,
`service_catalog_correlation_writer.go`) and the kubernetes / supply-chain /
incident / observability correlators read source facts from many scopes and
project a correlated service identity into the graph and into reducer-owned
facts. The service dossier read-model
(`go/internal/query/service_story_dossier.go`,
`service_story_overview.go`, `entity_workload_context.go`) assembles a service's
deployment lanes, dependencies, evidence graph, and API surface from a
graph-materialized `workloadContext` that spans many source scopes and
generations.

The owning store/read-model surfaces for the requested families are:

| Family | Owning read surface | Backing facts / store |
| --- | --- | --- |
| Deployment evidence | `service_story_dossier.go` (`deployment_evidence.artifacts`), `repository_deployment_evidence_read_model.go` | graph-materialized deployment relationships across repo scopes |
| Runtime evidence | `service_story_dossier.go` deployment lanes / instances | graph-materialized runtime instances across cluster/environment scopes |
| Dependencies | `service_story_dossier.go` upstream/downstream | graph relationships across repo scopes |
| Docs | `documentation_target_read_model.go` | `documentation_source` scope facts keyed to `service_id` payload |
| Incidents | `incident_routing_evidence_loader.go` | PagerDuty / Jira provider scope facts keyed to `service_id` payload |
| Vulnerabilities | `supply_chain_advisory_evidence.go`, `supply_chain_impact_*` | scanner / advisory provider scope facts keyed to `service_id`/image payload |
| Ownership | `service_catalog_correlations.go` (`reducer_service_catalog_correlation` fact, `owner_ref`) | reducer-owned correlation facts under the source catalog scope |

## Why the #1799 generation-diff model does not transfer

The repository-scope diff relies on three properties. Service evidence has
none of them.

### 1. No single scope with one generation lineage

The #1799 diff is `(prior generation of scope X) vs (current active generation
of scope X)`. A service has no scope X and no `active_generation_id`. Each
family is loaded from many independent source scopes — every contributing repo,
every PagerDuty account scope, every scanner worker scope, every documentation
source scope — and each of those has its own generation timeline. There is no
single "prior state of the service" snapshot to diff against a single "current
state of the service" snapshot. A `since_generation_id` parameter has no
meaning for a service, because a service is not produced by one generation.

### 2. No stable diff key across generations for the correlated layer

#1799 works because **source** facts carry a generation-independent
`stable_fact_key` (for example `repository:repo-123`,
`terraform_state_resource:aws_instance.app`). The same logical entity keeps the
same key across generations, so a FULL OUTER JOIN of prior-keys against
current-keys classifies each key deterministically.

The reducer-owned service layer breaks this. The
`reducer_service_catalog_correlation` fact's `stable_fact_key` **embeds
`generation_id`**:

```
service_catalog_correlation:<scope_id>:<generation_id>:<provider>:<entity_ref>
```

(see `serviceCatalogCorrelationStableFactKey` in
`go/internal/reducer/service_catalog_correlation_writer.go`). Because the key
includes the generation, the *same logical correlation* gets a *new key every
generation*. A FULL OUTER JOIN on `stable_fact_key` would classify every
correlation as `added` in the current generation and `superseded` in the prior —
100% churn that is always wrong. There is no stable per-correlation identity to
match `updated`/`unchanged`.

### 3. No coherent per-service "current vs prior" snapshot

Even setting keys aside, the families are read by joining
`ingestion_scopes.active_generation_id = fact.generation_id` and
`generation.status = 'active'` (see `listServiceCatalogCorrelationsQuery` in
`service_catalog_correlations.go`). The read model only ever exposes the
**current active** correlation per source scope. There is no durable, queryable
"the service as of prior reference R" snapshot to compare against. The
`materialization_status` field (`identity_only` vs materialized) is a
qualitative status, not a versioned lineage. The catalog "generation" exposed by
`tools_service_catalog.go` is the **catalog source scope's** ingestion
generation, not a per-service generation.

### Consequence

Any service-scope delta built on today's stores would either:

- classify everything as churn (wrong, item #2), or
- silently report `unchanged` because it can only see the current active state
  and has no prior snapshot (wrong, and exactly the "hide stale/partial answers
  behind a convenience summary" non-goal in #1797), or
- require fanning out a `since` reference across every contributing source scope
  and inventing a per-service reconciliation of their independent generation
  timelines — an unbounded, undefined operation with no deterministic key.

None of these is a correct, bounded, deterministic delta. Shipping any of them
would violate the repository's accuracy life-motto and the "do not ship behavior
not proven correct" rule.

## Per-family decision

| Family | Diffable today? | Reason |
| --- | --- | --- |
| Deployment evidence | No | Graph-materialized across repo scopes; no per-service generation; relationship identity not generation-stable as a service-scoped key. |
| Runtime evidence | No | Instances materialized across cluster/environment scopes; no per-service snapshot lineage. |
| Dependencies | No | Graph relationships across repo scopes; same lineage gap as deployment. |
| Docs | No | `documentation_source` facts keyed to `service_id` payload across many doc scopes; no per-service generation to diff. |
| Incidents | No | Provider scope facts (PagerDuty/Jira) keyed to `service_id` payload; provider scope generations are not service generations. |
| Vulnerabilities | No | Scanner/advisory provider scope facts; advisory state changes on the provider's timeline, not a service generation. |
| Ownership | No | `reducer_service_catalog_correlation` fact key embeds `generation_id`; no stable cross-generation correlation key. |

All seven are deferred. No partial subset is shippable because the blocker
(absence of a service-scope generation lineage and a stable per-service-evidence
diff key) is common to all of them.

## Recommended contract for the follow-up architecture work

To make service-scope deltas correct and bounded, the platform needs a durable,
versioned **service materialization snapshot** that the reducer writes on each
service re-correlation, with:

1. **A service-scope lineage.** Either a `scope_kind = 'service'` row in
   `ingestion_scopes` with its own `active_generation_id` and
   `scope_generations` history, or a parallel
   `service_materialization_generations` table keyed by `service_id`. Each
   service re-correlation commits a new generation; the active pointer protects
   current reads exactly like repository scopes.

2. **A generation-stable per-evidence diff key.** Each family must emit a
   per-service evidence row keyed by a **generation-independent**
   `service_evidence_key` (for example
   `deployment:<service_id>:<resolved_relationship_id>`,
   `incident:<service_id>:<provider>:<incident_ref>`,
   `vulnerability:<service_id>:<advisory_id>`,
   `ownership:<service_id>:<owner_ref>`). The `reducer_service_catalog_correlation`
   key must be reworked to drop `generation_id` from the identity so the same
   correlation keeps its key across generations (the generation belongs in the
   row's generation column, never in the identity key — this is the same
   distinction the AWS materialization design at
   `docs/internal/design/1231-s3-external-principal-grant-projection.md` draws
   between identity keys and stored generation columns).

3. **A payload hash per evidence row** so `updated` vs `unchanged` is detected
   the same way #1799 uses `md5(payload::text)`.

4. **Explicit retirement.** Service-evidence rows that drop out of a new
   materialization generation must be tombstoned (retired) or recorded as
   superseded, never silently absent, so the delta never collapses
   retired/superseded into unchanged.

5. **The #1799 envelope and classification reused verbatim.** Once a
   service-scope generation lineage and stable keys exist, the delta computation
   is the same FULL OUTER JOIN classification (`added`/`updated`/`unchanged`/
   `retired`/`superseded`, plus per-category `unavailable`) over the new
   snapshot rows, with the same bounded `sample_limit`, deterministic ordering,
   truncation flag, not-found vs unavailable handling, and `WriteSuccess`
   truth envelope. Extend `status.ChangedSinceCategory` with the service
   families rather than inventing a parallel shape, exactly as #1943 asks.

This is an architecture change to the reducer materialization path and the
storage schema (a new generation lineage and snapshot table), not a
query-surface addition. It must be designed and reviewed as such, with the
reducer/projector owners, before any route, MCP tool, CLI subcommand, or
OpenAPI path is added.

## Why this is recorded as design-only instead of a partial surface

The #1943 implementation policy is explicit: implement only the families that
can be proven correct with a stable diff key and a failing-first test; for the
rest, return an explicit `unavailable` or defer; and "if NO category supports a
correct bounded service-scope delta (the feature needs an architecture decision
about service-scope generation lineage), STOP coding, write the design findings
+ a recommended contract, open the PR as a DESIGN-ONLY proposal." The
investigation lands squarely in that last branch: the blocker is a missing
service-scope generation lineage, common to all seven families. A correct design
plus deferral is the honest, correct outcome; a runtime surface that reports
all-churn or false-unchanged would be a product accuracy failure.

## Follow-up

The architecture work (service materialization snapshot lineage + stable
per-evidence keys, then the delta surface) is tracked under #1943 / #1797. This
document is the design input for that work.

## Related

- `docs/public/reference/incremental-freshness-model.md` — repository-scope
  changed-since contract (the surface this design extends).
- `docs/internal/design/1231-s3-external-principal-grant-projection.md` — prior
  art on identity keys vs stored generation columns.
- #1799 — merged repository-scope changed-since deltas.
- #1797 — incremental freshness epic.
