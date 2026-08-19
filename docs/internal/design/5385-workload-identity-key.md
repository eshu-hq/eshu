# Workload And WorkloadInstance Identity Keys

Status: design for issue #5385, for owner sign-off. No implementation. The
measurements come from read-only probes and throwaway test files that are not
part of this branch.

Owners: reducer, correlation, and graph maintainers.

## 1. The key, exactly as built

Two lines in `go/internal/reducer/projection.go` (a third, in
`projection_helpers.go`, rebuilds the instance id independently — see section 4):

```go
workloadID := fmt.Sprintf("workload:%s", workloadName)                       // :279
instanceID := fmt.Sprintf("workload-instance:%s:%s", workloadName, environment) // :327
```

Neither embeds a repository, namespace, or cluster. `WorkloadCandidate` — the
struct being read three lines above — carries `RepoID`, `RepoName`,
`DeploymentRepoIDs`, and `Namespaces`. None reach the identifier.

Both keys are then declared globally unique
(`go/internal/graph/schema_tables.go:122-123`):

```cypher
CREATE CONSTRAINT workload_id IF NOT EXISTS FOR (w:Workload) REQUIRE w.id IS UNIQUE
CREATE CONSTRAINT workload_instance_id IF NOT EXISTS FOR (i:WorkloadInstance) REQUIRE i.id IS UNIQUE
```

By contrast, API endpoints in the same package already key on the repository —
`stableAPIEndpointID(repoID, workloadID, path)`
(`projection_helpers.go:58`). The repo-scoping instinct is present in this code;
it just was not applied to the workload itself.

## 2. What a cross-repo collision actually does

Two repositories with a workload of the same name become **one node carrying
both repositories' edges, with ownership silently reassigned to whichever
materialized last.**

### Measured, through the production dispatch shape

The reducer materializes one `Intent` per call
(`workload_materialization_handler.go:216`), and that intent's candidates are
loaded for a single `(scope_id, generation_id)` pair
(`correlated_workload_projection_input_loader.go:35-42`).

**One call is one scope-generation, not one repository.** An earlier revision of
this document said "two unrelated repositories therefore arrive as two separate
calls, not one" and treated that as structural. It is not — it is current
git-sync behaviour, and the distinction produces a second failure mode described
below. The loader filters on `(scope_id, generation_id)` and on nothing else; no
repository predicate exists anywhere on that path. The sibling helper
`scopeRepositoryGraphIDs` (`workload_materialization_repo_phase.go:202-232`)
returns a **deduplicated, sorted set** of repository graph ids drawn from the
same scope-generation, and that `seen` map exists because more than one
repository per scope is representable. So does the ingestion side: the commit
path explicitly accumulates several repository ids under one
`(ScopeID, GenerationID)` key
(`ingestion_backfill.go:294-338`), and a closed incident records why —
"the ingestion commit path accepts multi-repo scopes; production git sync just
happens to commit one repo per scope, so this had not fired there"
(`docs/internal/evidence/deferred-backfill-shared-partition-dupkey.md:11-13`).
That assumption has already misfired once in this pipeline, with a live Ifá gate
run taking a `SQLSTATE 21000` rollback for it.

The consequence is that **there are two collision modes, not one**, and which one
fires depends on whether the colliding repositories land in the same
scope-generation.

Driving the real `WorkloadMaterializer` against a live NornicDB that way — one
call per repository, `alpha` deploying to production and `beta` to staging, both
with a workload named `checkout`:

```
P5385 call repo=probe5385-alpha  WorkloadRows=1  InstanceRows=1
P5385 call repo=probe5385-beta   WorkloadRows=1  InstanceRows=1
P5385 workload node count -> 1
P5385 workload repo_id    -> probe5385-beta
P5385 DEFINES edge count  -> 2
P5385 definers            -> [probe5385-alpha, probe5385-beta]
P5385 instances           -> workload-instance:checkout:production (repo_id=probe5385-alpha)
                             workload-instance:checkout:staging    (repo_id=probe5385-beta)
```

Read as an operator would:

- **One `Workload` node, two owners.** `MERGE (w:Workload {id: row.workload_id})
  SET w.repo_id = row.repo_id` (`canonical.go:45-54`, batch form at
  `workload_materializer.go:351-357`) unconditionally overwrites `repo_id`, so
  alpha's workload is now attributed to beta. Nothing records that alpha ever
  owned it.
- **Both `DEFINES` edges persist**, because `MERGE (repo)-[rel:DEFINES]->(w)`
  adds each repository's own edge without touching the other's.
- **Instances from both repositories hang off the shared node.** Ask what
  environments alpha's `checkout` runs in and the graph answers production *and*
  staging. Staging is beta's; alpha may have no staging deployment at all.

There is no retraction counterpart for the `Workload` node or its `DEFINES`
edge. `WorkloadInstance` has one (`ReconcileWorkloadInstanceRetraction`, tested
never to cross repo scope at `workload_instance_retraction_test.go:91`); the
workload itself does not.

### Mode two: same scope-generation, and the row is dropped rather than merged

If two colliding repositories' facts sit in one scope-generation, they arrive in
**one** candidate slice, and the merge above never happens. `seenWorkloads`
(`projection.go:289`) takes the first candidate and **discards the second
repository's `WorkloadRow` entirely** — so the surviving node's kind,
classification, confidence and provenance are the first candidate's, and the
second repository's are gone. `seenInstances` (`:328`) does the same to any
instance sharing a name and environment.

