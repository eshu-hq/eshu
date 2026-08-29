# #6176 — the grouped semantic retract is fixed above v1.1.11, not on it

#6176 proposes removing `SemanticEntityWriter.WithSequentialRetract()` now that
#5323 is closed. #5323 proved the grouped `ExecuteWrite` delta-retract applies
correctly on NornicDB 1.2.1 and 1.2.2, with a 1.1.9 control that still fails.

This record adds the version #5323 did not test: **v1.1.11**, the image
`deploy/helm/eshu/values.yaml` pins and which
`docs/internal/evidence/5152-grouped-retract-underapply.md` calls "the pinned
production NornicDB". The grouped retract still under-applies there.

Scope that claim carefully, because the exposure is narrower than the headline
suggests and an earlier draft of this file overstated it. The workaround is
load-bearing **for the grouped-writes opt-in on the chart-pinned v1.1.11, not
for the default configuration** — see "What this means for #6176" below, where
the default is shown to route around grouped dispatch entirely, making the
removal a no-op there.

It is also narrower than the chart suggests. The operator reports running
`v1.2.3` (which reports `1.2.2` internally), and the grouped retract passes
20/20 on 1.2.2. So on the backend actually deployed the floor is already met;
what is stale is the committed chart pin, three patch versions behind at
v1.1.11. Anyone deploying from this repository as committed still gets the
backend where the grouped retract under-applies.

Root-Cause Evidence: on `timothyswt/nornicdb-cpu-bge@sha256:51b6174a` (reports
version 1.1.11), the production `SemanticEntityWriter` driven without
`WithSequentialRetract` left the retracted `Variable` node behind in 20 of 20
runs (`gen2: in-scope Variable retracted: count = 1, want 0`). The same writer
with `WithSequentialRetract` removed it in 20 of 20 runs on the same container,
and the same grouped shape passed 20 of 20 on 1.2.1 and on 1.2.2. The failure
therefore tracks the backend version, not the test.

## The shim

The existing live regression
`go/internal/replay/offlinetier/delta_tier_reducer_semantic_variable_retract_live_test.go`
(`TestReducerSemanticVariableRetractGraphTruth`), copied with symbols renamed
and exactly one line deleted — the `.WithSequentialRetract()` call. Nothing else
differs, which is the same shim shape #5323 used.

`liveExecutor` implements `cypher.GroupExecutor` (`executor_test.go`), so the
copy really routes the gen2 retract through `ExecuteGroup`. Without that,
`SemanticEntityWriter` falls back to a per-statement loop and a "grouped" test
passes vacuously — the shim asserts the interface directly so it cannot drift
back into that hole unnoticed.

## Results

Every cell is 20 runs, measured on this machine on the date of this commit.

