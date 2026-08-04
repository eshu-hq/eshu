# #5770 later-generation CI artifact correlation

## What was broken

Root-Cause Evidence: `buildCICDRunCorrelationReducerIntent` selected only
`ci.run`, while `CICDRunCorrelationHandler.Handle` loaded facts through an exact
scope-and-generation predicate. A later artifact therefore produced no intent,
and even a forced handler call had no earlier run anchor, so the classifier
wrote zero decisions.

The CI/CD collector can observe a run in one scope generation and its artifact
in a later generation. The projector only queued `ci_cd_run_correlation` when a
generation contained `ci.run`, and the reducer only read the triggering
generation. A later artifact was never reconsidered, so it could not add the
digest, repository, or commit evidence needed to upgrade the earlier run.

Queuing the artifact alone was not enough. Reducer reads are fenced to the
active generation, so writing only the affected run would hide every unaffected
run from the newly active correlation snapshot.

## Chosen behavior

A generation containing any `ci.run` keeps the existing full-snapshot behavior.
A generation containing `ci.artifact` but no run is a domain patch:

1. Extract the artifact's exact provider, run ID, and run-attempt keys.
2. For a payload-empty artifact tombstone, recover its provider/run/attempt
   routing identity from the latest payload-bearing predecessor with the same
   opaque stable fact key. Missing, blank, or pruned identity fails the intent
   closed instead of preserving a stale exact decision.
3. Load every latest retained live run from older successful generations in the
   same scope, plus run-scoped facts for those keys. Also reload the latest live
   `ci.workflow_image_evidence` rows for each recovered repository; the existing
   classifier still chooses exact commit evidence over repository fallback.
4. Remove retained artifacts superseded by a current live artifact, and remove
   the exact retained artifact named by a tombstone. The payload-bearing row
   used to route a tombstone is never classified as live evidence, and the
   tombstone control row is not quarantined as malformed input.
5. Recompute the complete bounded source snapshot and write it into the artifact
   generation. No prior derived correlation result is copied, so queue
   supersession of an unpublished predecessor cannot drop unaffected runs.

Deployment events do not carry a run ID; they join to runs by commit SHA. After
the history read recovers a run, it loads retained deployment events for that
commit and repository. The normal classifier then reselects the environment and
its exact evidence fact. It does not copy old artifact or image-identity links
from the prior decision. Repository ID is optional on both facts: a matching
commit remains sufficient when either side omits it, while two populated,
different repositories do not join.

A post-publication regression reproduced a retained artifact whose old digest
still matched an active image identity. Before filtering superseded artifacts,
the handler returned that old digest before examining the current artifact.
The production-path regression now requires the current digest and image, and
a focused edge test proves an empty current digest does not resurrect the old
one.

The result summary counts the complete rebuilt source snapshot as `evaluated`;
`preserved` is zero because the handler no longer copies prior derived rows.
Outcome counts continue to describe the complete snapshot written.

```text
go test ./internal/reducer \
  -run '^TestCICDRunCorrelationHandlerRebuildsCompleteSourceSnapshotForLaterArtifact$' \
  -count=1

before: ArtifactDigest = "sha256:bbbb...", want current "sha256:cccc..."
after:  ok github.com/eshu-hq/eshu/go/internal/reducer

ESHU_POSTGRES_DSN=<local-test-dsn> go test ./internal/storage/postgres \
  -run '^TestCICDRunCorrelationArtifactPatchRebuildsUnpublishedPredecessor$' \
  -count=1

before: active generation run IDs = ["run-a"], want ["run-a", "run-b"]
after:  ok github.com/eshu-hq/eshu/go/internal/storage/postgres
```

Normal historical selection ranks a tombstone before filtering it, so a
retracted run or artifact cannot return as live evidence through an older row.
For a requested payload-empty artifact tombstone stable key only, the query
separately recovers the latest older payload-bearing artifact as routing
identity. The reducer removes that exact stable key before classification, so
the routing row cannot become evidence. The history query rejects more than
12,000 facts, and the handler rejects more than 1,000 rebuilt decisions. A
storage adapter that lacks the patch read fails the reducer intent instead of
acknowledging an empty result.