**No `DEFINES` edge is written for the losing repository.** The edge batch is
built exclusively from `WorkloadRows` (`workload_materializer.go:124-143`), so a
dropped row means a dropped edge. `RepoDescriptors` *is* appended unconditionally
one block above the dedup check (`:283-287`), but it never reaches the edge
writer — it feeds only `ReconcileWorkloadInstanceRetraction` and
`ReconcileWorkloadDependencyEdges` (`workload_materialization_handler.go:274,307`).
That path is safe here, and deliberately so: it carries an explicit
positive-evidence guard, and a repository with a descriptor but zero instance
rows this pass has its existing instances **left untouched** rather than treated
as superseded (`workload_instance_retraction.go:58-73`). Someone already thought
about this shape.

Whether mode two leaves any graph trace at all depends on the environments:

- **Same name, same environment.** Both the workload row and the instance row are
  dropped. Nothing is written for the losing repository, and the surviving node
  looks like an ordinary single-definer workload. **No graph evidence exists.**
- **Same name, different environments.** The workload row is still dropped, but
  the instance row is written — with `RepoID` set to the *losing* repository
  (`projection.go:331-334`) and `WorkloadID` set to the shared name-only id. So
  the graph gains instances owned by a repository that has no `DEFINES` edge to
  the workload they hang off. **That is detectable**, and it is the same
  cross-repo environment leak mode one produces, arrived at by a different route.

This is the finding an earlier revision of this document raised and then
withdrew. The withdrawal was half right and I over-corrected: the probe that
produced it was invalid, because it called
`BuildProjectionRowsWithInfrastructurePlatforms` directly with both repositories'
candidates in one slice, which is not how git sync reaches it today. But the
shape that probe simulated is reachable through the production path — a
multi-repo scope produces exactly that slice — so the correct disposition is
"real, currently unexercised by git sync", not "not a production path". Both
modes are recorded here rather than one replacing the other.

Neither mode errors, and neither increments anything.

### Why the dedup does not save either mode

`projection.go:289` dedups on the name-only id before anything is written. That
is the map's actual job, and within one candidate slice it does it. But the id it
dedups on carries no repository, so it cannot tell "the same workload seen twice"
from "two repositories' different workloads that share a name" — it treats both
as the second one being redundant. That is mode two.

Across calls it does not run at all: the map is created per call
(`projection.go:253`), so each scope-generation starts with an empty one and the
graph-side `MERGE` decides. That is mode one.

An earlier draft tested only the first shape and reported a drop; the revision
after it tested only the second and reported a merge, calling the drop finding
withdrawn. Both were partial. The mechanism is one name-only key reached by two
dispatch shapes, and it fails differently in each.

### The reset path: I called this live, and it is not

An earlier revision of this document called `go/internal/graph/mutations.go:113-129`
a live data-loss path. **That was wrong**, and the correction matters more than the
original claim did.

```
rg -n --type go 'ResetRepositorySubtreeInGraph' . | rg -v 'internal/graph/mutations'
```

returns nothing. All three functions in that file have **zero callers** outside their
own file and test. It is a port of `graph/persistence/mutations.py`, which no longer
exists here. No CLI subcommand, admin route, or ingester path reaches it. What
production does on re-ingest is `canonicalNodeRepositoryIDCleanupCypher` —
`MATCH (r:Repository {id: $repo_id}) DETACH DELETE r` — which removes the Repository
node and its incident edges only; a shared `Workload` survives that and merely loses one
`DEFINES` edge, which the reducer re-MERGEs.

The proposed one-clause fix would not have worked either. The delete set has four
collections, and collection 3 is `OPTIONAL MATCH (owned_workload:Workload {repo_id: r.id})`.
Since `w.repo_id` is overwritten by whichever repository materializes last, resetting the
*last writer* deletes the shared node through collection 3 regardless of what the `DEFINES`
clause says.

**There is a real cross-repo leak, and it is elsewhere.** `retractRepoRunsOnEdgesCypher`
and `retractSingleRepoRunsOnEdgesCypher` (`canonical_relationships.go:352-364`) are
dispatched in production from `edge_writer_retract_repo.go:112,114`:

```cypher
MATCH (repo:Repository {id: repo_id})-[:DEFINES]->(w:Workload)
MATCH (i:WorkloadInstance)-[:INSTANCE_OF]->(w)
MATCH (i)-[rel:RUNS_ON]->(:Platform)
WHERE rel.evidence_source = $evidence_source
DELETE rel
```

Same unfiltered `DEFINES` traversal, and nothing scopes `i` to the retracting
repository. The matching upsert (`canonicalRunsOnUpsertCypher`, `:322-330`) has the
identical two-hop shape, and there `evidence_source` is only *set*, never filtered —
so nothing scopes the write side either.

**Measured on a live NornicDB at the pinned revision**, driving the production
dispatch (`EdgeWriter.RetractEdges` → `edge_writer_retract_repo.go`) for both the
single-repo and UNWIND retract shapes. Identical across 6/6 runs:

