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

The reducer materializes one `Intent` per call — the handler's signature is
`Handle(ctx, intent Intent)`
(`go/internal/reducer/workload_materialization_handler.go:127`) — and that
intent's candidates are loaded for a single `(scope_id, generation_id)` pair
(`go/internal/reducer/correlated_workload_projection_input_loader.go:35-42`).
The projection build itself happens once per `Handle` call, at
`workload_materialization_handler.go:216`.

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

- **One `Workload` node, two owners.** The single-row form is
  `MERGE (w:Workload {id: $workload_id}) SET … w.repo_id = $repo_id`
  (`go/internal/storage/cypher/canonical.go:45-54`); the batch form binds the
  same shape per row as `row.workload_id` / `row.repo_id`
  (`go/internal/reducer/workload_materializer.go:351-357`). Either way `repo_id`
  is set unconditionally, so alpha's workload is now attributed to beta. Nothing
  records that alpha ever owned it.
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
second repository's are gone. `seenInstances` (`:329`) does the same to any
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

Whether mode two leaves any graph trace at all depends on the environments.
Both sub-cases below are **traced from source, not executed** — the same standard
section 6a's tier 2 is held to:

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

The same shape: `DEFINES` is anchored to the retracting repository, but it
traverses into a possibly-shared `Workload`, and the `INSTANCE_OF` hop that
follows never scopes `i` back to that repository. The matching upsert (`canonicalRunsOnUpsertCypher`, `:322-330`) has the
identical two-hop shape, and there `evidence_source` is only *set*, never filtered —
so nothing scopes the write side either.

**Measured on a live NornicDB at the pinned revision**, driving the production
dispatch (`EdgeWriter.RetractEdges` → `edge_writer_retract_repo.go`) for both the
single-repo and UNWIND retract shapes.
Identical across 6/6 runs (ledger:5385-runs-on-cross-repo-retract-leak):

```
precondition:      workload_nodes=1 defines=2 instances=2
after beta pass:   beta_inst->beta_plat=1   alpha_inst->beta_plat=1
after alpha pass:  beta_inst->beta_plat=0
                   beta_inst->alpha_plat=1
                   alpha_inst->alpha_plat=1
                   gamma_inst->gamma_plat=1   (non-colliding control, untouched)
```

**Correction, from the retract/rebuild proof: the statement above deletes
nothing, so it cannot be what moved these edges.** Driving
`retractSingleRepoRunsOnEdgesCypher` verbatim over Bolt — eshu's own session
configuration, result consumed, and a 45-second settling loop because this
backend has eventual-read consistency — leaves the edge in place: `before=1
after=1`. Neo4j 5 deletes it. A bare relationship `DELETE` does not delete on the
pinned build at all, which makes this retract a **silent no-op** rather than an
over-broad delete.

So the `beta_inst->beta_plat` transition from 1 to 0 above has a different cause,
**and this document does not currently know what it is.**

A previous revision named `batchWorkloadInstanceRetractCypher`
(`workload_materializer_retract_instances.go:35-38`), which `DETACH DELETE`s the
`WorkloadInstance` node. That attribution is withdrawn, for two independent
reasons, either of which is sufficient:

- **The probe never dispatched it.** The run drove `EdgeWriter.RetractEdges`
  only, which reaches `edge_writer_retract_repo.go` and dispatches exactly three
  roles. `batchWorkloadInstanceRetractCypher` is not among them: it lives in a
  different package and is dispatched only through
  `WorkloadMaterializer.RetractInstances`. Taking the three in turn, none can
  produce the transition:

  - `repository_relationship_edges` (`:37`) and `runs_on_relationships` (`:51`)
    are relationship `DELETE`s, and section 5a measured every relationship
    retract as inert on this pinned build — 1 → 1 where Neo4j gives 1 → 0.
  - `evidence_artifacts` (`:64`) is **not** a relationship delete. An earlier
    revision of this list called all three relationship deletes and said the
    dispatch contained "no node delete at all"; that was wrong, and it matters,
    because node deletion is the one class section 5a proves does work here.
    `retractRepoEvidenceArtifactsCypher` (`canonical_relationships.go:365-374`)
    ends in `DETACH DELETE artifact`. It is eliminated on different grounds: it
    reaches `EvidenceArtifact` nodes through
    `(source_repo)-[rel:HAS_DEPLOYMENT_EVIDENCE]->(artifact)`, and a
    `WorkloadInstance`→`Platform` edge is not incident to an `EvidenceArtifact`,
    so `DETACH DELETE` on that node cannot touch `beta_inst->beta_plat`.

    Non-incidence alone does not close this, because labels are per-node and
    additive: a node carrying both `:EvidenceArtifact` and `:Platform` would be
    bound by that `MATCH` and would take its `RUNS_ON` edges with it. Two facts
    rule that out. `EvidenceArtifact` nodes have exactly one write site, a
    single-label `MERGE (artifact:EvidenceArtifact {id: row.artifact_id})`
    (`canonical_relationships.go:278`) over a dedicated id space
    (`repoEvidenceArtifactID`, `edge_writer_row_metadata.go:141`), and a
    single-label `MERGE` creates a distinct node rather than binding a
    differently-labelled one even on an id collision. And the label never appears
    on a node carrying another: `rg ':EvidenceArtifact:|:[A-Za-z]+:EvidenceArtifact'`
    over `go/` and `sdk/` returns nothing, tests included. That is the search that
    matters, because multi-label `MERGE` — not `SET` — is how this codebase builds
    multi-label nodes, at ten production sites across `package_registry_*_writer.go`
    and `oci_registry_canonical_writer.go`
    (`MERGE (p:Package:PackageRegistryPackage {uid: row.uid})` and siblings). No
    `SET` adds it either; the only label-adding `SET` in production Go is
    `SET r:TerraformStateResource` (`tfstate_canonical_writer_retract.go:56`).

    One limit worth stating, since this document separates what it traced from
    what it ran elsewhere: that argument is about the production graph model,
    while the thing being explained is a throwaway probe fixture whose node
    labels cannot be checked from this tree. It holds if the fixture used the
    production label model, which is what the probe was built to exercise.
- **Its own guard forbids the behaviour attributed to it.** The statement is
  `MATCH (i:WorkloadInstance {id: row.instance_id}) WHERE i.repo_id IN $repo_ids
  AND i.evidence_source = $evidence_source`. During alpha's pass, beta's repo id
  is not in `$repo_ids`, so it is structurally incapable of deleting beta's
  instance — which is exactly the scoping the same section credits it with.

**What survives this correction, and what does not.** The observation survives:
6/6 runs (ledger:5385-runs-on-cross-repo-retract-leak), beta's edge gone,
measured and ledgered. The end state survives and is still wrong: beta's instance
ends up asserting it runs on alpha's platform. The two-sided framing below is
unaffected, because it never depended on the retract.

**The write-side mechanism survives and is the one this design rests on.**
`canonicalRunsOnUpsertCypher` (`canonical_relationships.go:322-330`) is
`MATCH (repo:Repository {id: row.repo_id})-[:DEFINES]->(w:Workload)` followed by
`MATCH (i:WorkloadInstance)-[:INSTANCE_OF]->(w)` — two hops with nothing scoping
`i`, and `evidence_source` only `SET`, never filtered. That is a verified
in-source path for one repository's platform attaching to another repository's
instance, and it is what the two-sided bullet below reports.

What does not survive is the *retract-side* mechanism. The relationship retract
is inert on the pinned build and the node retract was never dispatched, so the
`beta_inst->beta_plat` 1 → 0 transition specifically has **no supported cause**. Do not build on the mechanism; the
observation is what this design relies on, and identifying the cause needs
another probe run with per-statement dispatch logging — the three dispatched
roles are each eliminated above, so the next move is watching what actually
executes rather than re-reading the same three statements. That is a gap in this document, not a reason to doubt the
measurement.

**And it surfaces a second defect worth its own attention.** If
`retractSingleRepoRunsOnEdgesCypher` is inert, then stale `RUNS_ON` edges are
never retracted by it on this backend. That is not an identity problem and it
does not block the re-key, but it means the retract half of any
retract-and-rebuild reasoning about `RUNS_ON` cannot be relied on until the
backend deletes relationships.

Three things the measurement shows that the retract-side framing above does not:

