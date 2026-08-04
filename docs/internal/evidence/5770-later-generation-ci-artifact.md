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
run from the previous correlation snapshot.

## Chosen behavior

A generation containing any `ci.run` keeps the existing full-snapshot behavior.
A generation containing `ci.artifact` but no run is a domain patch:

1. Extract the artifact's exact provider, run ID, and run-attempt keys.
2. Load the latest retained run-scoped facts for only those keys from older
   successful generations in the same scope.
3. Load the immediately preceding successful correlation snapshot, including an
   empty predecessor.
4. Recompute the affected runs, overlay them on the preceding snapshot, and
   write the complete result into the artifact generation.

Deployment events do not carry a run ID; they join to runs by commit SHA. After
the history read recovers a run, it loads retained deployment events for that
commit and repository. The normal classifier then reselects the environment and
its exact evidence fact. It does not copy old artifact or image-identity links
from the prior decision. Repository ID is optional on both facts: a matching
commit remains sufficient when either side omits it, while two populated,
different repositories do not join.

Historical selection ranks a tombstone before filtering it, so a retracted run
or artifact cannot return through an older row. The history query rejects more
than 10,000 facts; the predecessor query rejects more than 1,000. A storage
adapter that lacks the two patch reads fails the reducer intent instead of
acknowledging an empty result.

Carried decisions retain their original evidence fact IDs. Those IDs are useful
while superseded generations remain stored, but they are not permanent foreign
keys. A later full CI/CD snapshot can recompute the decisions against current
evidence and rebase that chain. If retention removes an older generation first,
its carried evidence link no longer resolves.

## Theory and performance checks

Performance Evidence: PostgreSQL 16 processed the same one-scope, 25-generation
input before and after the query rewrite: 216,000 retained step facts, 90 patch
keys, 1,000 prior decisions, and 1,000 terminal output rows. The full handler
fell from a 91.586-second baseline to 0.777 seconds, while existing reducer and
Postgres duration metrics retain operator visibility.

The performance question was whether the artifact-patch handler could remain
bounded on the existing fact indexes, or whether it needed a new JSON
expression index. The first production query joined every retained fact to the
requested run keys before ranking. The full handler disproved that design on a
representative same-scope backlog: almost all of its 91.586 seconds were spent
in the history read.

The replacement theory ranks the retained same-scope CI facts once, then
filters the latest rows to the requested keys. A test-only query selected the
expected 9,090 facts in 559.362 ms before the production SQL was changed.

| Full-handler measurement | Before | After |
| --- | ---: | ---: |
| Total handler | 91.586 s | 0.777 s |
| Historical evidence read | 91.524 s | 0.713 s |
| Previous snapshot read | 7.990 ms | 5.971 ms |
| Durable writer | 43.347 ms | 45.664 ms |
| Result | Failed 5 s budget | Passed 5 s budget |

Both runs used the same PostgreSQL 16 input: one scope, 25 generations,
216,000 retained step facts, 90 artifact patch keys, 1,000 prior decisions,
and a 1,000-decision output. The passing run wrote 1,000 distinct fact IDs and
patched exactly 90 decisions. This comparison establishes that the original
query shape was unacceptable and that the shipped handler meets the local
scale budget; it is not a general product speedup claim.

Other candidates were rejected before implementation:

| Candidate | Representative result | Accuracy | Disposition |
| --- | ---: | --- | --- |
| Broad provider/run/attempt expression index | 101 facts in 0.170 ms on a 92,500-row corpus | Passed | Rejected. The 9,544 kB index was about three times the existing 3,288 kB index and would add write cost to the highest-volume fact table. |
| Recompute only the artifact's run | Fastest write set | Failed | Rejected because the active-generation fence would make every unaffected prior run disappear. |
| Search backward for the latest non-empty snapshot | Bounded lookup | Failed | Rejected because an empty immediate predecessor is an authoritative empty snapshot; skipping it resurrects stale decisions. |

## Test-first and live proof

The first focused test run failed for both missing behaviors:

```text
go test ./internal/projector ./internal/reducer \
  -run 'TestBuildProjectionQueuesCICDRunCorrelationIntentForArtifactOnlyGeneration|TestCICDRunCorrelationHandlerPatchesLaterArtifactAndCarriesPreviousSnapshot' \
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
  -run '^TestCICDRunCorrelationArtifactPatchAgainstRealPostgres$' \
  -count=1 -v

--- PASS: TestCICDRunCorrelationArtifactPatchAgainstRealPostgres (0.22s)
ok github.com/eshu-hq/eshu/go/internal/storage/postgres 3.513s
exit 0
```

That regression proves a generation-three artifact can recover its
generation-one run and upgrade it to exact while carrying an unaffected run
from generation two. It also proves failed and future generations are excluded,
run attempt 2 cannot satisfy attempt 1, a payload-empty tombstone suppresses an
older live fact, and an empty immediate predecessor does not resurrect a still
older snapshot. It also covers both optional-repository fallbacks: a run without
`repository_id` can attach a repository-scoped deployment event, and a
repository-scoped run can attach an event that omits `repository_id`. Replaying
the same reducer intent writes the same fact IDs and payloads without duplicate
rows.

The opt-in scale regression runs the shipped handler, storage reads, strict
decision decoder, 1,000-decision overlay, and durable writer:

```text
manifest scopes=1 generations=25 retained_step_rows=216000 patch_keys=90
prior_decisions=1000 output_decisions=1000 duration=777.166708ms
history=713.016167ms previous=5.971375ms writer=45.663833ms budget=5s
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
Actions run 5150 is split across two generations for this regression. The B-12
query requires one object carrying the provider, run ID, repository, digest,
exact outcome, canonical write, environment, and environment evidence together:

```text
bash scripts/verify-golden-corpus-gate.sh

cassette facts settled: 18 collector sources, 33 scope generations
drains: 0 residual work, 0 nonterminal shared intents, 0 nonterminal completion events
mcp:list_ci_cd_run_correlations: 1 result
mcp:list_container_image_identities: 1 result
summary: 521 pass, 0 required-fail, 1 advisory-warn
pipeline wall time: 126s (blocking ceiling 1800s)
exit 0
```

The advisory was `phase_maintenance_drains`: 25 seconds against its 19-second
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