```
precondition:      workload_nodes=1 defines=2 instances=2
after beta pass:   beta_inst->beta_plat=1   alpha_inst->beta_plat=1
after alpha pass:  beta_inst->beta_plat=0
                   beta_inst->alpha_plat=1
                   alpha_inst->alpha_plat=1
                   gamma_inst->gamma_plat=1   (non-colliding control, untouched)
```

Three things this shows that the retract-side framing above does not:

- **The contamination is two-sided and does not need the retract at all.** Beta's
  plain *write* already attached beta's platform to alpha's instance, before any
  retract ran.
- **The end state asserts something false**, rather than merely losing an edge:
  beta's instance ends up claiming it runs on alpha's platform.
- **The blast radius is bounded** to the `resolver/cross-repo` evidence source.
  The materializer's own `reducer/workloads` RUNS_ON edges survived every run,
  and the non-colliding control was never touched.

What this does *not* show, stated plainly: that a collision exists in production.
Section 3.1's detectors read zero on the largest corpus available, with the
coverage limits recorded there. This proves the mechanism fires whenever one
does.

### The authorization layer is already paying for this

This is the strongest evidence that the defect is real and current, and it was in the tree
the whole time. `relationships_catalog_cypher.go:437-456` documents the exact mechanism —
"two repositories that define same-named workloads collapse to a single Workload node with
last-writer-wins repo_id" — and then deliberately **under-authorizes** to contain it:

> admitting a collision Workload via DEFINES would expose every edge attached to it,
> including edges a DIFFERENT tenant's ingestion wrote, purely because the two tenants'
> workloads share a name … under-authorization is the fail-closed, acceptable outcome
> here, never a leak.

`infra_scope_grant.go:247-251` adds disjunct 5 for the same reason: "a name-collision
Workload defined by two repositories materializes only ONE repo_id, so a grant for its
OTHER defining repository is missed by the flat compare."

So the cost of this defect is already being paid, in two places, as deliberate
under-authorization and an extra grant disjunct. A repo-scoped key makes `w.repo_id`
exact and retires both.

## 3. Measured blast radius

### 3.1 Corpus scale

Read-only queries against a live NornicDB holding a 908-repository graph
(137,420 `File`, 512,492 `Function`, 36,242 `TerraformResource` nodes — a
substantive run, not a stub):

| Measure | Value |
| --- | ---: |
| Repositories | 908 |
| `Workload` nodes | 40 |
| `WorkloadInstance` nodes | 33 |
| **Workloads with more than one defining repository** | **0** |
| Workloads whose instances span more than one `repo_id` | 0 |
| Instance/workload pairs where `i.repo_id = w.repo_id` | 33 of 33 |

Section 2 establishes that a **mode-one** collision produces exactly two
`DEFINES` edges on one node, so the highlighted row is a valid, non-vacuous
detector for that mode — and it is zero. The instance-side row is an independent
detector, and it covers mode one plus mode two's differing-environment sub-case;
it is zero as well. Neither covers mode two where the environments also match.

Every count was re-derived from an unfiltered `GROUP BY` distribution rather
than a `WHERE`-filtered aggregate, because a filtered predicate silently lied
during this probe (`coalesce(a.x,'lit') <> coalesce(b.x,'lit')` returns zero
rows even when the values differ, reproduced 3/3 on NornicDB 1.2.1 and 1.2.2). A
false finding was nearly published from it. The distributions used were:

```cypher
MATCH (r:Repository)-[:DEFINES]->(w:Workload)
WITH w, count(DISTINCT r) AS repos RETURN repos, count(w) ORDER BY repos
-- -> repos=1, workloads=40   (no row with repos>1)

MATCH (i:WorkloadInstance)-[:INSTANCE_OF]->(w:Workload)
RETURN (i.repo_id = w.repo_id) AS same_repo, count(*) ORDER BY same_repo
-- -> same_repo=true, pairs=33
```

**No collision is firing on the largest corpus available.** The mechanism is
real and destructive; the trigger is currently absent.

**The two detectors do not cover the same ground, and it is worth being precise
about which covers what.**

The `DEFINES` detector — the highlighted row — sees **mode one only**. A merged
node keeps both edges, so it is visible; a mode-two drop writes no edge for the
losing repository at all, and what survives looks like an ordinary single-definer
workload.

The **instance-side detector is better than I first credited it.** Under mode two
with differing environments, the losing repository's instance rows *are* written,
carrying its own `repo_id` while attached to the shared workload — so
"workloads whose instances span more than one `repo_id`" is a genuine mode-two
detector for that sub-case, and it measured zero too.

What neither detector can see is mode two where the environments also match: both
rows are dropped, and no trace remains. For that sub-case this table cannot
distinguish "no collision occurred" from "a collision occurred and one
repository's evidence was silently discarded."

And the whole corpus was ingested through git sync, which commits one repository
per scope, so it never exercised the multi-repo scope path that produces mode two
in the first place. The honest reading is **"zero cross-scope merges, and zero
cross-repo instance spans, under single-repo-per-scope ingestion"** — not "zero
collisions".

None of this changes the recommendation. It makes the case for the `seenWorkloads`
counter in migration item 7, which is the only thing that would see a same-name
same-environment drop.

### 3.2 Why the population is so small

40 workloads from 908 repositories is not an accident: admission requires
`confidence >= 0.82` (`workloadMaterializationMinConfidence`, `projection.go:25`)
plus a materializable classification. The name space is small, so names have
little opportunity to collide.

