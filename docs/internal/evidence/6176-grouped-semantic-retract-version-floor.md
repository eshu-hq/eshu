# #6176 — the grouped semantic retract is fixed above v1.1.11, not on it

#6176 proposes removing `SemanticEntityWriter.WithSequentialRetract()` now that
#5323 is closed. #5323 proved the grouped `ExecuteWrite` delta-retract applies
correctly on NornicDB 1.2.1 and 1.2.2, with a 1.1.9 control that still fails.

This record adds the version #5323 did not test: **v1.1.11**, the image the Helm
chart pins and which `docs/internal/evidence/5152-grouped-retract-underapply.md`
calls "the pinned production NornicDB". The grouped retract still under-applies
there, so the workaround is still load-bearing on Eshu's deployed default and
the removal is gated on moving the Helm pin first.

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

```bash
cd go
ESHU_REPLAY_TIER_LIVE=1 ESHU_GRAPH_BACKEND=nornicdb ESHU_NEO4J_DATABASE=nornic \
NEO4J_URI=bolt://localhost:17697 NEO4J_USERNAME=neo4j NEO4J_PASSWORD=nornicdb \
go test ./internal/replay/offlinetier/ \
  -run 'TestIssue6176GroupedSemanticRetract' -count=20 -v; echo $?
# v1.1.11: exit 1, 20 FAIL, "count = 1, want 0"
# 1.2.1 and 1.2.2 on the same command: exit 0, 20 PASS
```

Exit codes captured directly, not after a pipe. Pass and fail counts came from
counting `--- PASS:` / `--- FAIL:` lines in the captured output rather than
reading the summary, because a `-run` filter that matches nothing also exits 0.

## What this means for #6176

`ESHU_NORNICDB_CANONICAL_GROUPED_WRITES` defaults to false, and with it unset the
NornicDB semantic executor is `ExecuteOnlyExecutor`, which hides `GroupExecutor`
entirely — so the retract already runs one statement at a time and
`WithSequentialRetract` changes nothing. The exposure is the documented opt-in:
an operator on the Helm chart's own v1.1.11 who turns grouped writes on. Today
the workaround protects them. Removing it drops semantic nodes silently, which
is the #4367 symptom the flag was added for.

So #6176 step 4 ("pin the floor") is not paperwork to do alongside the removal.
It is a prerequisite with a number attached: the floor is **1.2.1**, and the
Helm chart is three patch versions below it. Two orders that work:

1. Bump `deploy/helm/eshu/values.yaml` to a digest reporting 1.2.1 or newer,
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