The two post-publication review regressions failed first: the storage query did
not contain a workflow-image history lane, and a payload-empty artifact
tombstone left the preceding exact artifact decision unchanged while also
emitting a false `input_invalid` quarantine. The corrected path keeps workflow
evidence tombstone-aware and repository-scoped, skips intervening empty
tombstones only for routing identity, and retracts the retired artifact from the
recomputed decision.

Every decision is rebuilt from current retained source evidence, so evidence
fact IDs remain resolvable for the same retention window as their source facts
and cannot silently depend on an unpublished derived generation.

## Theory and performance checks

Performance Evidence: PostgreSQL 16 processed the same one-scope, 25-generation
input before and after the retained-history query rewrite: 216,000 retained step
facts, 90 patch keys, 1,000 prior decisions, and 1,000 terminal output rows. The
full handler fell from a 91.586-second baseline to 0.740 seconds after ranking
the retained same-scope facts once. A later correctness review changed the
production path from prior-decision overlay to a full source-snapshot rebuild;
that final shape has its own measurement below and is not compared as a speedup.
Existing reducer and Postgres duration metrics retain operator visibility.

The performance question was whether the artifact-patch handler could remain
bounded on the existing fact indexes, or whether it needed a new JSON
expression index. The first production query joined every retained fact to the
requested run keys before ranking. The full handler disproved that design on a
representative same-scope backlog: almost all of its 91.586 seconds were spent
in the history read.

The replacement theory ranks the retained same-scope CI facts once, then
filters the latest rows to the requested keys. A test-only query selected the
expected 9,090 facts in 559.362 ms before the production SQL was changed.

| Comparable query-rewrite measurement | Before | After |
| --- | ---: | ---: |
| Total handler | 91.586 s | 0.740 s |
| Historical evidence read | 91.524 s | 0.679 s |
| Previous snapshot read | 7.990 ms | 6.238 ms |
| Durable writer | 43.347 ms | 44.719 ms |
| Result | Failed 5 s budget | Passed 5 s budget |

Both runs used the same PostgreSQL 16 input: one scope, 25 generations,
216,000 retained step facts, 90 artifact patch keys, 1,000 prior decisions,
and a 1,000-decision output. The passing run wrote 1,000 distinct fact IDs and
patched exactly 90 decisions. This comparison establishes that the original
query shape was unacceptable and that the shipped handler meets the local
scale budget; it is not a general product speedup claim.

The final post-review proof kept the same 216,000 retained step rows and
1,000-decision output, added 90 payload-empty current artifact tombstones, 90
older payload-bearing artifact identities behind intervening tombstones, and 90
retained same-repository workflow-image rows, and removed all seeded prior
correlation decisions. The fixture instead carries 1,000 retained `ci.run`
source rows, so every output must be rebuilt even though the current generation
patches only 90 runs. The complete handler took 0.914404208 seconds: 0.858194417
seconds for retained source history and 0.045260625 seconds for the writer. It
remained inside the five-second budget without restoring the global stable-key
index removed in migration 049. This final measurement is a correctness and
no-regression proof, not a direct speedup comparison with the prior overlay
fixture.

Other candidates were rejected before implementation:

| Candidate | Representative result | Accuracy | Disposition |
| --- | ---: | --- | --- |
| Broad provider/run/attempt expression index | 101 facts in 0.170 ms on a 92,500-row corpus | Passed | Rejected. The 9,544 kB index was about three times the existing 3,288 kB index and would add write cost to the highest-volume fact table. |
| Recompute only the artifact's run | Fastest write set | Failed | Rejected because the active-generation fence would make every unaffected prior run disappear. |
| Copy or search backward for a derived snapshot | Bounded lookup | Failed | Rejected because reducer queue supersession can leave the predecessor unpublished; an empty read is not proof of an authoritative empty source snapshot. |

## Test-first and live proof

The first focused test run failed for both missing behaviors:

```text
go test ./internal/projector ./internal/reducer \
  -run 'TestBuildProjectionQueuesCICDRunCorrelationIntentForArtifactOnlyGeneration|TestCICDRunCorrelationHandlerRebuildsCompleteSourceSnapshotForLaterArtifact' \
  -count=1

artifact-only intent: missing
artifact patch decisions: wrote 0
exit 1
```

After the implementation, the focused projector, reducer, and Postgres package
tests passed. A real PostgreSQL 16 regression then exercised the storage query,
handler, and durable write together:

```text
ESHU_POSTGRES_DSN=<local-test-dsn> \
  go test ./internal/storage/postgres \
  -run 'TestCICDRunCorrelationArtifactPatch(AgainstRealPostgres|RebuildsUnpublishedPredecessor)' \
  -count=1 -v

--- PASS: TestCICDRunCorrelationArtifactPatchAgainstRealPostgres (0.26s)
--- PASS: TestCICDRunCorrelationArtifactPatchRebuildsUnpublishedPredecessor (0.21s)
ok github.com/eshu-hq/eshu/go/internal/storage/postgres 3.685s
exit 0
```

`TestCICDRunCorrelationArtifactPatchRebuildsUnpublishedPredecessor` exercises
the production queue shape directly: generation 1 contains runs A and B with a
pending correlation item, generation 2 activates with an artifact only for A,
the claim supersession sweep terminalizes generation 1 before it publishes any
correlation facts, and generation 2 is claimed. The pre-fix handler then wrote
only A; the rebuilt source snapshot writes both A and B. This separates the
source-of-truth guarantee from queue timing and makes retries idempotent against
the same retained facts.

That regression proves a generation-three artifact can recover its
generation-one run and rebuild every unaffected latest retained run into the
target generation without reading prior derived decisions. It also proves
failed and future generations are excluded, distinct run attempts remain
distinct, and a payload-empty tombstone suppresses an older live fact. It also
covers both optional-repository fallbacks: a run without
`repository_id` can attach a repository-scoped deployment event, and a
repository-scoped run can attach an event that omits `repository_id`. Replaying
the same reducer intent writes the same fact IDs and payloads without duplicate
rows. It now also proves retained workflow-image evidence is loaded by
repository without resurrecting a tombstoned workflow row, and that a repeated
payload-empty artifact tombstone can route through its older payload-bearing
identity, overwrite the stale exact decision, remove the artifact/image
evidence, and avoid a false quarantine.

The opt-in scale regression runs the shipped handler, retained source read,
1,000-decision rebuild, and durable writer:

```text
manifest scopes=1 generations=25 retained_step_rows=216000
live_artifact_keys=90 tombstone_keys=90 workflow_rows=90
prior_decisions=0 output_decisions=1000 duration=914.404208ms
history=858.194417ms writer=45.260625ms budget=5s
exit 0
```

The active image-identity loader returns one envelope per evidence support, not
one envelope per canonical image. The delayed GitHub artifact has no active
same-generation run from which the image reducer can derive build provenance,
so repository narrowing alone cannot select it. A focused red test reproduced
the resulting false ambiguity with two supports that named the same non-empty
digest-qualified image reference. The CI/CD index now groups those agreeing
supports as one identity, unions their build-provenance repositories, and keeps
every support fact ID as evidence. The existing two-different-image-reference
case remains ambiguous.

The credential-free B-7 run keeps the older container-image identity proof
independent through a same-generation GitLab run/artifact pair, while GitHub
Actions runs 5150 and 5151 share generation 1 and only run 5150 receives an
artifact in generation 2. The B-12 query requires one exact object for 5150 and
a separate one-row derived object for unaffected 5151, so the corpus cannot stay
green if the active generation drops its sibling:

```text
bash scripts/verify-golden-corpus-gate.sh

cassette facts settled: 18 collector sources, 33 scope generations
drains: 0 residual work, 0 nonterminal shared intents, 0 nonterminal completion events
mcp:list_ci_cd_run_correlations: 1 result
mcp:list_container_image_identities: 1 result
summary: 522 pass, 0 required-fail, 1 advisory-warn
pipeline wall time: 126s (blocking ceiling 1800s)
exit 0
```

The advisory was `phase_maintenance_drains`: 27 seconds against its 19-second
advisory ceiling. It did not affect the blocking pipeline budget or any truth,
drain, replay, graph, HTTP, or MCP assertion.

## Observability

No-Observability-Change: this patch adds no metric, span, label, or log field.
Intent volume is already reported by
`eshu_dp_reducer_intents_enqueued_total`. Reducer outcomes and duration remain
visible through `eshu_dp_ci_cd_run_correlations_total`,
`eshu_dp_reducer_executions_total`, and
`eshu_dp_reducer_run_duration_seconds`. The new Postgres reads use the existing
instrumented query path and `eshu_dp_postgres_query_duration_seconds`.