This is the crux of the timing decision, and it cuts both ways:

- **Against acting now:** zero observed cross-scope merges and zero cross-repo
instance spans, on the largest corpus there is — but see 3.1 for what that zero
does and does not cover. It was measured under single-repo-per-scope ingestion,
which never exercises mode two, and one mode-two sub-case leaves no trace for any
detector to find.
- **For acting now:** 40 nodes and 33 instances is the cheapest this migration
  will ever be, and the admission gate is expected to widen, not narrow.

### 3.3 Golden corpus

| Artifact | `workload:` literals | `workload-instance:` literals |
| --- | ---: | ---: |
| `testdata/golden/e2e-20repo-snapshot.json` | 33 | 12 |
| `testdata/cassettes/` | 1 (in 1 file) | 0 |

The B-12 snapshot's own node counts are floor/ceiling ranges rather than
per-repository lists, so it cannot independently answer the collision question;
the 45 literals above are the regeneration surface, not a second measurement.

## 4. Edge families and identifier sites a re-key must carry

Relationships anchored on a `workload:`/`workload-instance:` identifier:

| Edge | Anchor | Written at |
| --- | --- | --- |
| `(Repository)-[:DEFINES]->(Workload)` | `workload_id` | `workload_materializer.go:364`, `canonical.go:52` |
| `(WorkloadInstance)-[:INSTANCE_OF]->(Workload)` | both | `workload_materializer.go:385`, `canonical.go:66` |
| `(WorkloadInstance)-[:RUNS_ON]->(Platform)` | `instance_id` | `workload_materializer.go:413`, `canonical.go:82`; id built independently at `projection_helpers.go:110` |
| `(WorkloadInstance)-[:DEPLOYMENT_SOURCE]->(Repository)` | `instance_id` | `workload_materializer.go:393`, `canonical.go:89` |
| `(Workload)-[:DEPENDS_ON]->(Workload)` | `workload_id`, `target_workload_id` | `workload_materializer.go:429`, `canonical.go:114-116, 174-176` |
| `(Workload)<-[:INSTANCE_OF]-(WorkloadInstance)-[:USES]->(CloudResource)` | `workload_id` | `workload_cloud_relationship_writer.go:21` |
| `(Function)-[:RUNS_IN]->(Workload)` | resolved via `(repo)-[:DEFINES]->(w)` | `canonical_runs_in_edges.go:24-31` |
| documentation edge onto `Workload` | `target_entity_id` | `canonical_documentation_edges.go:35` |
| `Endpoint` | `stableAPIEndpointID(repoID, workloadID, path)` | `projection_helpers.go:58` |

**A second, non-graph identity subsystem also uses this prefix, and a graph-only
re-key would leave it inconsistent.** `go/internal/collector/git_followup_facts.go:52,188`
emits a `reducer_workload_identity` Postgres fact whose `entity_key` is
`"workload:" + filepath.Base(repoPath)` — a *third* independent construction,
keyed on repo basename. It is read back by `repository_read_model_summary.go:114`
and `content_reader_repository_catalog.go:107`, and surfaced through
`entity_workload_context.go` as the `materialization_status: "identity_only"`
fallback (`:212`, `:269`) used precisely when a workload has no materialized
graph node. The supply-chain impact domain also treats any `workload:`-prefixed
`entity_key` as workload-identity evidence.

**A third construction of the instance id lives outside `projection.go`.**
`projection_helpers.go:110` builds `fmt.Sprintf("workload-instance:%s:%s", workloadName, environment)`
again, for `RuntimePlatformRow.InstanceID`, which feeds
`MATCH (i:WorkloadInstance {id: row.instance_id})` in the `RUNS_ON` upsert
(`workload_materializer.go:410-411`). A re-key that updates `projection.go:327`
and the node id but misses this line leaves the `MATCH` looking for the old
name-only format: it finds nothing, and `RUNS_ON` silently stops being written
with no error. This is the precise failure class this inventory exists to
prevent, and it was missed on the first two passes over this section.

Two further construction sites inside the reducer:

- `dependency.go:76` — `targetWorkloadID := fmt.Sprintf("workload:%s", depName)`,
  a second name-only reconstruction for the `DEPENDS_ON` target.
- `dependency_domain.go:101,116` — `partitionKey = fmt.Sprintf("workload:%s->%s", ...)`,
  the reducer conflict domain for dependency edges. A re-key changes the shape of
  a concurrency partition key, which is a concurrency question, not a cosmetic one.

Parse sites in the read path: `catalog.go:213-214,332`,
`impact_change_surface_resolvers.go:102-108`, `entity_workload_context.go:261,280,286`,
`repository_read_model_summary.go:114`, `content_reader_repository_catalog.go:107`,
`service_workload_resolution.go:137`, `supply_chain_impact_path.go:145`,
`supply_chain_impact_match.go:147`, `mcp/dispatch_args.go:61-66`.
`entity_map_resolver.go:188` resolves by `{repo_id, name}` and would keep working.

Nothing parses a `workload-instance:<name>:<env>` identifier back apart — a
verified negative, not an unsearched gap.