| NornicDB build | Reported version | Where it is pinned | Sequential (today) | Grouped (#6176) |
| --- | --- | --- | --- | --- |
| `eshu-nornicdb-pr290:3722b483c02c` | 1.2.1 | `docker-compose.yaml` | not re-run | **PASS 20/20** |
| `timothyswt/nornicdb-cpu-bge:v1.2.3` | 1.2.2 | nothing | PASS 20/20 | **PASS 20/20** |
| `timothyswt/nornicdb-cpu-bge@sha256:51b6174a` | 1.1.11 | `deploy/helm/eshu/values.yaml` | PASS 20/20 | **FAIL 20/20** |

The v1.1.11 row is the whole finding. The sequential column passing beside it is
what makes it a version defect rather than a broken test: the same container,
the same Cypher, the same run — only the dispatch route differs.

`timothyswt/nornicdb-cpu-bge:v1.2.3` reports `1.2.2` internally. Pin any bump by
digest rather than by that tag.

## Commands

Containers started from the images above with the `docker-compose.yaml` NornicDB
environment (`NORNICDB_ASYNC_WRITES_ENABLED=false`, embeddings and search off),
on ports 17699 (1.2.1), 17695 (v1.2.3 tag), and 17697 (v1.1.11).

The grouped column was measured with a throwaway probe that is **not committed**,
so there is no `-run` target in this repository that reproduces it. An earlier
draft of this file printed a `go test -run 'TestIssue6176GroupedSemanticRetract'`
command; that symbol exists nowhere but in this document, and a `-run` filter
matching nothing exits 0 — the exact false green the note below warns about. The
command has been removed rather than left to mislead.

Reconstruct the probe like this. It is four mechanical steps, and the point of
writing them down is that the result is only meaningful if the copy is exact:

1. Copy `go/internal/replay/offlinetier/delta_tier_reducer_semantic_variable_retract_live_test.go`.
2. Delete the single `.WithSequentialRetract()` call from the writer chain.
3. Rename the test function and the `sv*` fixture constants (marker, repo, both
   file paths, both uids) so the copy cannot collide with the original — both
   write `Variable` nodes keyed by uid into one shared graph, and a shared scope
   lets either cleanup delete the other's fixture mid-run.
4. Keep everything else byte-identical. The claim under test is that the
   dispatch route alone decides the outcome, so any second difference voids it.

Then run it against each backend:

```bash
cd go
ESHU_REPLAY_TIER_LIVE=1 ESHU_GRAPH_BACKEND=nornicdb ESHU_NEO4J_DATABASE=nornic \
NEO4J_URI=bolt://localhost:17697 NEO4J_USERNAME=neo4j NEO4J_PASSWORD=nornicdb \
go test ./internal/replay/offlinetier/ \
  -run '<your copied test name>' -count=20 -v; echo $?
# v1.1.11: exit 1, 20 FAIL, "gen2: in-scope Variable retracted: count = 1, want 0"
# 1.2.1 and 1.2.2 on the same command: exit 0, 20 PASS
```

The probe is not committed here on purpose. `go/internal/replay/offlinetier/AGENTS.md`
requires this tier's writers to be driven through
`storage/nornicdb.PhaseGroupExecutor`, which exposes `ExecutePhaseGroup` and
deliberately NOT `ExecuteGroup`, because the full-atomic `GroupExecutor` route is
the Neo4j path rather than production NornicDB (the #4019 bug class). A committed
test that drove `GroupExecutor` in this package would contradict that invariant.
What the probe measures is therefore the grouped-writes OPT-IN route
(`TimeoutExecutor`, which does implement `ExecuteGroup`), not the replay tier's
own wiring and not the reducer default.

Exit codes captured directly, not after a pipe. Pass and fail counts came from
counting `--- PASS:` / `--- FAIL:` lines in the captured output rather than
reading the summary, because a `-run` filter that matches nothing also exits 0.

## What this means for #6176

`ESHU_NORNICDB_CANONICAL_GROUPED_WRITES` defaults to false, and with it unset the
NornicDB semantic executor is `ExecuteOnlyExecutor`, which hides `GroupExecutor`
entirely — so the retract already runs one statement at a time and
`WithSequentialRetract` changes nothing. The exposure is the documented opt-in:
an operator on the Helm chart's own v1.1.11 who turns grouped writes on. Today
the workaround protects them. Removing it leaves retracted semantic nodes
BEHIND in the graph -- the observed `count = 1, want 0` above, a DETACH DELETE
that under-applied inside the grouped transaction. It is a failed retraction,
not a missing write; describing it as "dropping" nodes reverses the observed
graph state and would send the next diagnosis hunting for absent writes.

So #6176 step 4 ("pin the floor") is not paperwork to do alongside the removal.
It is a prerequisite with a number attached: the floor is **1.2.1**, and the
Helm chart is three patch versions below it.

> **State at diagnosis.** The rest of this section describes the tree as it
> stood when this was written, and its present tense is historical. Order 1
> below is the one that was taken: #6313 merged as `a281fad7523b` and the chart
> now pins `v1.2.3` by digest. See "The ordering constraint ... is now
> SATISFIED" below.

Two orders that work:

1. Bump `deploy/helm/eshu/values.yaml` to a digest reporting 1.2.1 or newer,
   which also closes a drift that already exists: the operator reports running
   `v1.2.3` while the committed chart still pins `v1.1.11@sha256:51b6174a`, and
   a dozen production comments under `go/internal/storage/cypher/` still cite
   v1.1.11 as "the pinned NornicDB image". Pin the bump BY DIGEST -- the
   `v1.2.3` tag reports `1.2.2` internally, so the tag name is not the version.
   Then
   re-run the compose/Helm parity and conformance proofs on that artifact, write
   the supported-version floor where operators see it, and then remove the
   workaround.
2. Keep the workaround and gate the grouped retract behind a backend-capability
   flag, the way `nornicdb.capabilities.relationshipMergePropertyIdentity`
   already gates NornicDB#290. Deployments on 1.2.1+ get the atomic
   retract→upsert; v1.1.11 keeps the split.

Option 1 is a compatibility narrowing and the owner's call. Option 2 buys the
atomicity back without dropping v1.1.11, at the cost of one more capability
flag. Either way the removal must not land before the floor moves, and no code
was changed for #6176 on this branch.

No-Regression Evidence: no production code changed. The measurement above is a
throwaway copy of an existing live regression test with one line deleted, run
against three NornicDB builds and then discarded; the shipped dispatch route,
its Cypher, and its statement metadata are untouched, so there is nothing on the
touched path to regress.

No-Observability-Change: no runtime code changed, so the semantic writer emits
exactly the per-statement execution telemetry it emitted before. The signal an
operator would use to see this defect is unchanged, and is the reason the defect
is silent: an under-applying grouped DELETE reports success.

## Outcome

The removal landed. The owner settled the compatibility question this record
left open: v1.2.3 — the build the deployment runs, self-reporting `1.2.2` — is
the supported backend, and it is above the 1.2.1 floor measured above. Option 1
without the capability flag, in other words, with the version decision made
rather than inferred.

`WithSequentialRetract` and its plumbing are gone, and the grouped retract was
re-measured on that build before the deletion: 20 of 20 PASS at `-count=20`,
exit 0, with the live executor asserted to implement `cypher.GroupExecutor` so
the run cannot pass through the per-statement fallback. Details, including both
mutation proofs, are in
`go/internal/storage/cypher/evidence-6176-semantic-retract-regrouped.md`.

**The ordering constraint this record raised is now SATISFIED.** #6313 merged
as `a281fad7523b` and `deploy/helm/eshu/values.yaml:1110` pins
`v1.2.3@sha256:4dfa887d990bf0b536693830830e34351c036716b0fe6dc957e1a3680e9f3c74`.
This branch is rebased onto that commit (`git merge-base --is-ancestor
a281fad7523b HEAD` exits 0), so the removal ships together with the backend it
was measured against rather than ahead of it.

Kept here because the reasoning is why the ordering mattered: while the chart
pinned `v1.1.11@sha256:51b6174a`, a deployment made from this repository landed
on the backend where the grouped retract under-applies, and would have been
exposed if an operator turned grouped writes on. GitHub enforces no cross-PR
merge order, so the sequencing was held by hand and then checked rather than
assumed.

One thing this record flagged IS still open: the other eleven v1.1.11-era
workaround classes under `go/internal/storage/cypher/` remain unmeasured on
1.2.2 — only the semantic `Variable` grouped delta-retract has been. That does
not block this removal, which is scoped to the class it measured, but it is not
closed either.

The exposure that motivated the ordering was the grouped-writes opt-in only —
with `ESHU_NORNICDB_CANONICAL_GROUPED_WRITES` unset the NornicDB semantic
executor is `ExecuteOnlyExecutor`, which hides `GroupExecutor`, and
`go/cmd/reducer/AGENTS.md` documents that opt-in as conformance-only rather than
a production configuration.