- **The contamination is two-sided and does not need the retract at all.** Beta's
  plain *write* already attached beta's platform to alpha's instance, before any
  retract ran.
- **The end state asserts something false**, rather than merely losing an edge:
  beta's instance ends up claiming it runs on alpha's platform.
- **The damage was observed only on** the `resolver/cross-repo` evidence source.
  The materializer's own `reducer/workloads` RUNS_ON edges survived every run,
  and the non-colliding control was never touched. "Observed only on" rather than
  "bounded to": bounding is a claim about which code path did the damage, and the
  retract-side mechanism is unknown.

What this does *not* show, stated plainly: that a collision exists in production.
Section 3.1's detectors read zero on the largest corpus available, with the
coverage limits recorded there. What it does show is that the *write* path
contaminates whenever a collision exists — that half is mechanised in the upsert
above and reproduced 6/6. It does not show why the retract-side transition
happened, and nothing downstream should lean on that half.

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
rows even when the values differ, reproduced on every attempt across NornicDB
1.2.1 and 1.2.2 — three attempts, no ledger row, so read it as "consistently"
rather than as a rate). A
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
real and contaminating — it adds a false edge rather than removing a true one;
the trigger is currently absent.

**The two detectors do not cover the same ground, and it is worth being precise
about which covers what.**

The `DEFINES` detector — the highlighted row — sees **mode one only**. A merged
node keeps both edges, so it is visible; a mode-two drop writes no edge for the
losing repository at all, and what survives looks like an ordinary single-definer
workload.

The **instance-side detector** would, by query shape, catch a differing-environment
mode-two leak: those instance rows are written carrying the losing repository's
`repo_id` while attached to the shared workload, so `i.repo_id <> w.repo_id`
holds. Worth keeping as a design note for the counters in migration item 7.

**But its zero here is not evidence about mode two, and an earlier revision of
this section wrongly credited it as such.** Mode two requires two colliding
repositories in one scope-generation. This corpus was ingested entirely through
git sync, which commits one repository per scope — so the precondition never
occurred, anywhere in its history, for any workload name, in either sub-case. A
detector reading zero against a condition that was never reachable is not
measuring that condition. It is blind for a different reason than the `DEFINES`
detector — structural unreachability rather than query insensitivity — but it is
equally blind.

So: **neither mode-two sub-case is measured by this corpus.** The honest reading
of both zeroes is "no cross-scope merge and no cross-repo instance span, under an
ingestion regime that only ever produced single-repo scopes" — not "zero
collisions", and not "mode two checked and absent".

None of this changes the recommendation. It is the case for both counters in
migration item 7, which are the only things that would see **any** of mode two.

### 3.2 Why the population is so small

40 workloads from 908 repositories is not an accident: admission requires
`confidence >= 0.82` (`workloadMaterializationMinConfidence`, `projection.go:25`)
plus a materializable classification. The name space is small, so names have
little opportunity to collide.

This is the crux of the timing decision, and it cuts both ways:

- **Against acting now:** zero observed cross-scope merges and zero cross-repo
instance spans, on the largest corpus there is — but see 3.1 for what that zero
does and does not cover. It was measured under single-repo-per-scope ingestion,
which never exercises mode two at all, so it bears on mode one only.
- **For acting now:** 40 nodes and 33 instances is the cheapest this migration
  will ever be, and the admission gate is expected to widen, not narrow.

### 3.3 Golden corpus

| Artifact | `workload:` | of which id values | `workload-instance:` | of which id values |
| --- | ---: | ---: | ---: | ---: |
| `testdata/golden/e2e-20repo-snapshot.json` | 33 | 32 | 12 | 6 |
| `testdata/cassettes/` | 1 (in 1 file) | 1 | 0 | 0 |

One number cannot carry both halves, and the split is what a regenerator needs:
**39 identifier values that regeneration changes, plus 7 prose mentions in
snapshot notes that a human has to update by hand.** The 46 raw occurrences are
the sum of the two. The 32 quoted `workload:` values are 13 `api-svc`, 10
`deployable-config`, 7 `deployable-source`, 1 `claim-honesty-demo` and 1
`supply-chain-demo-db`; the 6 instance values are all
`workload-instance:deployable-source:prod` or `:stage`; the cassette's single
value is `"workload_object_id": "workload:claim-honesty-demo"`
(`testdata/cassettes/kuberneteslive/supply-chain-demo.json:445`).

The B-12 snapshot's own node counts are floor/ceiling ranges rather than
per-repository lists, so it cannot independently answer the collision question;
these literals are the regeneration surface, not a second measurement.

## 4. Edge families and identifier sites a re-key must carry

This section is organised by **what a site does with the identifier**, because
that is what decides the migration action. Earlier revisions had one table, one
prose block and one flat "parse sites" list, with no kind labelled — so
"parse sites" became the catch-all: it swallowed three construction sites and
excluded every equality-anchored read. With no category defined, nothing stated
what *complete* would mean, and a reviewer could only lengthen the list, never
check it.

Each subsection below says how its members are enumerated. Where the population
is small and every member needs its own migration decision, that is a list.
Where it is large and the action is the same for every member, it is a criterion
you can run, with the count it returned and the commit it returned it at — so
staleness is falsifiable in one command rather than implied.

The stamp is a **`main`** commit deliberately. The criterion searches `go/` only and
this branch changes no Go file, so the count is a property of main's tree rather than
of this branch — and a branch sha does not survive the branch. An earlier revision
stamped `bdd4f6768`, which three rebases and a message rewrite left reachable from no
ref at all; `git checkout` of it fails with `reference is not a tree`, so the count
became unverifiable. A stamp that cannot be checked is worse than none, because the
stamp is what invites the check.

**What this does and does not buy.** Criterion plus count makes staleness
*detectable*, not *detected*: nothing re-runs a count that lives in prose. There
are three rungs, and this section is on the first — falsifiable by hand, enforced
by nobody. The second is the generate/verify pair section 6 item 0 proposes as
its floor, enforced by CI. The third is the typed constructor, enforced by the
compiler. Do not read this section as more than rung one.

That framing is about **enforcement** — who checks. There is a second axis it does
not cover: what cannot be checked at all. Section 4.4's "anchors composed at
runtime" table is itself hand-maintained, and item 0's argument applies to it in
full. The restructure reduces the hand-kept surface there from 27 rows to 4 and
makes those 4 the ones where each member needs its own decision anyway, which is a
large improvement rather than an escape. The generator at `impact_anchor_resolve.go`
is the sharpest case: a refactor could multiply the anchors it emits and nothing in
section 4 would move.

### 4.1 Identifier constructions

Enumerated by hand; two for the instance id, five for the workload id. These are
the **pure** constructions. Three further sites construct as part of a parse and
are listed in 4.5 rather than double-listed here, and one is in the console; the
paragraph after the table names all four so the count cannot be read as the whole
population.

| Builds | At |
| --- | --- |
| `workload-instance:<name>:<env>` | `go/internal/reducer/projection.go:327` |
| `workload-instance:<name>:<env>`, again | `go/internal/reducer/projection_helpers.go:110` |
| `workload:<name>` | `go/internal/reducer/projection.go:279` |
| `workload:<name>` for the `DEPENDS_ON` target | `go/internal/reducer/dependency.go:76` |
| `workload:<basename>` for the `shared_followup` fact, `reducer_domain: "workload_identity"` | `go/internal/collector/git_followup_facts.go:52` |
| `workload:<basename>` for the `shared_followup` fact **again**, `reducer_domain: "workload_materialization"` | `go/internal/collector/git_followup_facts.go:188` |
| `"workload:" + workloadName` for the returned row's `id` | `go/internal/query/entity_workload_context.go:261` |

**The two `git_followup_facts.go` rows are the same expression in the same file
under two different reducer domains** (`:51` and `:187`), and an earlier revision
of this table counted them as one. That is precisely the failure the next
subsection exists to warn about, committed by the table doing the warning — which
is the strongest argument in this document for item 0's generated criterion over
a hand-kept count. Section 4.7 named both envelopes correctly while this table
named one; a generator cannot disagree with itself that way.