This inventory is larger than it first appeared and should still be treated as a
starting point. **The `reducer_workload_identity` subsystem in particular needs a
decision before implementation** (section 8, question 3), because it is the
documented fallback for exactly the scenario in section 2 and is keyed on repo
basename rather than the candidate's workload name.

## 5. Options

Judged against the repo's rule that namespace, folder, or repo-name heuristics
must not invent environment or platform truth without stronger evidence.

### Option A — repository-scoped: `workload:<repo_id>:<name>`

Uses `candidate.RepoID`, already on the struct: durable resolved identity, not a
heuristic.

- **Invents nothing.**
- **Removes the merge** — two repositories get two workloads.
- **Removes the reset hazard**, since `DEFINES` can no longer span repositories.
- **Cost:** the identifier stops being human-readable; every parse site in
  section 4 needs revisiting.
- **`DeploymentRepoIDs` must not be the key.** This was an open question and is
  now settled. It fails three independent tests for key material: **plural** (the golden
  corpus carries deployment-typed evidence from three separate fixtures —
  `helm-argocd-platform`, `kustomize-deployable-overlay`, `helm-umbrella-chart` —
  all pointing at the same `deployable-source` repo; note one committed snapshot
  note records a single-element resolved value, so treat "plural" as established
  and the exact count as unverified), **unstable** (the
  primary is re-picked whenever higher-confidence evidence arrives,
  `workload_deployment_sources.go:232-234`, so node identity would churn on a
  confidence change), and **usually absent** — that file's own comment at
  `:135-145` calls having no deployment evidence "the overwhelmingly common and
  entirely expected outcome". `RepoID` has none of these; empty-`RepoID`
  candidates are skipped outright at `projection.go:260`.

  Keying on the deployment repo would also make the node self-contradictory: the
  id would claim the config repo while `repo_id` and the `DEFINES` edge — both
  written from `WorkloadRow.RepoID` — claim the defining one. The
  `DEPLOYMENT_SOURCE` edge's own reason string says what that relationship is:
  "Deployment manifests for workload instance live in deployment repository."
  That is provenance, and the correlation rules keep deployment repos
  provenance-only.

- **"One workload or two" was largely a false premise.** In the golden corpus the
  split-repo family already materializes as *three separately named* workloads —
  `deployable-source`, `deployable-config`, `kustomize-deployable-overlay` —
  because names come from repo names, not manifest names. Their unity is carried
  by `DEPLOYS_FROM` / `CORRELATES_DEPLOYABLE_UNIT` / `DEPLOYMENT_SOURCE`, none of
  which a re-key touches. The name-only collapse merges only when repo *names*
  coincide, which is this defect rather than a feature.

### Option B — namespace or cluster-scoped: `workload:<cluster>/<namespace>:<name>`

- **Fails the no-invented-truth rule.** `candidate.Namespaces` is a list, and
  `namespaceEnvironmentFallback` already treats namespaces as weak evidence —
  good enough for an environment guess, not for identity. Cluster is not on the
  candidate at all.
- **Does not solve the stated problem:** two repositories deploying `checkout` to
  the same namespace still collide.
- **Reject.** It keys on deployment topology; the issue is about repositories.

### Option C — composite: workload scoped by repo, instance derived from it

```
workload:<repo_id>:<name>
workload-instance:<repo_id>:<name>:<environment>
```

Option A plus deriving the instance key from the already-scoped workload id
rather than re-deriving from the bare name. Under Option A alone, `instanceID`
at `:327` still builds `workload-instance:<name>:<environment>`, so beta's
staging instance keeps a name-only key and can still attach to the wrong
workload — which is half the wrong answer in section 2.

- **Invents nothing** (same inputs as A).
- **Fixes both halves**; instances inherit their parent's scope by construction.
- **Cost:** the largest identifier churn of the three.

## 6. Migration

Every edge in section 4 is anchored on the old value, so old nodes must be
retracted and rebuilt rather than rewritten in place.

