# WorkloadInstance runtime admission — cluster→repo cross-scope fan-out design

Status: **SUPERSEDED for the tier-flip goal by spine #5471/#5608 (now merged).**
The cross-scope fan-out implementation (§2–§5, §8–§9) is **not being built** —
see "Supersession" below. §6 (environment-unbound instance state) and §7
(digest-join cardinality findings) are carried into follow-up issues. This note
remains the mandated "concurrency design artifact lands BEFORE implementation"
acceptance record for #5435 and the prove-theory-first cardinality shim (§7);
it is retained as the durable rationale for why the materialization approach was
not pursued.

## Supersession — why the materialization approach is not built

Spine #5471 (PR #5608, merged as `bcd41455db`) delivered the user-visible goal of
#5435 — "`trace_deployment_chain` stops answering `config_only` when runtime
evidence exists" — through a **read-path** mechanism that is architecturally
superior for a liveness question, and that this materialization design would
conflict with:

- **The runtime tier is decided read-side, not from the graph.**
  `truth.ClassifyDeploymentTruthTier(hasLiveEvidence, hasInstances, …)`
  (`go/internal/truth/deployment_tiers.go`) promotes to `runtime_confirmed` only
  on `hasLiveEvidence`, which is computed by `fetchWorkloadLiveEvidence` →
  `KubernetesPodTemplateStore.HasLiveIdentityMatch` (a bounded, access-scoped,
  ArgoCD-tracking-id-anchored existence read on active `kubernetes_live.pod_template`
  facts). `hasInstances` (the `WorkloadInstance` count this design would raise) is
  **deliberately bucketed with `config_only`** — the tier doc
  (`docs/public/reference/deployment-truth-tiers.md`) maps the legacy
  `materialized_runtime_instances` reason to `config_only` ("config-derived, not
  live observations") and lists config-materialized `WorkloadInstance` rows under
  "DOES NOT QUALIFY" for `runtime_confirmed`. Materializing live pods as
  `WorkloadInstance` rows therefore **cannot** flip the tier — the causal chain in
  §1 is void against the merged contract.

- **Building it would be worse than a no-op — it reopens a just-closed hazard.**
  Where the live probe already matches, pod-derived instances add nothing. Where
  it fails closed (no ArgoCD identity), this design's label×digest deployable-unit
  join would raise confidence `0.45 → ~0.9` under the reason string the tier doc
  calls misleading, while the tier stays `config_only` — laundering runtime
  evidence past the exact identity-binding gate the spine's codex-P1 fix installed.
  That fix closed "a shared image digest alone promotes workload A on workload B's
  live pod" (`TestFetchWorkloadLiveEvidenceDistinctWorkloadsSharedDigest`); the
  deployable-unit digest join here is that same false-positive vector rebuilt one
  layer down, in durable graph write state instead of a per-query probe.

- **Two node labels for one pod is a modeling error.** Live pods already have a
  canonical graph identity: `KubernetesWorkload` + `RUNS_IMAGE` (issue #388),
  keyed on the full live-object tuple including `metadata.uid`. A second
  `WorkloadInstance` projection of the same pods gives them two identities with
  different lifecycles.

Criterion 1 ("a `kubernetes_live` fixture drives `trace_deployment_chain` off
`config_only`") is already proven end-to-end by the spine, not just at unit level:
the B-12 snapshot pins `data.deployment_fact_summary.deployment_truth_tier =
runtime_confirmed` on the HTTP `trace-deployment-chain` shape (the cassette carries
`argocd.argoproj.io/tracking-id` pods), and `impact_trace_deployment_gitops_own_repo_test.go`
exercises the real HTTP handler including the fail-closed negatives. Criterion 3
(this artifact + the §7 shim) is delivered by this commit. Criterion 2
(environment-unbound instance evidence state) is genuinely residual and moves to a
follow-up as a **read-side** field, never a fabricated environment.

Residual work is split into follow-ups (see the closing comment on #5435):
- **Follow-up A** — read-side `live_instance_count` + environment-unbound evidence
  state on `trace_deployment_chain`, computed from the existing pod_template
  Postgres read model / `KubernetesWorkload` graph, with no materialization, no
  cross-scope fan-out, and no new conflict domain. Reuses §6 semantics and §7
  cardinality findings.
- **Follow-up B** — widen the live-evidence probe's identity anchors beyond ArgoCD
  (Helm release annotations, owner-reference chains, declared kind+namespace+name),
  each with the same fail-closed discipline. This is the one place runtime evidence
  still goes unanswered, and the correct fix — not a labels×digest join.

Everything below is retained as the original (pre-spine-merge) design record.
**It reflects the pre-spine tree**: the retained sections' `file:line`
references and code-structure claims (notably §1's `deploymentOverallConfidence`
waterfall — the current function takes `hasLiveEvidence` as a parameter and
checks live evidence first, and the `instance_count = len(instances)` anchor has
shifted) describe main *before* #5608 merged and will not match the current
tree. Do not verify those line numbers against current `main`; read them as
historical. The §7 cardinality claim also carries a codex-review correction
(see §7).

Issue: #5435 (k8s-live: WorkloadInstance join capstone). Part of epic #5430
(kubernetes-live). Depends on merged #5432 (CRI-resolved digest → `RUNS_IMAGE`)
and #5434 (namespace environment binding / `environment.State`).

Owners: reducer / shared-projection owners + kuberneteslive collector owners.

This note is the durable design for admitting `kubernetes_live.pod_template`
facts (cluster-scope runtime evidence) into the repo-scope workload projection
so live pods materialize `WorkloadInstance` nodes, and `trace_deployment_chain`
stops answering `config_only_evidence` when runtime evidence exists. The
central risk is not the graph write — it is the **new cross-scope fan-out**: a
cluster-scope fact arrival must re-trigger a repo-scope materialization that no
cluster-scope intent has ever triggered before. The concurrency rigor here is
copied from the #5007 cross-scope ownership doctrine and the shipped
`DeploymentMapping → ReplayWorkloadMaterialization` replay precedent; it is not
reinvented.

## 1. Problem And Current State

`trace_deployment_chain` decides deployment-evidence origin with a strict
waterfall in `deploymentOverallConfidence`
(`go/internal/query/impact_trace_deployment_resources.go:54-102`):

```
len(instances)          > 0 → "materialized_runtime_instances"
len(deploymentSources)  > 0 → "canonical_deployment_sources"
len(configEnvironments) > 0 → "config_only_evidence"   (confidence 0.45)
else                        → "no_deployment_evidence"
```

`instance_count` is just `len(instances)`
(`impact_trace_deployment_resources.go:28-29`), where `instances` are
materialized `WorkloadInstance` nodes fetched upstream. The node label already
exists and is actively written — `MERGE (i:WorkloadInstance {id: $instance_id})`
(`go/internal/storage/cypher/canonical.go:36`), built by
`BuildCanonicalWorkloadInstanceUpsert`
(`canonical.go:331-345`; params today: `WorkloadID, InstanceID, WorkloadName,
WorkloadKind, Environment, RepoID`).

The gap: `WorkloadInstance` rows are produced **only** by the repo/config
evidence path. `WorkloadMaterializationHandler.Handle`
(`go/internal/reducer/workload_materialization_handler.go:127-209`) loads its
inputs via `CorrelatedWorkloadProjectionInputLoader.LoadWorkloadProjectionInputs`,
which loads exactly two fact kinds
(`correlated_workload_projection_input_loader.go:35-41`):

```go
[]string{factKindRepository, factKindFile}
```

No `kubernetes_live.*` fact is ever consulted. A repo whose only running-proof
is live pods therefore has `len(instances) == 0` and, if it has config
(overlay/manifest) evidence, answers `config_only_evidence` — even though the
cluster is running the workload right now. Flipping that answer requires only
**one** non-empty `instances` entry, so admitting live pods as
`WorkloadInstance` rows is sufficient by itself to change the origin from
`config_only_evidence` to `materialized_runtime_instances`.

## 2. Why This Is A Cross-Scope Fan-Out (the crux)

Scopes in Eshu are the reducer's isolation and conflict boundary. Two facts
matter here and live in **different scopes**:

- `kubernetes_live.pod_template` is **cluster-scoped** — its `ScopeID` is the
  cluster the collector observed, not any git repo.
- `WorkloadInstance` materialization is **repo-scoped** — it runs under
  `DomainWorkloadMaterialization` for a repository scope and MERGEs nodes into
  that repo's platform sub-graph.

Today there is **no** path from a cluster scope's facts into a repo scope's
materialization. Every kubernetes_live intent builder stays inside the
observing cluster scope:

- `projector/kubernetes_correlation_intents.go:20-38` and
  `projector/kubernetes_workload_materialization_intents.go:26-102` emit
  `DomainKubernetesCorrelation`,
  `DomainKubernetesWorkloadMaterialization`, and
  `DomainKubernetesCorrelationMaterialization` intents, all with
  `ScopeID: scopeValue.ScopeID` — the cluster scope. None targets a repo scope.

Repo-scope `DomainWorkloadMaterialization` is triggered today only by a
repo-sync-time `shared_followup` fact (stable key
`"shared_followup:"+repoID+":workload_materialization"`), which emits the
materialization intent with entity key `"workload:"+repoBaseName`
(`go/internal/collector/git_followup_facts.go:150-173`). That is a purely
source/config-driven trigger, once per repository generation.

Admitting live pods therefore introduces a **genuinely net-new cross-scope
re-trigger**: a cluster-scope pod fact must cause a repo-scope materialization
to re-run so it can pick up the new runtime evidence. This is the first
cluster→repo trigger in the system, and it is where every concurrency hazard
lives.

## 3. The Re-Trigger Mechanism — model on the shipped replay precedent

There is exactly one shipped precedent for cross-**domain** re-trigger of
`DomainWorkloadMaterialization`, and the new design copies its safety
properties rather than inventing a mechanism:

`PlatformMaterializationHandler.Handle`
(`go/internal/reducer/platform_materialization.go:117-139`) calls
`WorkloadMaterializationReplayer.ReplayWorkloadMaterialization` only when
`crossRepoWrites > 0` after `DeploymentMapping`'s cross-repo resolution. The
replay itself (`go/internal/storage/postgres/reducer_queue_replay.go:134-174`,
`ReducerQueue.ReplayWorkloadMaterialization`) is:

- **idempotent** — it calls `ReopenSucceeded` first (re-open an already-terminal
  work item in place) and otherwise `enqueueReducerBatch`, whose insert is
  `ON CONFLICT (work_item_id) DO NOTHING` (`reducer_queue.go:32`);
- **bounded** — it enqueues only `workloadMaterializationReplayScopes(intent)`,
  an explicit scope list, never a global backlog scan.

That precedent is repo→repo (same scope family). The #5435 trigger is
cluster→repo, so it must add a **scope-resolution step** the precedent does not
need: given a cluster pod fact, determine the target **repo** scope(s) whose
workload materialization should re-run. That resolution is the digest-identity
join (§7): a pod's resolved image digest → the `reducer_container_image_identity`
that owns it → its `source_repository_ids`.

Design of the new trigger (`KubernetesLiveWorkloadReplayer`, cluster-scope
handler side):

1. On a cluster-scope kubernetes_live correlation/materialization pass, collect
   the distinct set of **target repo scopes** implied by the observed pods'
   resolved image identities (see §7 for how ambiguity is bounded).
2. For each distinct target repo scope, enqueue **one**
   `DomainWorkloadMaterialization` replay intent via the existing
   `ReopenSucceeded`/`enqueueReducerBatch` path — reused verbatim, not a new
   queue.
3. The repo-scope `WorkloadMaterializationHandler` re-runs. Its input loader is
   extended (§5) to additionally load the repo's admitted live-pod evidence and
   emit `WorkloadInstance` rows for it.

No cluster-scope handler ever writes a repo-scope graph node directly. The
cluster side only **enqueues a repo-scope intent**; the repo-scope handler
remains the sole writer of that repo's `WorkloadInstance` nodes. Ownership stays
single-writer per node, exactly as #5007 requires.

## 4. Conflict Domain And Key — reuse, do not add

The claim-time conflict key for workload materialization is already defined and
is deliberately **shared** across three domains
(`go/internal/storage/postgres/reducer_queue_conflict.go:180-197`):

```go
case reducer.DomainWorkloadMaterialization,
     reducer.DomainPlatformInfraMaterialization,
     reducer.DomainDeploymentMapping:
    return reducerConflictDomainPlatformGraph,
           reducerPlatformNodeWriterConflictKey(scopeKey)
```

All three MERGE the same `:Platform{id}` namespace for a scope and hold no
separate advisory lock, so they must not run concurrently for the same scope —
the shared key serializes them.

**Decision: the cluster→repo re-trigger introduces NO new conflict domain.** The
replay intent it enqueues is an ordinary `DomainWorkloadMaterialization` intent
for the **target repo scope**, so it hashes to the *same*
`reducerPlatformNodeWriterConflictKey(repoScopeKey)` as the repo's own
config-driven materialization. Consequences:

- A cluster-triggered replay and the repo's own config materialization for the
  same repo **cannot run concurrently** — they serialize on the existing fence.
  There is no new double-writer to `WorkloadInstance` / `:Platform{id}`.
- The cluster-scope kubernetes_live passes keep their own cluster-scope conflict
  keys (unchanged); only the *enqueued* repo intent carries the repo key.

This is the #5007 rule applied verbatim: *partition by conflict key, do not
reduce worker count*. Unrelated repos' materializations still run fully
concurrently; only same-repo overlap is serialized, and it already was.

## 5. Input-Loader Extension — repo-scope read of admitted live evidence

`CorrelatedWorkloadProjectionInputLoader.LoadWorkloadProjectionInputs`
(`correlated_workload_projection_input_loader.go:35-68`) gains a third input
source **read within the repo scope**: the live-pod evidence already attributed
to this repo. Critically, the loader does **not** reach into a cluster scope —
the attribution (pod→repo) is done at trigger time (§3/§7) and persisted as a
repo-scoped admitted-instance fact, so the repo-scope loader reads only
repo-scope facts. This keeps the loader's scope-purity invariant intact and
avoids a cross-scope read inside the hot materialization path.

Each admitted live pod becomes one `WorkloadInstance` row. The instance id is
keyed on the pod's own stable identity (cluster uid / object id), so:

- **N replicas → N instances** (correct: `instance_count` should reflect running
  replica count), not N² and not collapsed to one.
- Re-materialization is idempotent — same pod uid MERGEs the same node.

## 6. Environment-Unbound Instance State — no invented environment

Acceptance requires environment-unbound instances to appear as their **own
evidence state**, never with a fabricated environment. This mirrors the shipped
namespace precedent exactly
(`go/internal/reducer/kubernetes_namespace_materialization.go:39-55, 272-289`),
which gates every environment binding through `environment.Normalize` →
`environment.IsKnownToken` → `environment.Canonical` and defaults to
`StateEnvironmentUnbound` (`go/internal/environment/environment.go:147-156`)
without ever creating an `:Environment` node for an unknown value.

`BuildCanonicalWorkloadInstanceUpsert` (`canonical.go:331-345`) is extended to
carry two new node properties, sourced from the pod's labels via the same
`IsKnownToken`-gated lookup:

- `environment_state` — `bound` | `environment-unbound`;
- `evidence_class` — set to the namespace/pod-label evidence class only when
  bound, empty otherwise.

An environment-unbound live instance is still a **real, materialized
`WorkloadInstance`** (so it flips `trace_deployment_chain` to runtime origin),
but it carries no `TARGETS_ENVIRONMENT` edge and no fabricated environment
value. The graph never invents an `:Environment` node from a label it does not
recognize.

## 7. Digest-Join Cardinality — prove-theory-first shim (recorded before build)

The join key is *pod labels × image-identity (digest/repo) correlation*. Before
writing any join, the fan-out was measured to prove it cannot explode.

**Live substrate (QA graph, 2026-07-22, eshu `v0.0.3-pre-release-17`):**
unavailable for a live measurement — the QA graph runs no kuberneteslive/CRI
collector (active collectors: aws, package_registry, terraform_state). Direct
counts: `KubernetesWorkload` nodes = **0**; `reducer_container_image_identity`
facts (both `exact_digest` and `tag_resolved`) = **0**; `WorkloadInstance`
baseline = **294** (all config/deployment-source origin — the "before" number
that admitting live pods will add to). Recorded honestly: live data cannot
prove the join here, so the deterministic golden corpus is the substrate.

**Golden-corpus substrate (`testdata/`, the #5432-seeded reproducible set):**

- `kubernetes_live.pod_template` facts in `supply-chain-demo` cassette: **5**.
- Resulting `RUNS_IMAGE` edges (KubernetesWorkload→OciImageManifest): **3** —
  digest-pinned Deployment + ReplicaSet + one tag-referenced Pod promoting via
  its CRI-resolved digest (the `+1` from #5432). Each pod resolves to **≤1**
  image manifest.

**The three join axes and their bounds:**

1. **digest → image manifest**: content-addressed, but **not proven 1:1** — see
   correction below. A sha256 digest identifies image *content*, but the same
   digest can appear on multiple OCI records (mirrored or republished across
   repositories). The join (`BuildSourceImageDigestJoinIndex`,
   `kubernetes_workload_source_image_join.go:91`) stores only one node per
   digest by overwriting `byDigest[digest]`, so the corpus's 5 pods → 3 matches
   does **not** establish a no-multiplication bound when several OCI records
   carry one digest — it only reflects that the corpus happens to have no
   duplicate-digest records. **Correction (codex review, PR #5640):** before
   this axis can be called proven, the retained-for-reuse shim must actually
   count duplicate-digest OCI records and define an explicit ambiguous /
   fail-closed policy for them (a shared digest must never silently pick one
   repo). That measurement + policy is carried into follow-up #5638, which owns
   the read-side count; it was **not** proven here.
2. **pod → WorkloadInstance**: 1:1 on pod uid (§5). N replicas → N instances,
   which is the intended `instance_count`, not a fan-out defect.
3. **image → `source_repository_ids`**: the **only** multi-valued axis. A shared
   base image can list M candidate source repos
   (`query/container_image_identities.go`, `source_repository_ids[]` is
   overlap-attributed, not 1:1). Left naive, one pod could fan into M repo-scope
   instances.

**Bounding rule (the theory being proven):** a live pod is attributed to a repo
scope, and re-triggers that repo's materialization, **only when its image
identity resolves to exactly one source repository** (an `exact_digest`
identity with a single `source_repository_ids` entry). When the identity is
ambiguous (multi-repo) or unresolved, the pod is admitted as an
**environment-unbound, repo-unattributed** instance — it is *not* fanned out
into M repo-scope instances and does *not* re-trigger M repo materializations.
This mirrors the `selector_match`/#5434 "ambiguous ⇒ provenance-only, not
promoted, invent nothing" rule.

**Cardinality conclusion:** total admitted instances ≤ observed live pods
(bounded by axis 1 and 2 being 1:1 and axis 3 being collapsed-not-multiplied).
For the corpus: ≤5 new runtime instances over the 294 baseline. There is no
pod×repo or pod×image cross-product. Theory proven before implementation.

## 8. Poll-Storm / Debounce Control

A Deployment with many replicas emits many `pod_template` facts in one
generation, all resolving to the **same** repo. Without control this is N
re-triggers of one repo's materialization.

Control (two existing mechanisms, no new machinery):

- **Distinct-scope coalescing at enqueue** — the trigger (§3) enqueues one
  replay intent per *distinct target repo scope*, computed after the §7
  attribution, not one per pod.
- **Intent-identity + generation fence** — the enqueued intent reuses the
  workload-materialization entity key (`"workload:"+repoBaseName`) under the
  current generation, and the queue insert is `ON CONFLICT (work_item_id) DO
  NOTHING` (`reducer_queue.go:32`) with `ReopenSucceeded` idempotency. N pod
  facts for the same repo in the same generation therefore collapse to **one**
  materialization run, not N.

Result: re-trigger volume is bounded by *distinct (repo scope × generation)*,
not by pod count — the same bound the shipped `DeploymentMapping` replay relies
on.

## 9. Transaction / Retry Boundaries And Hazard Table

- **Transaction scope**: the repo-scope `WorkloadInstance` MERGE batch runs in
  the existing managed graph write for `DomainWorkloadMaterialization` (one
  managed txn per materialization run). The cluster-side trigger's enqueue is a
  separate Postgres transaction from any graph write — enqueue and materialize
  never share a transaction, so a slow graph write never holds the queue.
- **Retry scope**: a failed materialization retries the whole repo-scope run;
  because every write is a MERGE keyed on pod uid / instance id and the enqueue
  is `ON CONFLICT DO NOTHING`, replay of an already-succeeded run converges to
  the same graph (idempotent).

| Hazard | Disposition |
| --- | --- |
| Deadlock (two writers, same `:Platform{id}`) | Removed by design — cluster-triggered and config-triggered runs share the existing `reducerPlatformNodeWriterConflictKey(repoScope)` fence (§4); they serialize, never interleave. |
| Race (stale runtime evidence overwrites fresh) | Generation fence — a replay intent carries the current generation; older-generation admitted evidence is filtered by the loader, same as config facts. |
| Starvation (pod storms crowd out config work) | Coalescing (§8) caps re-triggers at distinct (repo×generation); unrelated repos keep full concurrency (§4). |
| Duplication (same pod → many instances) | Instance id keyed on pod uid (§5); MERGE idempotent. |
| Stale-work replay (dead-letter / partial projection) | Reuses `ReopenSucceeded` + `ON CONFLICT DO NOTHING` (`reducer_queue_replay.go:134-174`) — the shipped, tested replay path. |
| Cross-scope read in hot path | Avoided — attribution happens at trigger time and is persisted repo-scoped; the repo loader reads only repo-scope facts (§5). |

## 10. Observability

New signals (per concurrency-rigor Observability requirement), all
`eshu_dp_*`:

- counter `..._workload_runtime_instances_admitted_total{outcome}` — outcome ∈
  `bound` | `environment_unbound` | `skipped_ambiguous_identity`
  (skipped = axis-3 multi-repo collapse; proves the fan-out bound is holding).
- counter `..._workload_runtime_retrigger_total{result}` — result ∈ `enqueued` |
  `coalesced` (proves §8 debounce is coalescing pod storms).
- span event on the cluster→repo trigger carrying distinct-target-repo count and
  observed-pod count (the fan-out ratio an operator reads at 3am).
- structured log at admission with pod uid, resolved digest, attributed repo (or
  `unattributed`), environment_state.

These answer: which conflict domain is hot (repo scope on the retrigger
counter), whether the fan-out bound holds (`skipped_ambiguous_identity` > 0 with
no instance explosion), and whether pod storms coalesced.

## 11. Spine Dependency And Sequencing

*Editorial note: this section was written before #5608 merged (as
`bcd41455db`); the spine is now on `main`. Its "currently unmerged" language and
"deferred until #5608 merges" sequencing describe a state that has already
passed — see the Supersession section at the top for the current disposition.*

The typed deployment-truth-tier vocabulary does **not** exist on `origin/main`
today (`rg 'truth_tier|TruthTier|deployment_truth'` → 0 matches); it is added by
spine #5471 / PR #5608 (branch `5471-deployment-truth-tiers`, currently
unmerged). #5435's *acceptance* does not require that typed vocabulary — flipping
`config_only_evidence` → `materialized_runtime_instances` rides the existing
string-literal origin scheme (§1). **However**, #5608 edits the exact origin
decision in `impact_trace_deployment_resources.go`. Implementing #5435's write
path against that same function before #5608 lands would fork the file and force
a conflict resolution neither PR owner can do cleanly.

**Sequencing decision:** this design artifact and the §7 cardinality shim land
now (satisfying the "artifact + shim before implementation" acceptance gates).
The implementation (loader extension, trigger, node-property additions,
regression fixture) is deferred until #5608 merges, then built on the settled
origin/tier surface. The regression test the issue requires — a `kubernetes_live`
fixture driving `instance_count > 0` on `trace_deployment_chain`, with
environment-unbound instances as their own state — is authored in that
implementation PR.

## 12. Verification Plan (for the implementation PR)

- Reducer unit test: an admitted live-pod fact → one `WorkloadInstance` MERGE
  with `environment_state=environment-unbound` when labels carry no known
  environment token; `bound` + `evidence_class` when they do.
- Cross-scope trigger test covering the Replay/Retry matrix: duplicate delivery
  of an already-succeeded materialization, stale-generation replay, pod-storm
  coalescing (N pods, same repo → 1 enqueue), ambiguous multi-repo identity →
  `skipped_ambiguous_identity` and no repo re-trigger, empty/no-pod scope.
- Query regression: a `kubernetes_live` fixture makes `trace_deployment_chain`
  report `instance_count > 0` and origin `materialized_runtime_instances` where
  the same repo previously answered `config_only_evidence`.
- Golden corpus: extend the `supply-chain-demo` cassette + B-12 snapshot to
  assert the new runtime `WorkloadInstance`(s) and the origin flip
  (non-vacuous, `minimum_results >= 1`).
- `scripts/verify-performance-evidence.sh` +
  `scripts/test-verify-performance-evidence.sh` with a
  `No-Regression Evidence:` + observability note naming the shared conflict
  domain and the coalescing counter.

## 13. Consequences And Remaining Risk

- Positive: live pods make deployment truth runtime-backed instead of
  config-only, with no new conflict domain, single-writer ownership preserved,
  and a proven-bounded fan-out.
- Cost: one extra repo-scope input read per materialization and a bounded
  cluster→repo enqueue per distinct target repo per generation.
- Remaining risk (stated plainly): the pod→repo attribution accuracy depends on
  the `reducer_container_image_identity` `source_repository_ids` quality; a
  wrongly-single-attributed identity would attribute a pod to the wrong repo.
  Mitigation is the §7 rule (attribute only on unambiguous exact-digest single
  source) plus the `skipped_ambiguous_identity` telemetry to make the
  conservative path observable. Sharpening ambiguous attribution is explicitly
  out of scope for #5435 and left to the identity-resolution track.