**Constructing sites that also parse, and so live in 4.5:**
`go/internal/query/catalog.go:213` (idempotent re-prefix),
`go/internal/query/impact_change_surface_resolvers.go:107` (construct guarded by
a prefix test at `:104`), and `go/internal/query/entity_workload_context.go:286`
(an equality chain whose third arm constructs). Outside `go/`,
`apps/console/src/pages/VulnerabilitiesReachable.tsx:377` builds
`` `workload:${workload.id.slice("wl:".length)}` `` — a console-side construction
that also re-keys off a *different* prefix, and the only construction this
document has found outside `go/`. **It is demo-only, and that disposition is part
of the row.** `wl:` has exactly one source in the console:
`apps/console/src/console/demoModel.ts:74,96,97`, the neutral prospect demo
fixture, whose header states that private mode "never falls back to these rows".
The guard at `:376` therefore cannot fire on live data — a live
`workload:<repo_id>:<name>` does not start with `wl:`, so the branch is skipped
and `:379` returns `workload.label || workload.id` — the value unmodified by the
`wl:` slice, which is the point here, though for a node carrying a display label
that value is the label rather than the id. The consequence of a re-key here is "update
the demo fixture", not "production breaks", and it is listed beside five
production constructions only because it is a construction, not because it
carries their risk.

Applying that discipline is not optional in this document: `entity.go:58` is
marked dead on the production path and `dependency_domain` carries "zero
production callers" for the same reason. A row without its reachability
disposition reads as production risk by default, and this is the fourth site
where that distinction changed the answer.


#### The second instance-id construction, and why it is easy to miss

**A second construction of the instance id lives outside `projection.go`.**
`go/internal/reducer/projection_helpers.go:110` builds `fmt.Sprintf("workload-instance:%s:%s", workloadName, environment)`
again, for `RuntimePlatformRow.InstanceID`, which feeds
`MATCH (i:WorkloadInstance {id: row.instance_id})` in the `RUNS_ON` upsert
(`go/internal/reducer/workload_materializer.go:410-411`). A re-key that updates `projection.go:327`
and the node id but misses this line leaves the `MATCH` looking for the old
name-only format: it finds nothing, and `RUNS_ON` silently stops being written
with no error. This is the precise failure class this inventory exists to
prevent, and it was missed on the first two passes over this section.

One further site, and one that looks like a site and is not:

- `go/internal/reducer/dependency.go:76` — `targetWorkloadID := fmt.Sprintf("workload:%s", depName)`,
  a second name-only reconstruction for the `DEPENDS_ON` target.
- `go/internal/reducer/dependency_domain.go:101,116` — `partitionKey = fmt.Sprintf("workload:%s->%s", ...)`.
  An earlier revision called this the reducer's conflict domain for dependency
  edges and said a re-key changes the shape of a concurrency partition key. **That
  production consequence does not exist, and the claim is withdrawn.**
  `BuildWorkloadDependencyIntentRows` (`dependency_domain.go:88`) has zero
  production callers — every call is from `dependency_domain_test.go:208,237,278`.
  The live path is `BuildWorkloadDependencyIntentRowsFromEdges`
  (`workload_dependency_reconciliation.go:181-204`), called once from
  `workload_materialization_handler.go:328`, and it sets `RepositoryID` and
  `Payload` and never `PartitionKey`.

  What is real is smaller and still worth recording: both arguments already carry
  a `workload:` prefix (`dependency.go:76` for the target;
  `projection.go:279`→`:286`→`dependency.go:87` for the source), so the string
  this builds is `workload:workload:a->workload:b`. The three tests pass
  unprefixed fixture ids, so the doubling is invisible to the only code that runs
  it — which also means those tests do not pin the production id shape. This is
  the same unreached-code pattern section 2 withdrew for `mutations.go`, and this
  document made the error twice.

### 4.2 Node property stamps

Where the identifier lands as a property rather than as an edge anchor.

| Property | Stamped at |
| --- | --- |
| `endpoint.workload_id` | `go/internal/reducer/workload_materializer.go:441` |
| `Endpoint` node id, built from the workload id | `stableAPIEndpointID(repoID, workloadID, path)`, `go/internal/reducer/projection_helpers.go:58` |
| `WorkloadInstance.workload_id` | `go/internal/storage/cypher/canonical.go:63` (`i.workload_id = $workload_id,`) and `go/internal/reducer/workload_materializer.go:376` (`i.workload_id = row.workload_id,`). This is the denormalized copy migration item 4 covers, and at least five read paths filter on it. |

### 4.3 Edge writes anchored on the identifier

Enumerated by hand; every member needs its own migration decision.

| Edge | Anchor | Written at |
| --- | --- | --- |
| `(Repository)-[:DEFINES]->(Workload)` | `workload_id` | `go/internal/reducer/workload_materializer.go:364`, `go/internal/storage/cypher/canonical.go:52` |
| `(WorkloadInstance)-[:INSTANCE_OF]->(Workload)` | both | `go/internal/reducer/workload_materializer.go:385`, `go/internal/storage/cypher/canonical.go:66` |
| `(WorkloadInstance)-[:RUNS_ON]->(Platform)` | `instance_id` | `go/internal/reducer/workload_materializer.go:413`, `go/internal/storage/cypher/canonical.go:82`; id built independently at `go/internal/reducer/projection_helpers.go:110` |
| `(WorkloadInstance)-[:DEPLOYMENT_SOURCE]->(Repository)` | `instance_id` | `go/internal/reducer/workload_materializer.go:393`, `go/internal/storage/cypher/canonical.go:89` |
| `(Workload)-[:DEPENDS_ON]->(Workload)` | `workload_id`, `target_workload_id` | `go/internal/reducer/workload_materializer.go:429`, `go/internal/storage/cypher/canonical.go:114-116, 174-176` |
| `(Workload)<-[:INSTANCE_OF]-(WorkloadInstance)-[:USES]->(CloudResource)` | `workload_id` | `go/internal/storage/cypher/workload_cloud_relationship_writer.go:21` |
| `(Function)-[:RUNS_IN]->(Workload)` | resolved via `(repo)-[:DEFINES]->(w)` | `go/internal/storage/cypher/canonical_runs_in_edges.go:24-31` |
| documentation edge onto `Workload` | `target_entity_id` | `go/internal/storage/cypher/canonical_documentation_edges.go:35` |
| `(Workload)-[:EXPOSES_ENDPOINT]->(Endpoint)` | `workload_id` | `go/internal/reducer/workload_materializer.go:459`, anchored by `MATCH (workload:Workload {id: row.workload_id})` at `:457` |

`EXPOSES_ENDPOINT` has **no `canonical.go` counterpart** — unlike every other row
above, which cites one. There are exactly two write sites in the tree, both batch
constants in `workload_materializer.go` (`:456-461` for the workload edge,
`:449-454` for the repository sibling), and `canonical.go` contains no `Endpoint`
at all. A reader looking for the single-row form will not find one.

### 4.4 Reads that equality-match the identifier

Large and mostly uniform, so this one is a criterion rather than a list — the
implementer's action is the same for every member. Run from the repository root:

```bash
rg -nP -g '*.go' -g '!*_test.go' \
  '^(?!\s*//).*(?::Workload(?:Instance)?\s*\{[^}]*\b(?:id|workload_id)\s*:\s*\$|\b(?:w|i|inst|instance|workload)\.(?:id|workload_id)\s*(?:=|<>|IN)\s*\$)' \
  go/internal/query go/internal/reducer go/cmd
```

**27 lines across 16 files at `2b9cca96d`** (28 anchors —
`entity_workload_platform.go:69` carries two). `-P` is load-bearing: the
`^(?!\s*//)` comment guard needs PCRE2. That guard also suppresses four doc
comments describing the shape, so a comment-only edit to those lines does not
move the count. The three path arguments are load-bearing too: dropping them and
running over all of `go/` returns 35, and the extra 8 are write-path lines in
`storage/cypher/canonical.go` — including a `SET` assignment inside an upsert,
which is not a match predicate.

One representative per anchor kind, because the kinds break differently:

| Anchor kind | Representative | How a re-key breaks it |
| --- | --- | --- |
| Node-pattern property | `go/internal/query/entity_workload_handlers.go:29` | Matches nothing; the endpoint 404s. |
| Denormalized `i.workload_id` | `go/internal/query/compare.go:189`, `go/internal/query/workload_runtime_topology.go:91` | Covered by migration item 4; the second is under a query-plan pin. |
| Id/name conflated in one clause | `go/internal/query/entity.go:58` | The same parameter is tested against `w.name` **and** `w.id`, so after a re-key the name half still matches and the id half does not — the selector half-works, which is the worst shape to debug. |
| Inequality exclusion | `go/internal/query/impact_change_surface_legacy.go:126` | `impacted.id <> $target_id` stops excluding the start node, so it **appears in its own impact set** — a wrong answer with no error. |
| List membership | `go/internal/query/catalog_workload_environments.go:60,68,86` | Silent empty result. |