0. **Make the identifier type-safe, as step zero.** Section 4's list is
   hand-maintained and has been wrong three times: it missed
   `projection_helpers.go:110` through two revisions, and then missed an entire
   structural category described below. Do not fix it by hand a fourth time.

   **Text search cannot close this.** Two grep shapes find most constructions:

   ```bash
   rg -n 'fmt\.Sprintf\("workload(-instance)?:' go/ --glob '!*_test.go'
   rg -n '"workload:"\s*\+|"workload-instance:"\s*\+' go/ --glob '!*_test.go'
   ```

   They return 12 sites, and they are provably not exhaustive. A third form
   builds the same string with no `workload:` substring anywhere in the source:

   ```go
   // searchdocs/project.go:283 — the kind literal, no colon
   GraphHandle{Kind: "workload", ID: clean(input.WorkloadID)}

   // searchbench/evidence.go:358 — the colon, in a different file
   return handle.Kind + ":" + handle.ID
   ```

   Both greps return zero on all three files involved. This is an ordinary Go
   refactor — hoisting a join into a generic helper — not an exotic evasion, and
   it defeats text matching by construction rather than by bad luck. Any guard
   built on grep has a **false-negative** blind spot, which is the worst property
   for something meant to be trusted as complete.

   **So make the compiler do it.** Introduce `type WorkloadID string` and
   `type WorkloadInstanceID string`, give each exactly one constructor, and have
   downstream consumers (Cypher parameters, struct fields, row builders) take the
   typed value instead of `string`. The question stops being "does a literal
   appear somewhere a grep can see" and becomes "does anything outside the
   constructor file produce a `WorkloadID`" — which the compiler answers, and
   which a reviewer verifies by reading one file.

   The marginal cost is low precisely because the re-key already has to touch
   every legitimate construction site. Doing the type change in the same pass is
   close to free; doing it later means touching them all twice.

   **What the type change does and does not give you**, since the point of this
   step is to stop overclaiming what a check covers:

   - **Cypher parameters are safe, verified.** The Bolt driver encodes
     `map[string]any` values by `reflect.Kind()`, not by exact type
     (`neo4j-go-driver/v5@v5.28.4` `internal/bolt/outgoing.go`, `case
     reflect.String`), so a `WorkloadID` reaches the wire correctly with no
     conversion and no silent loss. This is the boundary every graph write
     crosses, so it is the one that had to hold.
   - **`GraphHandle.ID` and similar `string` fields force an explicit
     conversion.** Go will not implicitly assign a defined type to a `string`
     field, so the crossing becomes `string(id)` — visible and greppable, where
     today it is invisible.
   - **Go conversions are unrestricted, so this is not a proof.**
     `WorkloadID(anyString)` compiles from any package; Go has no
     private-constructor mechanism for a defined string type. What the change
     actually buys is a collapse of the search space: instead of hunting
     unbounded string-construction *shapes* — three different ones found in
     three tries — you hunt one fixed token that cannot be spelled another way.
     Pair it with `rg -n '\bWorkload(Instance)?ID\(' go/ --glob '!<constructor>.go'`
     to flag conversions outside the constructor's own file.
   - **The Postgres/JSON `entity_key` path is outside the type's reach by
     nature, not by leak.** `reducer_workload_identity` facts are untyped
     storage; a static Go type cannot constrain them. That path needs its own
     mechanism, which is open question 3 — do not assume this step covers it.

   **If the type change is ruled out of scope**, the grep pair is still worth
   landing as a floor. The repo already has generate+verify pairs for this class
   of drift — `generate-fact-kind-registry.sh`/`verify-fact-kind-registry.sh` and
   `generate-env-registry-doc.sh`/`verify-env-registry-doc.sh` — and that
   *convention* is the thing to follow, not the `generator-script-discipline`
   skill's literal three-file template. Neither precedent uses a `lib/` chunk,
   because their source of truth is Go rather than shell data: each generator is
   a thin wrapper (16 lines around `go run ./cmd/fact-kind-registry`), its verify
   counterpart is the same program with `-check` so the two cannot drift, and the
   skill's test mirror is a separate `test-*.sh` file that already exists for
   both. A grep-based generator here would be a simpler, thinner shape than
   either — legitimate for this problem, but not the same structure.

   Two conditions if that route is taken:

   - **Key the pin by file plus matched line text.** Never by line number, which
     re-pins on unrelated edits above it and trains reviewers to rubber-stamp;
     never by count, since a count match is not a pin match — the failure that
     made the `dirgate` grandfather rows serialize.
   - **State the blind spot in the script's own header.** It bounds
     literal-adjacent construction and does not bound generic kind/prefix
     joiners; point at `searchdocs.GraphHandle` with `handleKey` as the shape to
     audit by hand. A guard advertised as complete when it is not invites
     exactly the "trust it and stop looking" failure this step exists to
     prevent.

1. **Rebuild, do not rewrite.** Retract every `Workload` and `WorkloadInstance`
   plus their edges, then re-project from facts. At 40 nodes and 33 instances
   this is cheap; the reducer already owns a correct rebuild path.
2. **Scope the RUNS_ON retract in the same change.**
   `retractRepoRunsOnEdgesCypher` / `retractSingleRepoRunsOnEdgesCypher`
   (`canonical_relationships.go:352-364`, dispatched from
   `edge_writer_retract_repo.go:112,114`) traverse `DEFINES` unfiltered and never
   scope the instance to the retracting repository. A repo-scoped key makes that
   traversal safe by construction. `mutations.go` needs no fix — it is
   unreachable (section 2) and its correct disposition is deletion under the
   repo's own dead-code precedent, separately from this work.
3. **Regenerate the golden artifacts** — 45 literals in the B-12 snapshot, 1 in
   the cassettes, moved in the same change per the golden-corpus rules.
4. **Re-key `WorkloadInstance.workload_id` in the same change.** The item most
   likely to be missed, and the only one that fails *silently*. It is a
   denormalized scalar copy of the workload id on every instance node, and
   **three** read paths filter on it directly: `workload_runtime_topology.go:90-97`
   (`i.workload_id = $workload_id`), `service_workload_resolution.go:249,284`
   (`w.id = i.workload_id`), and `compare.go:187-194`. Re-key the node without
   this and topology goes blank, environment compare degrades to "unsupported",
   and nothing errors anywhere.

   A fourth site, `impact_resource_investigation_reads.go:78`, only *projects*
   the scalar and uses it as the last fallback in
   `firstNonEmpty(resolved.id, workloadIDRaw, instanceID)` (`:151-161`), after a
   live `INSTANCE_OF` traversal that would return the correctly re-keyed id. It
   would most likely survive an unmigrated re-key, and its actual failure mode
   needs its own verification rather than being assumed to match the other three.

