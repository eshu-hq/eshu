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
(`correlated_workload_projection_input_loader.go:35-42`). Two unrelated
repositories therefore arrive as **two separate calls**, not one.

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

### The narrower same-scope case, which is different

Within a *single* intent, `projection.go:289` dedups on the name-only id before
anything is written, so a second candidate with the same name in the same
scope-generation is dropped rather than merged. That is the dedup's actual job.
It cannot help across repositories, because the map is created per call
(`projection.go:253`).

This distinction matters: an earlier draft of this document tested only the
same-scope path and concluded the losing repository was silently dropped. Driven
through the production shape, the opposite happens. **The failure is a merge, not
a drop.**

### The reset path makes it destructive

`go/internal/graph/mutations.go:113-129` builds a repository's delete set from
four collections, the first being:

```cypher
OPTIONAL MATCH (r)-[:DEFINES]->(defined_workload:Workload)
```

with no `repo_id` filter, unioned with the `repo_id`-scoped ones and then
`DETACH DELETE`d. Because a collided workload genuinely carries `DEFINES` edges
from both repositories — measured above, not hypothesised — **resetting
repository A destroys shared state that belongs to repository B.**

Precisely what happens, since this is the one item asking for an independent fix:

- Repository B's **`Workload` node is deleted outright**, along with its
  `DEPENDS_ON` edges, which sit directly on the removed node.
- Repository B's **`WorkloadInstance` nodes survive but are orphaned.**
  `DETACH DELETE` removes only the relationships touching the deleted node, so
  the instances lose their `INSTANCE_OF` edge and with it their only path back to
  a workload. The rows remain, with `repo_id` still correct, and instance-rooted
  queries still find them — `infra_ecosystem_overview.go:58` counts platforms via
  `MATCH (i:WorkloadInstance)-[:RUNS_ON]->(p:Platform)` and never touches
  `INSTANCE_OF`. What breaks is every Workload-rooted traversal, which is the
  shape most consumers use (`catalog_workload_environments.go:69`,
  `entity_workload_platform.go:68`, `workload_runtime_topology.go:103`, and
  others, all `MATCH (w:Workload)...<-[:INSTANCE_OF]-(i)`).
- **Endpoints are partially severed.** There are two `EXPOSES_ENDPOINT` writers
  (`workload_materializer.go:452` from the repository, `:459` from the workload).
  Only the workload-sourced edge touches the deleted node, so an endpoint that
  also carries repository B's own edge stays reachable through B directly.

B does not get the deleted node back until B is re-ingested.

This is a live defect gated only on a name collision existing, not a latent one.
Any option below must fix this statement.

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

Section 2 establishes that a collision produces exactly two `DEFINES` edges on
one node, so the highlighted row is a **valid, non-vacuous detector for the
failure this issue describes** — and it is zero. So is the independent
instance-side detector.

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

### 3.2 Why the population is so small

40 workloads from 908 repositories is not an accident: admission requires
`confidence >= 0.82` (`workloadMaterializationMinConfidence`, `projection.go:25`)
plus a materializable classification. The name space is small, so names have
little opportunity to collide.

This is the crux of the timing decision, and it cuts both ways:

- **Against acting now:** zero observed collisions, on the largest corpus there is.
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
- **Open question it raises:** a service defined in a source repository and
  deployed from a config repository becomes two workloads where today it is one.
  Whether that is a fix or a regression depends on which repository `RepoID`
  carries — and `DeploymentRepoIDs` existing separately suggests the model
  already distinguishes them.

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
2. **Fix `mutations.go:119` in the same change.** Its unfiltered `DEFINES` match
   is destructive *today* (section 2), so this is worth doing on its own merits
   regardless of which key wins.
3. **Regenerate the golden artifacts** — 45 literals in the B-12 snapshot, 1 in
   the cassettes, moved in the same change per the golden-corpus rules.
4. **Update the parse sites** in section 4, with a test per site.
5. **Decide the `reducer_workload_identity` question** (section 8, question 3).
6. **Add the collision counter that does not exist.** Nothing today reports two
   candidates contending for one identity.

**Compatibility:** any consumer holding a stored `workload:<name>` identifier —
saved query, dashboard, external API caller — breaks.
`impact_change_surface_resolvers.go` accepts a bare name and prefixes it, so that
entry point survives; a stored full id does not.

## 7. Recommendation and cost

**Option C. Fix the reset statement immediately and independently; take the
re-key now rather than later, though that half is finely balanced.**

The reset fix is not finely balanced. Section 2 shows repository A's reset
destroys repository B's workload whenever a name collision exists. That is a
data-loss path with a one-clause fix, and it should not wait for the identity
decision.

For the re-key, the case for now is section 3.2: cost scales with the workload
population, which is 40 nodes, 33 instances, and 46 golden literals today. The
failure is silent — a merged node, reassigned ownership, and cross-repo
environments, with no error and no counter. The case for waiting is that measured
collisions are zero, and this is a schema-risk change with cassette and B-12
impact.

| Item | Estimate |
| --- | --- |
| Key change | Two format strings. Trivial. |
| Edge/parse-site sweep | The section 4 inventory, now including a non-graph subsystem. **Moderate-to-large, and the main risk.** |
| `mutations.go` fix | Small, and worth landing on its own. |
| Golden regeneration | 46 literals across the snapshot and one cassette. |
| Retract/rebuild proof | Live replay-tier retract coverage per edge family. Moderate. |
| Collision telemetry | Small. |
| Consumer breakage | Unknown until question 2 is answered. |

## 8. Open questions

1. **Which repository owns a workload deployed from a different repository?**
   `RepoID` and `DeploymentRepoIDs` are separate fields; the key must use the
   right one. **Load-bearing — the design cannot be accepted without it.**
2. **Do stored `workload:<name>` identifiers exist outside the graph** — saved
   queries, dashboards, external callers?
3. **Does the `reducer_workload_identity` Postgres fact get re-keyed too?** It is
   keyed on repo basename, is the documented fallback for an unmaterialized
   workload, and a graph-only re-key leaves the two schemes disagreeing.
4. **Now or later for the re-key?** Section 7 gives both cases. The reset fix
   should go now either way.
5. **Should the collision counter land first?** It is useful under every outcome
   including do-nothing, and would turn question 4 into a measurement.

## 9. Sign-off request

Before any code is written I need:

- **A decision on Option C over A**, and on timing.
- **An answer to question 1** — it determines the key.
- **A decision on question 3**, the non-graph identity subsystem.
- **Agreement that the `mutations.go` fix can go ahead independently**, since it
  is a live data-loss path and does not depend on the identity decision.

No production code has been written for this issue and none will be until the
above is settled. The probe files are throwaway and are not in this branch.