`entity.go:58` is in the 27 but is **dead on the production path**:
`serviceLookupWhereClause` has exactly two references in the tree, its own
declaration and `service_context_endpoint_test.go:107`. Live count is 26.

**A verified negative:** `go/internal/mcp/` contains no workload-id-anchored
Cypher at all. Its citations elsewhere in this document are argument
normalisation, not graph reads.

#### Anchors composed at runtime, invisible to the criterion

The criterion is a literal search, and these are assembled at run time, so no
count covers them. They are rows because each needs its own decision:

| Shape | Where | Note |
| --- | --- | --- |
| Property name from a Go variable | `go/internal/query/impact_change_surface_resolvers.go:113,131,142`, callers at `:29,32,41,42,80,83,88,93` | Builders carry no literal `id`; call sites carry no `$`. Neither end matches. |
| Label, variable and parameter all dynamic | `go/internal/query/impact_anchor_resolve.go:31,46-48` | **A generator, not a site** — it emits one anchor per label per property, and `Workload`/`WorkloadInstance` are in the label list. There is no count to state. |
| Label-less `impacted.id <> $target_id`, can bind a Workload | `go/internal/query/impact_change_surface_traversal.go:34`, `impact_change_surface_legacy.go:126` | `:34` matches unlabelled `(impacted)` with the label predicate at `:36`; the legacy statement names both labels inside `any(label IN labels(impacted) …)`. **`:36` is textually present and operationally inert** — see below. |
| Same `impacted.id <> $target_id` shape, but **`Repository`-anchored** | `go/internal/query/impact_change_surface_traversal.go:18`, `:42` | Both label `impacted` inline on the MATCH as `(impacted:Repository)`, so neither is label-less and neither names `Workload` or `WorkloadInstance`. `:18` is a `(start:Repository)<-[:DEPENDS_ON*]-(impacted:Repository)` traversal and cannot bind a workload at all. Listed only because they share the shape a reader would grep for. |
| Label-less by-id anchors that can resolve a Workload | `go/internal/query/infra_relationship_filter.go:88`, `go/internal/query/entity.go:283` | `MATCH (n) WHERE n.id = $entity_id` and similar. |