5. **Update the parse sites** in section 4, with a test per site. Two deserve
   naming: `catalog.go:328-333` (`catalogWorkloadKey` merges catalog rows by
   *name only*, so split siblings silently vanish from `/catalog`) and
   `mcp/dispatch_args.go:54-59` (`normalizeQualifiedIdentifier` cuts at the FIRST
   colon, so a three-part id becomes `<repo>:<name>` and 404s).

   The loud breaks are safer and already have error types: name-selector surfaces
   go ambiguous for collision names —
   `impact_trace_workload_selection.go:52-54` (`errAmbiguousTraceWorkloadSelector`)
   and `service_workload_resolution.go:93-104` (`serviceWorkloadAmbiguousError`).
   The `repo`/`environment` narrowing arguments those surfaces already accept
   become mandatory for collision names.
6. **Decide the `reducer_workload_identity` question** (section 8, question 3).
7. **Add the collision counter that does not exist**, and build it *with* the
   re-key rather than ahead of it. Nothing today reports two candidates
   contending for one identity.

   Placement matters more than it looks. Beside the `seenWorkloads` dedup
   (`projection.go:289`) the counter would be **blind to the case that matters**:
   that map is created per call (`:253`), so it is blind to mode one — the
   cross-scope merge, which is what git sync produces today and what section 2
   measured live. It is **not** blind to mode two: a multi-repo scope-generation
   is exactly the case where the dedup drops a row, and a counter on that branch
   would catch it. So one counter is not enough. Instrument both: the
   `seenWorkloads` branch, comparing the incoming candidate's `RepoID` against
   the retained row's, which detects a same-scope drop at the moment it happens;
   and a graph-side detector for workloads carrying more than one `DEFINES` edge,
   which is the only thing that sees a cross-scope merge. Section 3.1 has already
   run both graph-side detectors and both read zero — under an ingestion regime
   that only produces single-repo scopes, which is why those zeroes are a weaker
   signal than they look, and why the one sub-case they cannot see at all is the
   one the in-process counter exists for.

   After the re-key both become **regression guards**: two repositories defining
   one workload should be structurally impossible, and either counter firing
   means the key leaked.

## 6a. What holds a `workload:<name>` identifier, measured

This was an open question. It is answered, and the answer is the largest cost in
section 7: **these identifiers are a public contract, in three tiers that fail
differently.**

**Tier 1 — loud, and alias-fixable.** `workload_id` is a path parameter on
`/api/v0/workloads/{workload_id}/context` and `/story`
(`openapi_paths_entities.go:107,134`; handler matches `w.id` exactly at
`entity_workload_handlers.go:19,29`, 404 on miss). It is a required body field on
`POST /api/v0/compare/environments` (`compare.go:35,64`, `MATCH (w:Workload) WHERE
w.id = $workload_id` at `:160`, no name fallback). The CLI rejects a label
containing `:` (`cli/opdigest/digest.go:91,244`), so `workload:<repo_id>:<name>`
would be unusable for `eshu report --scope` until that parser changes. These break
visibly, which is the right behaviour for a contract in transition.

**Tier 2 — silent, alias-fixable with work.** Every claim in this tier is
**traced through the source, not executed against a re-keyed graph** — unlike
tier 1, where the exact-match and 404-on-miss behaviour is visible in the handler
itself. The tracing is careful and I believe it, but the document holds section 2
to a measured standard and these are not measured.

The impact resolver tries id, then a
`workload:`-prefixed candidate, then name (`impact_change_surface_resolvers.go:82-108`),
so a stored prefixed handle matches none of the three after a re-key and resolves
empty. MCP's `normalizeQualifiedIdentifier` cuts at the first colon
(`dispatch_args.go:54-59`), turning a three-part id into `<repo_id>:<name>`.
Search documents persist `GraphHandles{Kind:"workload", ID}` in Postgres
(`storage/postgres/eshu_search_index.go:216,300-312`) and stay stale until
reindexed. The catalog read model *synthesizes* graph-shaped ids from the
separate `reducer_workload_identity` scheme (`catalog.go:213`), so the Console is
handed ids that would 404 against `/workloads/{id}/context`.

