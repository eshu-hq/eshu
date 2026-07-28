# #5426 — what the golden corpus asserts about environment_evidence

Companion to
[#5426 — corroborated versus declared-only deployment environments](5426-corroborated-vs-declared-environment.md).
That document carries the design narrative and the package-level verification.
This one carries the B-7 golden-corpus record: what the corpus now asserts end
to end, the two blockers that had to be fixed to get there, and what it still
does not assert.

## What the corpus now asserts

The `list_supply_chain_impact_findings` MCP shape gained two pins on the
CVE-2026-00010 finding, alongside the `minimum_results: 1` it already carried:

```json
"findings[].environments[]": "prod",
"findings[].environment_evidence.prod": "deploy_event"
```

plus `environments` and `environment_evidence` under
`result_item_required_fields`. Both fields are `omitempty` on the finding row,
so requiring the key AND pinning the value is what keeps this non-vacuous: an
empty map drops the key entirely and the shape goes red.

```
[PASS] mcp:list_supply_chain_impact_findings: "findings" has 1 results;
  item fields [cve_id environment_evidence environments impact_status
  repository_id runtime_context subject_digest] present;
  json paths [], values [findings[].environment_evidence.prod
  findings[].environments[] findings[].repository_id
  findings[].runtime_context.deployment_ids[]
  findings[].runtime_context.truth_basis
  findings[].runtime_context.workload_ids[] findings[].subject_digest]
```

These are the BAKED fields `applySupplyChainRuntimeContext` writes, distinct
from the read-time-resolved `runtime_context` the same shape already pinned. The
chain they assert runs end to end:

1. the `cicdrun` cassette's two `ci.deployment_event` facts resolve the
   `github_actions` run to the canonical `prod` token (#5425),
2. the run's `ci.artifact` digest equals the finding's `subject_digest`,
3. `matchingSupplyChainDeployments` ACCEPTS that correlation rather than
   rejecting it as provenance-only, and
4. `recordSupplyChainEnvironmentEvidence` labels the environment
   `deploy_event`.

Break any link and one of the two fields disappears from the response.

The finding's persisted payload at the end of that run:

```
cve_id                | CVE-2026-00010
repository_id         | repository:r_217415d9
environments          | ["prod"]
environment_evidence  | {"prod": "deploy_event"}
runtime_reachability  | deployed_image
missing_evidence      | ["service evidence missing", "workload evidence missing"]
```

## Two blockers, both measured on the live corpus

Neither was a fixture gap. Both were reproduced against a `--keep` gate run's
Postgres, fixed, and re-proven. No cassette byte changed.

### 1. The persisted image identity lost its CI build provenance

`matchingSupplyChainDeployments` rejects a `provenance_only` correlation, and
the corpus's correlation was exactly that. The reason sat one hop upstream:

```
$ SELECT scope_id, payload->>'source_repository_ids',
         payload->>'build_provenance_repository_ids'
    FROM fact_records WHERE fact_kind='reducer_container_image_identity'
     AND payload->>'digest' = 'sha256:abcdef...ab';

ci_cd_run:github_actions:eshu-hq:supply-chain-demo | ["repository:r_217415d9", "repository:r_69256c06"] | (null)
git-repository-scope:repository:r_217415d9        | ["repository:r_217415d9"]                           | (null)
... 14 more rows, every one (null)
```

Sixteen rows for that digest and not one carried build provenance, so
`cicdImageMatchesForRepository` — which joins on
`build_provenance_repository_ids` and nothing else — had no key to narrow with.
The unfiltered 16-row match classified `ambiguous`:

```
outcome=ambiguous provenance_only=true environment=prod environment_evidence=deploy_event
reason="artifact digest matches multiple container image identity rows"
```

and the finding recorded the rejection:

```
environments=[]  environment_evidence=(null)
missing_evidence=["deployment evidence provenance-only", "deployment exposure evidence missing", ...]
```

The cause is an asymmetry #5808 left behind. A `ci.artifact` carries a digest
and no image reference, so it reaches identity through the bare-digest ref, and
#5808 taught that path to keep `buildProvenanceRepositoryIDs`. But when a
deploying repository also names the same digest by explicit image reference,
both refs classify to the same `(image_ref, outcome)` pair, share one
`containerImageIdentityStableFactKey`, and the explicit-reference decision wins
the upsert — so the row Postgres keeps is the one #5808 never touched. That
decision reaches the CI run only through `applyCIRunDigestRevision`, which
copied the run's repository into `SourceRepositoryIDs` and left
`BuildProvenanceRepositoryIDs` alone, unlike its sibling
`applySLSADigestRevision`. The two-repository `source_repository_ids` in the row
above is that function's fingerprint.

**Fix:** `applyCIRunDigestRevision` now confers build provenance as well
(`go/internal/reducer/container_image_identity_registry.go`).
`recordCIRunDigestAnchor` files a repository there only from a `ci.artifact`
whose `artifact_type` is `container_image` and whose run reported PRODUCING that
digest, so the attribution is build evidence by construction — the same evidence
`addContainerImageDigestRef` already treats that way.

Failing-then-green, in
`go/internal/reducer/container_image_ci_run_digest_provenance_test.go`:

- `TestCIRunDigestAnchorConfersBuildProvenanceOnCompetingImageRefDecision`
  BEFORE: `BuildProvenanceRepositoryIDs = []string(nil)` on the
  `"image reference named a digest observed in registry facts"` decision.
- `TestCIRunDigestBuildProvenanceLetsCorrelationEscapeProvenanceOnly`
  BEFORE: `Outcome = "ambiguous" (artifact digest matches multiple container
  image identity rows)`. That test emulates the writer's last-write-wins upsert
  and adds deploy-only sibling rows deliberately: a single-row fixture
  classifies `exact` even with the provenance dropped, which is the false-green
  shape that let this survive.

AFTER, on the live corpus:

```
ci_cd_run:github_actions:eshu-hq:supply-chain-demo | [...r_217415d9, ...r_69256c06] | ["repository:r_69256c06"]

outcome=exact provenance_only=false environment=prod environment_evidence=deploy_event
reason="artifact digest matches one container image identity row"
```

The `findings[].repository_id: repository:r_217415d9` pin in the same shape is
unaffected, by design rather than luck: `supplyChainImageIdentityAnchorTier`
reads `source_repository_ids` first, and the CI scope's row names two
repositories there. The new build provenance moves that row from tier C
(unresolvable) to tier B (resolvable only via its own build provenance); it can
never reach tier A. The fifteen agreeing deploy-only rows stay at tier A, and
tier A never loses to tier B (#5813).

#### Why #5829's stated blocker does not apply

This fix is the change #5829 tracks, and #5829 is titled "blocked on #5827". Its
body argues that widening `BuildProvenanceRepositoryIDs` "would make the graph
worse in order to make a correlation sharper", because that field is the sole
gate on two graph writers (`containerImageBuiltFromRows`,
`containerImageDerivedFromRows`), and because #5827 is open: `MERGE` matches on
`(start, end, type)` while `projectContainerImageBuiltFromEdges` retracts per
`(scope_id, generation_id, evidence_source)`, so two scopes emitting the same
`(digest, repository)` pair means one scope's retract deletes an edge the other
still supports.

That argument rests on the widening being cross-scope. It is not. Both halves
were checked here rather than assumed:

**The widening is INTRA-scope.** `ci.run` and `ci.artifact` are in
`containerImageIdentityFactKinds()`
(`go/internal/reducer/container_image_identity.go`), which is passed to
`loadFactsForKinds(ctx, h.FactLoader, intent.ScopeID, intent.GenerationID, ...)`
— a scope-local load. They are absent from every arm of `identityFactFilterSQL`,
the filter `listActiveContainerImageIdentityFactsQuery` shares with the epoch
probe (`go/internal/storage/postgres/facts_active_container_image_identity.go`):
that filter admits `oci_registry.image_tag_observation` / `.image_manifest` /
`.image_index`, `aws_image_reference`, `azure_image_reference`,
`gcp_image_reference`, `aws_relationship`, `content_entity`, and `file`, and no
CI kind at all. The handler's only other cross-scope loads are narrower still:
`ListActiveContainerImageSLSAFacts` is `source_system = 'sbom_attestation'` over
three `attestation.*` kinds, and `ListActiveRepositoryFacts` is
`fact_kind = 'repository'`. So a CI run's digest anchor reaches exactly one
intent — the CI scope's own — and no other scope can confer this provenance.
#5829's "the widening is specifically cross-scope" premise does not hold, which
makes its AC#1 and AC#4 moot.

**The emitted row SET is unchanged.** Measured on the corpus-shaped fixture
(`ciDigestProvenanceEnvelopes`), `containerImageBuiltFromRows` went from 1 row
to 2 IDENTICAL rows — both
`(sha256:abcdef...ab, repository:r_69256c06)` — because both decisions for that
digest now carry the provenance and the second is the one Postgres keeps. No new
`(start, end, type)` pair, no second scope, so #5827's retract/MERGE collapse is
not reachable from this change. The duplicate itself is now collapsed by
cross-decision dedup in `containerImageBuiltFromRows`, so the batch carries the
pair once; that is a payload and counter fix, not a correctness one, since
`MERGE` was already idempotent over it.

Both properties are pinned so a later change cannot quietly break them:
`TestContainerImageBuiltFromRowsPinCICompetingRefDigestToOneRepositoryPair` and
`TestContainerImageBuiltFromRowsEmitNothingForDeployOnlyScope`
(`go/internal/reducer/container_image_provenance_edges_test.go`) assert the
per-decision pair, the single distinct pair across decisions, the single row
after dedup, and that a deploy-only scope emits nothing.
`TestContainerImageDerivedFromRowsStayEmptyForCIRunScope`
(`go/internal/reducer/container_image_derived_from_edges_test.go`) covers the
other gated edge: a `ci_cd_run` scope resolves to an empty `owningRepositoryID`,
owns no Dockerfile, and returns `nil` before the widened child gate is
consulted — so DERIVED_FROM cannot grow a row from this change either.

The per-decision pair assertion is the failing-then-green one. Reverted via a
`go test -overlay` that swaps in the pre-fix
`container_image_identity_registry.go` (leaving every other file at branch HEAD):

```
$ go test -overlay=<overlay>.json ./internal/reducer \
    -run 'TestContainerImageBuiltFromRowsPinCICompetingRefDigestToOneRepositoryPair|TestContainerImageBuiltFromRowsEmitNothingForDeployOnlyScope|TestContainerImageDerivedFromRowsStayEmptyForCIRunScope' \
    -count=1 -v
--- PASS: TestContainerImageBuiltFromRowsEmitNothingForDeployOnlyScope (0.00s)
--- PASS: TestContainerImageDerivedFromRowsStayEmptyForCIRunScope (0.00s)
    container_image_provenance_edges_test.go:314: decision
      "ghcr.io/eshu-hq/supply-chain-demo@sha256:abcdef...ab"
      (image reference named a digest observed in registry facts)
      emitted BUILT_FROM rows []map[string]interface {}{},
      want exactly one map[string]interface {}{"digest":"sha256:abcdef...ab",
      "repository_id":"repository:r_69256c06"}
--- FAIL: TestContainerImageBuiltFromRowsPinCICompetingRefDigestToOneRepositoryPair (0.00s)
```

All three pass at branch HEAD. The deploy-only and DERIVED_FROM assertions pass
under the overlay too, and that is expected: they are invariants that hold in
both states by construction — the guard, not the repro.

#5827 stays open on its own merits. It is simply not a blocker for this change.

### 2. The impact finding was never replayed after the correlation improved

Fixing the correlation was not enough. A gate run carrying only that fix
produced

```
outcome=exact provenance_only=false environment=prod environment_evidence=deploy_event
```

while the finding for the SAME digest still reported `environments=[]` and
`"deployment evidence provenance-only"`, and the new assertion failed with
`result item missing required field "environment_evidence"`.

`supply_chain_impact` is triggered by its own vulnerability scope's facts
(`projector/supply_chain_impact_intents.go`), and nothing re-triggers it when a
correlation in another scope later improves. The correlation itself lands
`derived` on its first execution — the identity rows it joins against have not
committed yet — and reaches `exact` only on a maintenance replay, which
`bootstrap-index` already performs for `container_image_identity` and
`ci_cd_run_correlation`. `supply_chain_impact` is the third link in that chain,
and `crossScopeDependencyCatalog`'s own doc already said so
("supply_chain_impact reads the correlation output for its deployment context,
one hop further along the same chain") — it was simply never replayed, so a
finding classified against a provenance-only correlation kept that verdict for
good.

**Fix (first attempt, too narrow):** `supply_chain_impact` joined the reopen
list in `go/cmd/bootstrap-index/bootstrap_pipeline.go`. That made the gate go
green and was still wrong, for the reason the next section records.

### 3. The reopen the gate proves is not the reopen production runs

Codex raised this as a P1 on PR #5846, and it holds.

`ReopenSucceededReducerWorkItems` had exactly one production caller:
`go/cmd/bootstrap-index/bootstrap_pipeline.go`. The live ingester does not run
that binary. It calls
`RunDeferredRelationshipMaintenanceAfterShardDrain` →
`RunDeferredRelationshipMaintenance`, which replayed only `deployment_mapping`
and `code_import_repo_edge`
(`go/internal/storage/postgres/ingestion_reopen_deployment_mapping.go`).

So `container_image_identity`, `ci_cd_run_correlation`, and the
`supply_chain_impact` added above were reopened **only** under
`eshu-bootstrap-index`. Under normal ingestion a finding that lost the
cross-scope activation race kept its empty environment fields indefinitely —
the exact defect section 2 describes, unfixed on the path that actually runs in
production. And because the gate drives `eshu-bootstrap-index` for its three
maintenance passes, it went green over that gap: a false green, not evidence
about the ingester.

Saying `supply_chain_impact` "was never replayed" was therefore too narrow. It
was never replayed *by the ingester*, along with the two domains ahead of it in
the chain that had carried the same gap since #5423 and #5710.

**Fix:** the reopen domain list moved to
`postgres.CrossScopeCorrelationReopenDomains`, typed off the `reducer.Domain`
constants, and both runtimes consume it: `bootstrap-index`'s
`correlation_reopen` phase and, new here, the ingester's own
`reopenMaintenanceWorkItemsInTransaction`, in the same transaction as the two
relationship-domain reopens. One list plus one SQL shape is what makes the
gate's `eshu-bootstrap-index` passes evidence about the ingester: only the call
site differs.

The pass's backfill skip-set is deliberately **not** applied to the correlation
reopen. That set records which partitions committed no new backward evidence
this pass; the correlation domains wait on a different signal — another scope's
generation activating — so gating them on it would skip exactly the replay the
activation race needs.

**Per-drain bound.** `RunDeferredRelationshipMaintenance` runs on every shard
drain, not once, and succeeded reducer rows are never terminalized
(`supersedeInactiveReducerGenerationsCTE` sweeps only
pending/retrying/failed/dead_letter), so one row accumulates per (scope,
generation, domain) for the life of the store. An unbounded replay would
resurrect the whole ingestion history into `pending` on every drain. The listing
query carries a per-scope **replay floor**: keep only the work items on the
scope's active generation or newer, falling back to the scope's **latest**
generation when there is no usable active generation.

The fallback covers three shapes, only one of which is the activation race. A
scope that never activated keeps reopening, as it must. A scope whose ACTIVE
generation FAILED has `active_generation_id = NULL`
(`failProjectorWorkQuery`), and a bare `IS NOT NULL` guard reads that as "never
activated" and reopens every one of its generations on every drain forever —
`supersedeInactiveReducerGenerationsCTE` carries the same guard, so nothing
terminalizes them either. A dangling `active_generation_id` (no foreign key
constrains it) behaves identically.

Measured on a 900-scope × 25-generation backlog, Postgres 16, against the
**production** index `(stage, domain, status, visible_at, updated_at DESC)`,
median of three runs:

| shape | listing `EXPLAIN ANALYZE` | rows listed per domain |
| --- | --- | --- |
| unbounded | 29.8 ms | 22 551 |
| superseded exclusion (`IS NOT NULL` guard) | 114.2 ms | 951 |
| replay floor, `MATERIALIZED` | 20.3 ms | 903 |
| replay floor, inlined | 166.0 ms | 903 |

An earlier version of this measurement created a four-column index production
does not have, which is what made the bounded listing look like a regression.

The failed 25-generation scope contributes 25 rows per drain under the guard and
1 under the floor; the never-activated scope contributes 1 under both.

Expected delta versus unbounded, same dataset: `floor ∖ unbounded = 0` (the
bound only removes), `unbounded ∖ floor = 21 648`, no kept row on a superseded
generation, the never-activated scope's row kept (`1`). This is a behavior
change, not an output-preserving rewrite. For the three fact-backed domains the
dropped work is provably unreadable: `facts_active_container_image_identity.go`,
`facts_active_cicd_run_correlation.go`, and
`facts_active_supply_chain_impact.go` all join
`scope.active_generation_id = fact.generation_id`. That argument does **not**
cover `deployable_unit_correlation` or
`kubernetes_correlation_materialization`, which write **graph edges** rather
than those fact rows; for them the floor is justified the other way round —
re-projecting a stale generation spends graph writes anchoring edges to a
generation the read surfaces no longer resolve.

**Real per-drain cost.** The listing is not where this pass spends its time, and
the pre-change ingester baseline was zero: it ran no correlation reopen at all.
Production issues one client round-trip per reopened row. On a corpus of the
same shape and size as the listing proof — 900 scopes x 25 generations, but
without its three extra hole-shape scopes, so 900 and 22 500 rows per domain
here rather than 903 and 22 551 — the
whole five-domain call takes 5.5 s wall — 74 ms of listings, the rest 4500
single-row `UPDATE` round-trips (900 x 5 domains) — against 2 m 17 s and
112 500 round-trips (22 500 x 5)
unbounded. Not measured: the reducer re-executing every reopened item, one per
active scope per domain on every drain indefinitely, two of the five domains
writing graph edges when they run.

Proof scripts, run against a throwaway `postgres:16` container:
`docs/internal/evidence/5426-reopen-bound-proof.sql` (plans, row counts, the
per-scope hole breakdown, and the expected-delta check) and
`TestCorrelationReopenPerDrainCostProof` (`ESHU_CORRELATION_REOPEN_COST_PROOF_DSN`)
for the round-trip cost. `docs/internal/evidence/5426-reopen-update-cost.sql` is
retained for history only; its server-side loop excludes the round-trips.

**Pinned by** (failing-then-green on a live Postgres, `internal/storage/postgres`):

- `TestRunDeferredRelationshipMaintenanceReopensCrossScopeCorrelationDomains` —
  red before the wiring with
  `domain deployable_unit_correlation work item status after ingester maintenance
  = "succeeded", want "pending"`.
- `TestRunDeferredRelationshipMaintenanceSkipsSupersededCorrelationWorkItems` —
  the bound, including the never-activated case.
- `TestRunDeferredRelationshipMaintenanceBoundsScopesWithNoUsableActiveGeneration`
  — the failed-and-nulled and dangling-pointer cases the `IS NOT NULL` guard
  left generation-count-linear.
- `TestCrossScopeCorrelationReopenDomainsCoversDeclaredChain` and
  `TestListSucceededReducerWorkItemsByDomainQueryCarriesReplayFloor`.
- `TestPipelinedBootstrapRunsDeferredBackfillWorkflow` now asserts the bootstrap
  call passes the shared list rather than a hand-copied literal, so the two
  runtimes cannot drift apart again.

**Not done here.** The gate still drives `eshu-bootstrap-index` for its
maintenance passes; it never starts `eshu-ingester`, so it does not exercise
`RunDeferredRelationshipMaintenanceAfterShardDrain` end to end. Adding an
ingester run to `scripts/verify-golden-corpus-gate.sh` needs a collector source
and shard/barrier configuration the gate does not currently have, which is
larger than this follow-up. Sharing the list and the SQL narrows the exposure to
the call site, and
`TestRunDeferredRelationshipMaintenanceIssuesCorrelationListingPerDomain` /
`TestRunDeferredRelationshipMaintenanceCorrelationListingCarriesReplayBound`
now cover that call site hermetically — they drive the real
`RunDeferredRelationshipMaintenance` against the package fakes with no Postgres,
so deleting the `ReopenSucceededReducerWorkItems` call or dropping the replay
floor fails in ordinary CI rather than skipping. Closing the end-to-end gap
still needs the gate change. The durable fix for the
underlying dependency remains #5709's readiness-defer and activation-driven
re-enqueue, which replaces blanket replay with a real dependency gate;
`crossScopeDependencyCatalog` already declares the chain for it.

Replay stays idempotent (each domain upserts its decision on a stable fact key),
and slice order still sequences nothing — convergence comes from maintenance
running more than once.

**No-Observability-Change:** `eshu_dp_correlation_reopened_total` keeps its
name, type, and `domain` attribute; only its emitting call sites widen to
include the ingester.

## Gate runs

On the two-pass gate, at the head reviewed then:

```
$ COMPOSE_PROJECT_NAME=env5426final ESHU_POSTGRES_PORT=15485 NEO4J_BOLT_PORT=7795 \
    NEO4J_HTTP_PORT=7794 GATE_API_PORT=18485 GATE_MCP_PORT=18486 \
    GATE_COLLECTOR_SETTLE_SECONDS=75 bash scripts/verify-golden-corpus-gate.sh
summary: 506 pass, 0 required-fail, 2 advisory-warn
=== PASS: B-7 golden corpus gate green (elapsed 158s, budget ceiling 1800s) ===
```

and the `--keep` run the payload rows above were queried from:

```
$ COMPOSE_PROJECT_NAME=env5426fix4 ESHU_POSTGRES_PORT=15483 NEO4J_BOLT_PORT=7793 \
    NEO4J_HTTP_PORT=7792 GATE_API_PORT=18483 GATE_MCP_PORT=18484 \
    GATE_COLLECTOR_SETTLE_SECONDS=75 bash scripts/verify-golden-corpus-gate.sh --keep
summary: 506 pass, 0 required-fail, 2 advisory-warn
=== PASS: B-7 golden corpus gate green (elapsed 161s, budget ceiling 1800s) ===
```

Both advisory warns are timing, not assertions. `phase_collect` observed 76s
against a 25s ceiling is the deliberate `GATE_COLLECTOR_SETTLE_SECONDS=75`
override (without it the run trips the known collector-settle flake, #5831).
`phase_maintenance_drains` observed 11s against a 10s ceiling is this branch's
known machine-load-sensitive check: it read 9s before either fix and 11s on the
run that carried ONLY the build-provenance fix, with no reopen-list change at
all, so the 2s is not attributable to the extra replayed domain. Earlier runs on
this branch recorded the same check landing on either side of its advisory
ceiling with an identical assertion set.

The run before that one aborted on a `container_image_identity` dead-letter in
the ECR registry scope — `write container image built_from provenance edges:
Neo4jError ... (UNWIND MERGE chain relationship update failed: not found)`,
which is #5767. It is unrelated to either fix here: it fired during the FIRST
drain, before any reopen ran, in a scope whose build-provenance rows this change
does not touch (its only build provenance is the pre-existing OCI-config-label
tier on a different digest). The re-run finished with `dead_letter=0`. Recorded
rather than hidden.

## Correcting the earlier record

An earlier version of this document concluded that "no corpus identity row
carries build-provenance repository attribution for the digests involved" and
that closing the gap was fixture work. The first half was an accurate
measurement; the conclusion drawn from it was wrong. No row carried the
attribution because the producer dropped it on the row it persisted, not because
the cassettes lacked the evidence — the `cicdrun` cassette's `ci.run`/
`ci.artifact` pair had been supplying that evidence since #5423.

The version before that attributed the blocker to #5766 as unlanded upstream
work. That was also wrong: #5766's narrow (`2e1560ff8`) is an ancestor of this
branch.

Both conclusions were reached by reasoning about the corpus rather than querying
it. The measurement that settled it took one `--keep` run and three SQL
statements.


## The gate needed a third maintenance pass, and that was measured

Adding the `environment_evidence` assertion to the corpus made a *required* B-7
assertion depend on how many maintenance cycles the gate runs. The chain is three
links — `container_image_identity` → `ci_cd_run_correlation` → `supply_chain_impact`
— and all three are reopened concurrently in one slice, so the reopen order
sequences nothing.

Two cycles happened to suffice. They suffice with **zero margin**, which is not
the same thing, so I measured the boundary rather than assuming it:

| maintenance passes | `mcp:list_supply_chain_impact_findings` |
| --- | --- |
| 1 | `[FAIL] result item missing required field "environment_evidence"` |
| 2 | `[PASS]` |
| 3 | `[PASS]` |

A required assertion sitting exactly on the convergence boundary reds `main` the
first time drain ordering shifts, and the failure would read as a projection bug
rather than as a gate that was always one scheduling accident away from failing.
The loop now runs three cycles.

The cost is small and measured: the `maintenance_drains` phase was 4s at one
pass, 11s at two, and 14s at three, against a ~158s gate.

```
$ COMPOSE_PROJECT_NAME=fu5426three bash scripts/verify-golden-corpus-gate.sh
[PASS] mcp:list_supply_chain_impact_findings: "findings" has 1 results;
  item fields [cve_id environment_evidence environments impact_status
  repository_id runtime_context subject_digest] present
summary: 506 pass, 1 required-fail, 2 advisory-warn
```

The one required-fail is `mcp:list_aws_runtime_drift_findings` losing its
`drifted_attributes`, which is #5837 — a known flake on a surface this change
does not touch, seen three times now across unrelated runs.

## What is still not asserted

The pin is one-sided by measurement, not by choice. `prod` is the only
environment any ACCEPTED deployment carries in this corpus: the `gitlab_ci` run
has no environment evidence at all, and a `declared` environment could only
reach a finding through the repository-plus-operational-anchor branch, which
needs baked `workload_ids`/`service_ids`/`deployment_ids` that this finding does
not have — they resolve at read time instead (#5835). So there is no
`declared`-tier value to assert alongside `deploy_event` here, and claiming one
would need a second fixture chain rather than a second pin.