**On `:36` being inert.** `impact_change_surface_traversal.go:150-156` states that the
pinned NornicDB build "does not evaluate that clause position as a filter -- label tests
there are silently dropped and every reachable node comes back", and that combining the
`repo_id` and label predicates into one `WHERE` empties the traversal on the same build,
so no clause arrangement filters correctly server-side today. Enforcement therefore lives
in Go (`changeSurfaceImpactedLabels`, `changeSurfaceRowLabelAdmitted`, #6186). A re-key
must treat that row's label predicate as documentation of intent, not as a filter that
runs.

**What this row's history says about the table.** An earlier revision called all four
citations label-less and claimed each carried a `Workload`/`WorkloadInstance` predicate;
two of the four are `Repository`-anchored and carry neither. The criterion's count stayed
27 throughout and would have stayed 27 through the drift, because `impacted` is not in its
variable allowlist and these lines were never among the 27. So this table's failure mode is
**characterisation drift**, which no count detects — the second axis section 4.1's
enforcement rungs do not cover, and the reason this table is the one to re-read rather than
re-count.

So there is no single honest number for this category. "27 statically greppable
sites at `2b9cca96d`, plus the runtime-composed anchors above, one of which is a
generator" is the accurate statement, and saying so is stronger than picking one.

### 4.5 Reads that prefix-parse or decompose the identifier

Small enough to list, and the members are **not** homogeneous — a `HasPrefix`
test, a first-colon cut and an idempotent re-prefix each break differently — so
this is rows rather than a criterion. Enumerated by hand across `go/` **and the
console** — the table carries five console rows covering seven sites (two rows
cite two lines each), and an earlier revision's
`go/`-only scope line understated them.

| Site | Shape |
| --- | --- |
| `go/internal/query/catalog.go:213` | **Both** a parse and a construct: an idempotent re-prefix, `"workload:" + TrimPrefix(name, "workload:")`. |
| `go/internal/query/catalog.go:214,332` | Pure parses. |
| `go/internal/query/impact_change_surface_resolvers.go:102-108` | Construct guarded by a prefix test at `:104`; the construct itself is at `:107`. |
| `go/internal/query/entity_workload_context.go:280` | Prefix parse — `strings.TrimPrefix(selector, "workload:")`, the only `TrimPrefix` in the function. |
| `go/internal/query/entity_workload_context.go:286` | **Both.** `selector == normalized \|\| plainSelector == normalized \|\| selector == "workload:"+normalized` — the third arm constructs. An earlier revision filed this as a pure parse. |
| `go/internal/query/repository_read_model_summary.go:114` | Prefix parse. |
| `go/internal/query/content_reader_repository_catalog.go:107` | Prefix parse. |
| `go/internal/query/service_workload_resolution.go:137` | `HasPrefix(selector.ServiceName, "workload:")` gates whether an id-equality read fires at all — so it breaks twice over. |
| `go/internal/query/supply_chain_impact_path.go:145` | Prefix parse. |
| `go/internal/reducer/supply_chain_impact_match.go:147` | Prefix parse — `supplyChainWorkloadIDsFromPayload`. |
| `go/internal/query/supply_chain_impact_runtime_context_store.go:193` | The **query-side half of that pair**: `strings.HasPrefix(workloadID, "workload:")`, whose comment at `:184-188` says it mirrors the reducer's extraction. A re-key breaks both halves; an earlier revision listed only the reducer half. |
| `go/internal/query/entity_map_resolver.go:62`, `:135` | Both call `canonicalWorkloadIDCandidate(from)` (defined at `impact_change_surface_resolvers.go:102`) and append a prefix-constructed resolver when it differs from the input. |
| `normalizeQualifiedIdentifier` (now `go/internal/mcp/servicecontext/routes.go`) | `normalizeQualifiedIdentifier` cuts at the **first** colon and returns the *tail*, so a three-part id becomes `<repo_id>:<name>` — a different break from a `HasPrefix` test. It **is** applied to workload selectors in production: the service-context selectors in `go/internal/mcp/servicecontext/routes.go` and the `serviceContextRoute` adapter in `dispatch_service_selector.go`, and `go/internal/mcp/README.md` describes it as stripping the `workload:` prefix. |
| `canonicalWorkloadIdentifier` (now `go/internal/mcp/servicecontext/routes.go`) | `canonicalWorkloadIdentifier` also cuts at the first colon but returns the **whole value** when the head is `workload`, so it survives a re-key intact. Listed because it sits beside the previous row and an earlier revision cited it as the truncating one. |
| `apps/console/src/api/impactDeploymentGraph.ts:453`, `:455` | `startsWith("workload-instance:")` and `startsWith("workload:")`. Outside `go/`, which the enumeration above does not cover. |
| `apps/console/src/api/eshuGraphDeploymentTopology.ts:298,300` | `startsWith("workload:")`, deciding a column and a node kind. |
| `apps/console/src/pages/ExplorerPage.tsx:380` | `needle.startsWith("workload:")` short-circuits query rewriting. |
| `apps/console/src/api/exposureServiceSelection.ts:172` | **Strips**, not tests: `value.startsWith("workload:") ? value.slice("workload:".length) : value`. |
| `apps/console/src/api/eshuGraphRelationships.ts:192` | **Strips** too: `trimmed.slice(prefix.length)` after a case-insensitive `startsWith`. |

**Construct and parse are not disjoint** — `catalog.go:213`,
`impact_change_surface_resolvers.go:102-108` and `entity_workload_context.go:286`
are all three — which is one more reason this could not have been fixed by adding
rows to a flat "parse sites" list. Those hybrids stay here and are cross-referenced
from 4.1 rather than moved, because a reader auditing parse behaviour needs to see
them. Only `entity_workload_context.go:261`, a pure construction an earlier
revision filed here, actually moved.

`go/internal/query/entity_map_resolver.go:188` resolves by `{repo_id, name}` and would keep
working — **but do not skip that file on the strength of this line.** Its
`workload_instance` case at `:70-75` emits **three** resolvers, and the file also
holds the two `canonicalWorkloadIDCandidate` callers in the table above.

The signature is
`entityMapNodeResolverQuery(label, matchProperty, from, anchorProperty, rank, limit)`
(`:159-165`), so *match* property and *anchor* property are different parameters
and must not be conflated: the three resolvers differ in **match** property
(`:72` `id`, `:73` `workload_id`, `:74` `name`) while the **anchor** property is
`id` for all three. The rank-0 `id` and rank-1 `workload_id` resolvers match on
values the re-key moves and break; the rank-2 `name` resolver survives. Migration
item 4 states this correctly and is the version to trust if these two ever
disagree again.

Nothing decomposes a `workload-instance:<name>:<env>` identifier back into its
name and environment — not in `go/`, and not in the console either: none of the
console strippers below splits an instance id. That strict negative holds and is
verified rather than an unsearched gap.

The `go/`-only scope of the enumeration above, however, understated the console.
It is **seven** sites, in the table, and they are not all the same kind. Five
*test* the prefix, and a re-key that keeps the prefix leaves those working. Two
**strip** it — `exposureServiceSelection.ts:172` and `eshuGraphRelationships.ts:192`
— and stripping is a different break: a stripper hands downstream code a bare
name where a three-part id would yield `<repo_id>:<name>`, so it fails by
producing a plausible wrong value rather than by not matching. The console is
therefore in scope for the re-key in a way an earlier revision's single cited
line did not convey.

**The `reducer_workload_identity` subsystem needs a decision before
implementation** (section 8, question 3). Section 4.7 has the detail; do not
re-derive it here.

### 4.6 Derived keys that embed the identifier

Three members, three different consequences, each needing its own decision. This
category exists because none of these is an edge write or a parse site, so nobody
sweeping the earlier `section 4` would have looked for them — and two of the
three are the most consequential findings in this document.

| Derived key | Embeds | Consequence of a re-key |
| --- | --- | --- |
| `ServiceRuntimeEvidenceKey` — `runtime:<service_id>:<platform_kind>:<environment>:<workload_ref>` (`go/internal/reducer/service_materialization_runtime.go:101-103`, shape in its own doc comment at `:98-99`, built via `ServiceRuntimeEvidenceKeyFromIdentity` `:108-114`) | the WorkloadInstance id, as `WorkloadRef` (declared `:65-69`, set at `go/internal/reducer/service_runtime_instance_lookup.go:124,134`) | **Changes a durable persisted Postgres key**, and nothing retires the old row — see migration item 7. |
| `serviceRuntimeEvidenceIdentity` (`go/internal/reducer/service_materialization_runtime.go:121-128`, joined at `:127`) | the same `WorkloadRef`, hashed into the payload at `:140` | Flips every existing row to removed-plus-added rather than unchanged. |
| The retract statement's anchor (`go/internal/reducer/workload_materializer_retract_instances.go:35-38`) | `instance_id` | Retract with the **old** ids before the new ones exist, or the statement matches nothing. Its own comment at `:21` records that instance ids are not repository-namespaced, and `:17-18` already carries a worked re-key example. |

The reducer conflict-domain key at `go/internal/reducer/dependency_domain.go:101,116`
looks like a fourth member and is not; section 4.1 above records why the
production consequence does not exist.

### 4.7 Non-graph identity subsystems

One member, and it needs its own decision (open question 3).

**A second, non-graph identity subsystem also uses this prefix, and a graph-only
re-key would leave it inconsistent.** An earlier revision of this paragraph
attributed it to the wrong producer, fact kind, and field; the corrected reading:

- The `reducer_workload_identity` fact is written by
  `go/internal/reducer/workload_identity_writer.go:52`
  (`const workloadIdentityFactKind = "reducer_workload_identity"`), and its
  persisted field is **`entity_keys`** — a JSON *array* (`:163`), not a scalar.
  Both readers unnest it accordingly:
  `go/internal/query/repository_read_model_summary.go:96-97` and
  `go/internal/query/content_reader_repository_catalog.go:89` both unnest
  `payload->'entity_keys'` — the first with a comma join, the second with
  `CROSS JOIN`, each wrapping it in `coalesce(…, '[]'::jsonb)`. So the re-key
  surface here is the reducer writer's own key construction plus three SQL
  readers unnesting an array — not a collector field.
- `go/internal/collector/git_followup_facts.go` is a *different* fact. It emits kind
  `"shared_followup"` (`:58`) with a singular `entity_key` of
  `"workload:" + filepath.Base(repoPath)` (`:52`) — keyed on repo basename, and
  carrying `reducer_domain: "workload_identity"` (`:51`). A second envelope in the
  same file carries `reducer_domain: "workload_materialization"` (`:187`) with its
  own `entity_key` at `:188` — a different domain, so the two are not one thing
  and must not be cited as one.

Either way the prefix is surfaced through `entity_workload_context.go` as the
`materialization_status: "identity_only"` fallback (`:212`, `:269`) used
precisely when a workload has no materialized graph node, and the supply-chain
impact domain treats any `workload:`-prefixed key as workload-identity evidence.
Open question 3 and migration item 6 both turn on this, so an implementer
following the earlier wording would have started in the wrong package.

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
  now settled. It fails three independent tests for key material.

  **Plural**, and the type settles it before any fixture argument does:
  `DeploymentRepoIDs` is declared `[]string` at
  `go/internal/reducer/projection.go:39`. The corpus agrees — deployment-typed
  evidence comes from three separate fixtures, `helm_argocd_platform`,
  `kustomize-deployable-overlay` and `helm-umbrella-chart`, all pointing at the
  same `deployable-source` repo. (The first is underscored where its two
  siblings are hyphenated; all three are at
  `scripts/lib/golden-corpus-fixtures.sh:29,34,41`.) One committed snapshot note
  records a single-element resolved value, so the exact count in that fixture is
  unverified — but a slice is a slice, and a key cannot be built on a field whose
  cardinality is not one.

  **Unstable** (the
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
  "Deployment manifests for workload instance live in deployment repository"
  That is provenance, and the correlation rules keep deployment repos
  provenance-only.

- **"One workload or two" was largely a false premise.** In the golden corpus the
  split-repo family already materializes as *separately named* workloads —
  `workload:deployable-source` and `workload:deployable-config` — because names
  come from repo names, not manifest names. An earlier revision named a third,
  `kustomize-deployable-overlay`; that is a fixture name, not a workload. The
  snapshot's complete `workload:` set is `api-svc`, `claim-honesty-demo`,
  `deployable-config`, `deployable-source`, and `supply-chain-demo-db`, and its
  only instances are `deployable-source:prod` and `deployable-source:stage`. Their unity is carried
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

## 5a. Retract/rebuild proof

Run before any implementation — and now run, on a fresh single-purpose stack against the
pinned backend, with Neo4j 5.x community as the control and a settling loop on
every read.

**Result: relationship retraction does not work on the pinned build; node-level
`DETACH DELETE` does.**

**One trial per cell in both tables below, and that is enough here — but say so
rather than let a bare number imply more.** These are binary works/does-not-work
outcomes with a Neo4j control on every row and a settling loop on every read, not
latencies: a single sample distinguishes 1 → 0 from 1 → 1 in a way it could never
distinguish two timings. The settling loop is what earns it, and it is why the
"still 1 after 43 s" figures are quoted — the read was retried to a deadline
rather than taken once. Note also that this document records one earlier run on
this backend giving a confident wrong answer from read staleness (see below), so
a single unsettled read would not have been enough.

Ledger: `ledger:5385-retract-rebuild-relationship-inert` and
`ledger:5385-retract-rebuild-node-detach-delete` carry these outcomes, their trial
count and the two harness errors; the tables below are the readable form, not the
source of truth.

| Operation | Neo4j 5 | NornicDB (pinned) |
| --- | --- | --- |
| bare relationship `DELETE` | 1 → 0 | **1 → 1**, still 1 after 43 s |
| `retractSingleRepoRunsOnEdgesCypher` verbatim | 1 → 0 | **1 → 1**, still 1 after 46 s |
| node `DETACH DELETE` (instance retract) | node and all incident edges gone | node and all incident edges gone |

I made two harness errors and corrected them before trusting these numbers,
and both are worth recording because either would have produced a confident
wrong answer:

- The driver executes lazily. A write whose result is never consumed looks
  exactly like a backend ignoring the write. Caught by the Neo4j control
  failing identically.
- This backend has eventual-read consistency. An immediate count after a delete
  reads stale. A first run showed `RUNS_ON` surviving a `DETACH DELETE` — it had
  not; the same query returned zero moments later. The relationship-`DELETE`
  result was then re-checked with a settling loop, and that one is real.

**What this means for the migration.** A re-key's retract-and-rebuild cannot lean
on relationship retraction on this backend. What does work is node-level
`DETACH DELETE`, which `batchWorkloadInstanceRetractCypher` already uses and which
removes every incident edge — so instance-anchored families are carried by
deleting and rebuilding the instance node.

That statement is anchored on `instance_id`
(`workload_materializer_retract_instances.go:36`), and the instance id **changes**
under a re-key. So the migration must retract using the **old** ids before writing
the new ones. Retracting after the new ids exist finds nothing and leaves the old
instance nodes, with all their edges, orphaned under the previous key.

### Every family, measured both ways

The four families not incident to a `WorkloadInstance` were then measured
individually, each driven through its own production retract statement, with the
same settling discipline and Neo4j as the control. One trial per cell again, on
the same reasoning as above:

| Family | Relationship retract, Neo4j | Relationship retract, NornicDB | Workload `DETACH DELETE`, both |
| --- | --- | --- | --- |
| `DEFINES` | 1 → 0 | **1 → 1** | cleared |
| `DEPENDS_ON` | 1 → 0 | **1 → 1** | cleared |
| `RUNS_IN` | 1 → 0 | **1 → 1** | cleared |
| documentation `DOCUMENTS` | 1 → 0 | **1 → 1** | cleared |

**Every relationship retract is inert on the pinned backend. Node-level
`DETACH DELETE` clears every one of them, on both backends.**

### What the migration must therefore do

**Delete nodes, not relationships.** `DETACH DELETE` of the `Workload` node
removes all four families measured above; `DETACH DELETE` of each
`WorkloadInstance` node removes `INSTANCE_OF`, `RUNS_ON` and
`DEPLOYMENT_SOURCE`. Both work identically on Neo4j and on the pinned build — so
the migration is not betting on a backend fix.

One family in section 4.3 is **not** in the table above: `EXPOSES_ENDPOINT`,
which hangs off the `Workload` node. A `Workload` `DETACH DELETE` removes it too,
for the same reason it removes the other four — but that is **reasoned from
node-delete semantics, not measured**, and the probe did not exercise it. Treat
it as expected rather than proven, on the same footing this document gives every
other traced-but-not-executed claim. So "every family in section 4" holds only
with that caveat attached, and an earlier revision asserted it without one.

**Retract before writing the new ids, using the old ones.** Both retract
statements are anchored on the id (`workload_materializer_retract_instances.go:36`
for instances), and the id is what changes. Retract after the new nodes exist and
the statement finds nothing, leaving the old nodes and all their edges orphaned
under the previous key.

**Do not rely on the existing relationship retract statements for any part of
this.** They are correct Cypher and they work on Neo4j; they do nothing here.
That is a backend defect rather than a design flaw, but the migration has to be
written against the backend it runs on.

## 6. Migration

Every edge in section 4 is anchored on the old value, so old nodes must be
retracted and rebuilt rather than rewritten in place.

0. **Make the identifier type-safe, as step zero.** Section 4's list is
   hand-maintained and has been wrong three times: it missed
   `projection_helpers.go:110` through two revisions. Do not fix it by hand a
   fourth time.

   **An earlier revision of this item claimed text search *cannot* close this,
   and that claim is withdrawn.** It offered two grep shapes and then argued a
   third construction form defeats them — `GraphHandle{Kind: "workload", ID: …}`
   in one file, joined by `handle.Kind + ":" + handle.ID` in another, with no
   `workload:` substring in either.

   Those are not construction sites. The `ID` handed to `GraphHandle` is
   **already a full graph identifier**: `searchdocs/project_test.go:108` sets
   `WorkloadID: "workload:payments-api"` and asserts the handle carries it
   through unchanged. So that join produces `workload:workload:<name>` — a
   doubled prefix, and a *handle key*, not a workload id. It is pinned as such by
   `searchnornicdb/backend_test.go:196`, `searchnornicdb/benchmark_test.go:102`,
   and a committed evidence suite. `searchbench/retrieval_proof_test.go:210` goes further and
   **fails the suite** if a workload handle's id does not already start with
   `workload:` — the search layer requires a pre-built id rather than assembling
   one. `cli/hookpreflight/preflight.go:251` is a user-typed selector forwarded
   as a tool argument, not an identifier either.

   They are carriers, and forcing them through a `WorkloadID` would be wrong.
   A re-key touches them the way it touches every string carrier — reindex, and
   callers pass the new value — which section 6a already covers as tier 2.

   **The weaker argument that survives is still sufficient.** Grep does find the
   real construction sites. What it does not do is keep a *human-maintained list*
   correct: this document's own inventory missed `projection_helpers.go:110`
   through two revisions, and the withdrawn paragraph was itself incomplete — it
   named two files carrying the handle form, and the categories are not even
   the same shape: there are **three** non-test sites that build a
   `{Kind, ID}` pair with kind `workload` — two graph handles and one `Scope`
   (`searchdocs/project.go:283`,
   `searchdocs/semantic_context.go:52`, `cli/hookpreflight/preflight.go:251`) and
   a separate set that merely *joins or carries* one, of which
   `searchbench/evidence.go:354` is one among several. Giving a single number to
   two different categories is the same class of error as the inventory it is
   citing. The failure mode
   is not that the search cannot see the sites; it is that someone has to run the
   right search, read every hit, and transcribe them without error, repeatedly,
   as the tree moves. A type removes that step rather than making a search
   sharper.

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
     joiners; point at `searchdocs.GraphHandle` joined by
     `go/internal/searchbench/evidence.go:354` as the shape to audit by hand.
     Name that file: there is no `searchdocs.handleKey`, and four unrelated
     unexported functions share the name `handleKey`. A guard advertised as complete when it is not invites
     exactly the "trust it and stop looking" failure this step exists to
     prevent.

1. **Rebuild, do not rewrite.** `DETACH DELETE` every `Workload` and
   `WorkloadInstance` **node**, which removes their edges as a consequence, then
   re-project from facts. Do not retract the edges themselves — section 5a
   measured every relationship retract as inert on the pinned build, so an
   edge-first rebuild silently does nothing. At 40 nodes and 33 instances this is
   cheap; the reducer already owns a correct rebuild path.
2. **Scope both halves of the RUNS_ON pair — the upsert is the live one.**
   `canonicalRunsOnUpsertCypher` (`canonical_relationships.go:322-330`) is
   `MATCH (repo:Repository {id: row.repo_id})-[:DEFINES]->(w:Workload)` then
   `MATCH (i:WorkloadInstance)-[:INSTANCE_OF]->(w)`, with nothing scoping `i`, so
   one repository's platform attaches to every instance of a shared workload.
   This is the half section 2 mechanises and reproduces, and the half section 7
   calls the live leak. A repo-scoped id fixes it by construction: the first
   `MATCH` then reaches exactly one `w`, so the second reaches only that
   repository's instances. No separate predicate is needed.

   The retract half — `retractRepoRunsOnEdgesCypher` /
   `retractSingleRepoRunsOnEdgesCypher` (`canonical_relationships.go:352-364`,
   dispatched from `edge_writer_retract_repo.go:112,114`) — traverses `DEFINES`
   into a possibly-shared `Workload` and never scope the instance back to the
   retracting repository either,
   and the same repo-scoped key makes that traversal safe by construction. Worth
   fixing, but do not treat it as the leak.

   **Section 5a measured these retract statements as inert on the pinned build**: every
   relationship retract came back 1 → 1, a silent no-op, where Neo4j gives
   1 → 0. So scoping them fixes a latent hazard that will matter when the
   backend starts applying them; it does not fix anything observable today, and
   an implementer who scopes this and stops has fixed nothing. Section 5a's three
   standing instructions — delete nodes not relationships, retract with the old
   ids first, and do not rely on the relationship retracts for any part of this —
   govern the whole of this section and are stated there rather than repeated
   here.

   `mutations.go` needs no fix — it is unreachable (section 2) and its correct
   disposition is deletion under the repo's own dead-code precedent, separately
   from this work.
3. **Regenerate the golden artifacts** — 38 identifier values in the B-12
   snapshot and 1 in the cassettes, moved in the same change per the
   golden-corpus rules, plus 7 prose mentions in snapshot notes that
   regeneration will not touch (section 3.3).
4. **Re-key `WorkloadInstance.workload_id` in the same change.** The item most
   likely to be missed, and the only one that fails *silently*. It is a
   denormalized scalar copy of the workload id on every instance node, and **at
   least five** read paths filter on it directly:
   `go/internal/query/workload_runtime_topology.go:90-97`
   (`i.workload_id = $workload_id`), `go/internal/query/service_workload_resolution.go:249,284`
   (`w.id = i.workload_id`), `go/internal/query/compare.go:187-194`,
   `go/internal/query/entity_map_resolver.go:70-75` (the `workload_instance` case emits three
   resolvers; the rank-0 `id` and rank-1 `workload_id` ones both anchor on values
   the re-key moves, via `MATCH (n:WorkloadInstance {workload_id: $from})` at
   `:168`), and `go/internal/query/impact_change_surface_resolvers.go:93` (the phase-2
   alternate-identity resolver, `changeSurfaceWorkloadInstanceResolverQuery("workload_id", …)`).
   Re-key the node without these and topology goes blank, environment compare
   degrades to "unsupported", entity-map and change-surface resolution stop
   matching, and nothing errors anywhere.

   An earlier revision said three, and section 4 disposes of
   `entity_map_resolver.go` on the strength of one line (`:188` resolves by
   `{repo_id, name}` and would keep working) in a way that reads as "skip this
   file". That is true of that line and false of the file. Treat a count in this
   document as a floor until re-derived.

   A fourth site, `go/internal/query/impact_resource_investigation_reads.go:78`, only *projects*
   the scalar and uses it as the last fallback in
   `firstNonEmpty(resolved.id, workloadIDRaw, instanceID)` (`:151-161`), after a
   live `INSTANCE_OF` traversal that would return the correctly re-keyed id. It
   would most likely survive an unmigrated re-key, and its actual failure mode
   needs its own verification rather than being assumed to match the other three.

   **`workload_runtime_topology.go` is under a committed query-plan gate, and
   this item edits the thing the gate pins.**
   `go/internal/queryplan/testdata/hot-cypher.yaml:236-256` pins
   `fetchWorkloadRuntimeTopology` by `source_sha256`, declares
   `required_anchors: WorkloadInstance.workload_id` and `required_schema:
   workload_instance_workload_id` (the index at
   `go/internal/graph/schema_tables_indexes.go:176`), and carries this caveat
   verbatim: "The WorkloadInstance workload_id predicate must remain the textual
   traversal anchor; retained NornicDB evidence showed repository-first traversal
   was about 75 times slower for the same rows (issue #5272)." A repo-scoped key
   changes the value that predicate matches on. Re-keying it therefore needs the
   pin updated, the index checked against the new key shape, and the 75x finding
   re-proven rather than assumed to carry — this is a performance-contract
   dependency, not a string edit.

5. **Update the parse sites** in section 4, with a test per site. Two deserve
   naming: `go/internal/query/catalog.go:328-333` (`catalogWorkloadKey` merges catalog rows by
   *name only*, so split siblings silently vanish from `/catalog`) and
   `normalizeQualifiedIdentifier` (now `go/internal/mcp/servicecontext/routes.go`) (`normalizeQualifiedIdentifier` cuts at the FIRST
   colon, so a three-part id becomes `<repo>:<name>` and 404s).

   The loud breaks are safer and already have error types: name-selector surfaces
   go ambiguous for collision names —
   `go/internal/query/impact_trace_workload_selection.go:52-54` (`errAmbiguousTraceWorkloadSelector`)
   and `go/internal/query/service_workload_resolution.go:93-104` (`serviceWorkloadAmbiguousError`).
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
   which is the only thing that sees a cross-scope merge. Section 3.1 ran both
   graph-side detectors and both read zero, but under an ingestion regime that
   never produced a multi-repo scope — so those zeroes say nothing about mode two
   at all, in either sub-case. The in-process counter is what makes mode two
   visible; the graph-side one is what makes mode one visible.

   After the re-key both become **regression guards**: two repositories defining
   one workload should be structurally impossible, and either counter firing
   means the key leaked.

8. **Retire the old `service_evidence_key` rows, and do it with the old ids.**
   This is a Postgres migration, not a graph one, and section 4.6 is the only
   place it appears — it is neither an edge write nor a parse site, so a sweep of
   the graph surfaces will not find it.

   `ServiceRuntimeEvidenceKey` (`go/internal/reducer/service_materialization_runtime.go:101-103`,
   shape stated in its own doc comment at `:98-99`) builds
   `runtime:<service_id>:<platform_kind>:<environment>:<workload_ref>` via
   `ServiceRuntimeEvidenceKeyFromIdentity` (`:108-114`), and `WorkloadRef` is the
   `WorkloadInstance` id. So a re-key changes a **durable persisted key**, not an
   in-memory identity.

   Nothing retires the old one. `runtimeInstanceHasDurableIdentity` (`:150-155`)
   only drops empty refs, and the delta classifies by identity — so after a
   re-key the pre-re-key row looks **absent** rather than **retired**, and is
   left behind under the previous key with no tombstone path. That is the same
   failure section 5a warns about for instance nodes, reappearing in a table
   section 6 did not mention until now: retract with the old ids **before** the
   new ones exist, or the statement matches nothing and orphans the row.

   **The consequence is user-visible, not housekeeping.** `service_evidence_key` is
   half a primary key — `schema/data-plane/postgres/026_service_evidence_snapshots.sql:5`
   declares it and `:10` is `PRIMARY KEY (generation_id, service_evidence_key)`,
   indexed at `:17`; the same file is mirrored at
   `go/internal/storage/postgres/migrations/026_service_evidence_snapshots.sql` — and `go/internal/storage/postgres/service_changed_since_sql.go:57-79`
   uses it to classify added, updated, unchanged, **retired** and superseded
   between generations. That is published through the MCP tool
   `get_service_changed_since`, whose own description
   (`go/internal/mcp/freshness/tools.go`) promises "Retired and superseded are
   never collapsed into unchanged." That sentence also appears on the
   repository-scope definition in the same file, but that tool is keyed by
   stable fact key and never mentions `service_evidence_key`. Citing it would
   attribute this consequence to a tool the re-key does not touch. So the generation that re-keys reports every
   service's runtime evidence as retired-plus-added through a tool that guarantees
   the churn cannot be suppressed. Section 6a's consumer-breakage accounting should
   carry it too.

## 6a. What holds a `workload:<name>` identifier, measured

This was an open question. It is answered, and the answer is the largest cost in
section 7: **these identifiers are a public contract, in three tiers that fail
differently.**

**Tier 1 — loud, and alias-fixable.** `workload_id` is a path parameter on
`/api/v0/workloads/{workload_id}/context` and `/story`
(`openapi_paths_entities.go:107,134`; handler matches `w.id` exactly at
`entity_workload_handlers.go:19,29`, 404 on miss). It is a required body field on
`POST /api/v0/compare/environments` (`go/internal/query/compare.go:35,64`, `MATCH (w:Workload) WHERE
w.id = $workload_id` at `:160`, no name fallback). The CLI rejects a label
containing `:` (`isShareSafeLabel` at `go/internal/cli/opdigest/digest.go:244`,
reached via `normalizeScope` at `:200`, whose guard at `:223` is the call site;
note `:91` is the `Scope` doc comment, which lists
`workload:name` as a *valid* target rather than a rejection), so
`workload:<repo_id>:<name>`
would be unusable for `eshu report --scope` until that parser changes. These break
visibly, which is the right behaviour for a contract in transition.

**Tier 2 — silent, alias-fixable with work.** Every claim in this tier is
**traced through the source, not executed against a re-keyed graph** — unlike
tier 1, where the exact-match and 404-on-miss behaviour is visible in the handler
itself. The tracing is careful and I believe it, but the document holds section 2
to a measured standard and these are not measured.

The impact resolver tries id, then a
`workload:`-prefixed candidate, then name (`impact_change_surface_resolvers.go:80-108`),
so a stored prefixed handle matches none of the three after a re-key and resolves
empty. MCP's `normalizeQualifiedIdentifier` cuts at the first colon
(`normalizeQualifiedIdentifier` (now `go/internal/mcp/servicecontext/routes.go`)), turning a three-part id into `<repo_id>:<name>`.
Search documents persist `GraphHandles{Kind:"workload", ID}` in Postgres —
written at `go/internal/searchdocs/project.go:283`, matched back at
`storage/postgres/eshu_search_index.go:216,300-312` — and stay stale until
reindexed. The catalog read model *synthesizes* graph-shaped ids from the
separate `reducer_workload_identity` scheme (`go/internal/query/catalog.go:213`), so the Console is
handed ids that would 404 against `/workloads/{id}/context`.

**Tier 3 — silent, and NOT alias-fixable.** The **provider-asserted** wording
appears on **two** facts, both in the service-catalog schema and both consumed:

| Fact | Field | Consumed at |
| --- | --- | --- |
| `service_catalog.repository_link` | `sdk/go/factschema/servicecatalog/v1/repository_link.go:72` | `go/internal/reducer/service_catalog_correlation_index.go:306` |
| `service_catalog.entity` | `sdk/go/factschema/servicecatalog/v1/entity.go:60` | `go/internal/reducer/service_catalog_correlation_index.go:239` |

Both carry the identical comment, "a provider-asserted Eshu workload id,
mirroring ServiceID", and both reach the correlation index through
`stringPtrValue`. An earlier revision of this section said the wording "belongs
to one fact and one only" and named only `repository_link`, which understated
the tier-3 surface by half: a re-key has to carry both, and an out-of-tree
collector emitting `service_catalog.entity` is as affected as one emitting
`service_catalog.repository_link`.

An earlier revision also attributed it to
`sdk/go/factschema/aws/v1/attribute_shapes.go:56-63`, which does not contain the
phrase at all — `:45-49` calls those fields "collector-side
workload-correlation tags", close to the opposite characterization. The tier-3
argument below traces the AWS `Resource.Attributes` shape, so read it as resting
on that shape's own behaviour rather than on the `repository_link` wording.
`cloudResourceServiceAnchorDecisionForPayload`
(`aws_resource_service_anchor.go:121-139`) passes them straight through from
`Resource.Attributes` with no name-matching fallback. They reach an exact match in
`workload_cloud_relationship_writer.go:21`:

```cypher
MATCH (workload:Workload {id: row.workload_id})<-[:INSTANCE_OF]-(instance:WorkloadInstance)
```

A `MATCH` that misses is a silent no-op — the `USES` edge simply is not written.
**A read-side alias layer cannot cover this**, because the value originates
outside the repository. It needs a reducer-side resolution step, and the
ambiguity path it would use already exists: the
`ambiguous_anchor` literal at
`workload_cloud_relationship_materialization.go:233`, and the `default:` branch
that increments its counter at `:306-308`.

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
`{workload_id: "workload:api-svc"}` (`docs/public/getting-started/first-five-questions.md:67`),
and `docs/public/reference/http-api/catalog-workload-selection.md:58` tells readers that pasted `workload:...`
handles "remain canonical". The Console does all three, and in a section titled
"measured" these were the only claims carrying no citation:

- **Builds ids from names.** `apps/console/src/pages/ExplorerPage.tsx:384` returns
  `` `workload:${service.name}` ``, and
  `apps/console/src/api/eshuGraphDeploymentModel.ts:49` falls back to the same
  shape when neither `trace.workload_id` nor `context.id` is set.
- **Puts them in bookmarkable URLs.**
  `apps/console/src/pages/ImpactDeploymentSummary.tsx:108` links to
  `` `/workspace/services/${encodeURIComponent(trace.workloadId)}` ``.
- **Prompts operators to paste one.**
  `apps/console/src/pages/ExposureServiceSelector.tsx:93` is the placeholder
  "Search authorized services or paste `workload:…`".

All four were handed to me as bare filenames with no directory. I guessed
`components/` for two of them, which is wrong — they are under `pages/` — and
then recorded that as an error in the values I had been given. It was not; the
error was mine. Corrected here because a wrong correction in the record
miscalibrates how the next list gets treated, and because this is the reason the
document cites by file and line rather than by description.

Migration must therefore add, beyond section 6: a read-side resolution layer for
tiers 1 and 2 that fails closed on an ambiguous legacy name, a reducer-side
resolution step for tier 3, a search reindex, and doc updates in lockstep.

## 7. Recommendation and cost

**Option C, taken now rather than later. There is no separable quick fix.**

An earlier revision recommended shipping a `mutations.go` fix immediately and
independently. That recommendation is withdrawn: the path is unreachable
(section 2). The live leak is the RUNS_ON **upsert** — `canonicalRunsOnUpsertCypher`
traverses `DEFINES` then `INSTANCE_OF` with nothing scoping the instance, which
is the half section 2 mechanises and reproduces. The retract half is a latent
hazard rather than a live leak: section 5a measured every relationship retract as
inert on the pinned build, so scoping it fixes something that will matter when
the backend starts applying those statements, not something happening today.
Either way, scoping needs to know which repository owns a workload — so both ride
with the re-key rather than ahead of it.

The case for now is section 3.2: cost scales with the workload population, which
is 40 nodes, 33 instances, and 39 golden identifier values today. **Both failure modes are
silent.** Mode one merges — one node, ownership reassigned to whoever wrote last,
cross-repo environments. Mode two drops — the losing repository's workload row
discarded, so the surviving node carries someone else's kind, classification,
confidence and provenance, no `DEFINES` edge for the repository that lost, and
where environments differ, instances owned by a repository with no edge to the
workload they hang off. Neither errors and neither increments anything.

The case for waiting is that the corpus detectors read zero — with the scoping
section 3.1 puts on that zero. They cover mode one. They say nothing about mode
two in either sub-case, because the corpus was ingested through git sync and the
multi-repo scope that mode two requires never occurred in it. And this is a
schema-risk change with cassette and B-12 impact.

| Item | Estimate |
| --- | --- |
| Key change | Two format strings, once the constructors are extracted (step zero). Trivial in isolation. |
| Query-plan gate | `fetchWorkloadRuntimeTopology` is pinned by `source_sha256` with `WorkloadInstance.workload_id` as a required anchor and a retained 75x-regression caveat. Re-proving it is **not** trivial and was unpriced until now. |
| Edge/parse-site sweep | The section 4 inventory, now including a non-graph subsystem. **Moderate-to-large, and the main risk.** |
| RUNS_ON scoping | Small once the key is decided; cannot land before it. |
| Golden regeneration | 39 identifier values across the snapshot and one cassette, plus 7 prose mentions a human updates. |
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
   written by `workload_identity_writer.go` into an `entity_keys` array, is the
   documented fallback for an unmaterialized workload, and a graph-only re-key
   leaves the two schemes disagreeing. (The repo-basename key an earlier revision
   attributed here belongs to the separate `shared_followup` fact; section 4 has
   the split.)
4. **Now or later for the re-key?** Section 7 gives both cases. There is no
   separable timing decision beside it: the RUNS_ON scoping fix needs the
   identity answer, so it rides with the re-key either way.

## 9. Sign-off request

Before any code is written I need:

- **A decision on Option C over A**, and on timing.
- **Confirmation of the recommended answer to question 1** — the defining
  repository, `WorkloadCandidate.RepoID`. It determines the key, and everything
  else in this design follows from it.
- **A decision on question 3**, the non-graph identity subsystem.

There is no longer an independent fix to approve. An earlier revision asked to fix
`mutations.go:119` on its own; that path is unreachable (section 2). A later one
called the RUNS_ON retract "the real leak", which section 5a disproved — that
retract is a silent no-op, not an over-broad delete. What remains is the cross-repo
merge itself, which the name-only key causes directly and which no retract fix
reaches. Fixing it needs the identity answer, so it belongs to this
issue rather than beside it.

No production code has been written for this issue and none will be until the
above is settled. The probe files are throwaway and are not in this branch.