**Tier 3 — silent, and NOT alias-fixable.** `workload_id`/`workload_ids` are
documented in the published SDK fact schema as **provider-asserted**
(`sdk/go/factschema/aws/v1/attribute_shapes.go:56-63`;
`servicecatalog/v1/repository_link.go:72` — "a provider-asserted Eshu workload
id"), and `cloudResourceServiceAnchorDecisionForPayload`
(`aws_resource_service_anchor.go:121-139`) passes them straight through from
`Resource.Attributes` with no name-matching fallback. They reach an exact match in
`workload_cloud_relationship_writer.go:21`:

```cypher
MATCH (workload:Workload {id: row.workload_id})<-[:INSTANCE_OF]-(instance:WorkloadInstance)
```

A `MATCH` that misses is a silent no-op — the `USES` edge simply is not written.
**A read-side alias layer cannot cover this**, because the value originates
outside the repository. It needs a reducer-side resolution step, and the
ambiguity path it would use already exists (`ambiguous_anchor`,
`workload_cloud_relationship_materialization.go:306-308`).

**How much this actually costs is not knowable from inside this repository, and
the honest answer is neither reassuring nor alarming.** Nothing in-tree writes
these attributes — a sweep of `go/internal/collector/` for a writer of
`workload_id`/`workload_ids` on AWS resource attributes returns zero. That is not
an accident of coverage: `servicecatalog/facts_builder.go:34-36` states that
"service_id and workload_id are deliberately absent: the collector observes a
YAML file and has no canonical service or workload identity to assert," and four
collector test files pin `workload_id` blank. So the path is wired end to end —
decode, exact `MATCH`, silent miss — with **no known writer in this codebase**.
Whether real deployments feed it is outside what this repository can show. It
stays tier 3 because "no in-tree writer today" is a different claim from "no
writer," and a published schema field is a contract whether or not this repo
exercises it.

**And users hold these handles.** Public docs give copyable examples with the
prefix spelled out — `GET /api/v0/workloads/workload:payments-api/context`
(`docs/public/guides/shared-infra-trace.md:47,53`),
`{workload_id: "workload:api-svc"}` (`getting-started/first-five-questions.md:67`),
and `catalog-workload-selection.md:57` tells readers that pasted `workload:...`
handles "remain canonical". The Console builds ids from names and puts them in
bookmarkable URLs (`/workspace/services/<id>`), and prompts operators to paste
`workload:…` directly.

Migration must therefore add, beyond section 6: a read-side resolution layer for
tiers 1 and 2 that fails closed on an ambiguous legacy name, a reducer-side
resolution step for tier 3, a search reindex, and doc updates in lockstep.

## 7. Recommendation and cost

**Option C, taken now rather than later. There is no separable quick fix.**

An earlier revision recommended shipping a `mutations.go` fix immediately and
independently. That recommendation is withdrawn: the path is unreachable
(section 2). The live leak is the RUNS_ON upsert/retract pair, and scoping it
needs to know which repository owns a workload — so it rides with the re-key
rather than ahead of it.

The case for now is section 3.2: cost scales with the workload population, which
is 40 nodes, 33 instances, and 46 golden literals today. **Both failure modes are
silent.** Mode one merges — one node, ownership reassigned to whoever wrote last,
cross-repo environments. Mode two drops — the losing repository's workload row
discarded, so the surviving node carries someone else's kind, classification,
confidence and provenance, no `DEFINES` edge for the repository that lost, and
where environments differ, instances owned by a repository with no edge to the
workload they hang off. Neither errors and neither increments anything.

The case for waiting is that the corpus detectors read zero — with the scoping
section 3.1 puts on that zero: measured under single-repo-per-scope ingestion,
covering mode one and one of mode two's two sub-cases, and blind to the other.
And this is a schema-risk change with cassette and B-12 impact.

| Item | Estimate |
| --- | --- |
| Key change | Two format strings. Trivial. |
| Edge/parse-site sweep | The section 4 inventory, now including a non-graph subsystem. **Moderate-to-large, and the main risk.** |
| RUNS_ON scoping | Small once the key is decided; cannot land before it. |
| Golden regeneration | 46 literals across the snapshot and one cassette. |
| Retract/rebuild proof | Live replay-tier retract coverage per edge family. Moderate. |
| Collision telemetry | Small. |
| Consumer breakage | **The largest item.** Public API path/body params, MCP selectors, the Console's bookmarkable URLs, persisted search handles, and provider-asserted ids in the SDK fact contract. Section 6a. |

## 8. Open questions

1. **Which repository owns a workload deployed from a different repository?**
   **Recommended answer: the defining repository, `WorkloadCandidate.RepoID`.**
   Section 5 gives the evidence — `DeploymentRepoIDs` is plural, unstable and
   usually absent, and keying on it makes the node contradict its own `repo_id`
   and `DEFINES` edge. Every `DEFINES`-paired query tightens under source keying
   and goes silently false under deployment keying. This is presented as a
   recommendation rather than a decision: it is the one answer the whole design
   rests on, so it should be confirmed rather than assumed.
2. **Do stored `workload:<name>` identifiers exist outside the graph?**
   **Answered: yes, on every axis — this is a public contract break, not an
   internal refactor.** See section 6a. No decision needed; the cost is now
   known and is the largest line item in section 7.
3. **Does the `reducer_workload_identity` Postgres fact get re-keyed too?** It is
   keyed on repo basename, is the documented fallback for an unmaterialized
   workload, and a graph-only re-key leaves the two schemes disagreeing.
4. **Now or later for the re-key?** Section 7 gives both cases. There is no
   separable timing decision beside it: the RUNS_ON scoping fix needs the
   identity answer, so it rides with the re-key either way.

## 9. Sign-off request

Before any code is written I need:

- **A decision on Option C over A**, and on timing.
- **An answer to question 1** — it determines the key.
- **A decision on question 3**, the non-graph identity subsystem.
- **Confirmation of the recommended answer to question 1**, since everything else
  in this design follows from it.

There is no longer an independent fix to approve. An earlier revision asked to fix
`mutations.go:119` on its own; that path is unreachable (section 2), and the real
leak — the RUNS_ON retract — cannot be scoped without the identity answer, so it
belongs to this issue rather than beside it.

No production code has been written for this issue and none will be until the
above is settled. The probe files are throwaway and are not in this branch.
