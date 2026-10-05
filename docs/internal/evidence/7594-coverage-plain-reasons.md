# #7594: plain-language reasons on cross-repo coverage gaps

## Scope

`consumer_coverage` in the `POST /api/v0/code/dead-code/cross-repo` response
(and the `find_cross_repo_dead_code` MCP tool) gains text an admin can act on.
Each `incomplete[]` entry gets `reason` and `next_step`. `consumer_coverage`
gets `coverage_summary`. All three are fixed text derived in memory from the
`state` the response already carries and from whether the request named its own
`consumer_repo_ids`, by `coverageGapText` and
`coverageSummary` in `go/internal/query/codequery/deadcode`. The change is
additive: no existing field changes, and there is no new data, schema, or SQL.

`CrossRepoDeadCodeCoverageStates` (in `querycontract/code`) lists every state
the coverage statements can return, and `KnownState` reads it.
`TestCoverageGapTextCoversEveryKnownState` fails when a listed state has no
text, so a new state cannot ship without a reason and a next step.

`truncated` says what Eshu knows and does not know: the watermark stores only
a boolean, so the reason names both causes ("no entry points ... or the walk
hit its depth or size limit") and does not pick one. Separating them needs a
stored reason column or a read-time probe and is a follow-up if admins need it.

`coverage_summary` counts only the listed gaps. The coverage statement stops at
its cap and never counts the repositories it checked, so a cut list says "At
least N" and the sentence never claims a total. It does not quote the cap
either: the statement trims duplicate-hidden repositories after it sets the cut
flag, so a cut list can hold fewer than the cap.

## Example responses

The `consumer_coverage` object for one repository in each state (other fields
as before; abridged to the keys this change touches):

```json
{"repository_id": "repo-a", "state": "no_snapshot_yet", "generation_id": "gen-1", "retryable": true,
 "reason": "Eshu has not finished building the call-graph snapshot for this repository (queued or running).",
 "next_step": "Wait and ask again. This should clear by itself."}
{"repository_id": "repo-b", "state": "older_epoch", "generation_id": "gen-2", "retryable": true,
 "reason": "The snapshot was built by an older version of the analysis and is being rebuilt.",
 "next_step": "Wait and ask again. This should clear by itself."}
{"repository_id": "repo-c", "state": "truncated", "generation_id": "gen-3", "retryable": false,
 "reason": "The snapshot is current but Eshu cannot prove it is complete: no entry points (roots) were found for this repository, or the walk hit its depth or size limit.",
 "next_step": "Waiting will not clear this. Name the repositories you care about with `consumer_repo_ids`, or check whether this repository's framework entry points are modeled."}
{"repository_id": "repo-c", "state": "truncated", "generation_id": "gen-3", "retryable": false,
 "reason": "The snapshot is current but Eshu cannot prove it is complete: no entry points (roots) were found for this repository, or the walk hit its depth or size limit.",
 "next_step": "Waiting will not clear this. Check whether this repository's framework entry points are modeled."}
{"repository_id": "repo-d", "state": "no_active_scope", "retryable": false,
 "reason": "This repository id is not an indexed repository.",
 "next_step": "Check the id, or index the repository."}
```

`coverage_summary` for four listed gaps (two that clear, two that do not), for
a cut list, for two ids that are not indexed, and for a complete answer:

```text
4 repositories cannot be judged yet: 2 of them should clear on their own, 2 will not. Name the repositories you care about with `consumer_repo_ids`.
At least 25 repositories cannot be judged yet (the list was cut): 3 of them should clear on their own, 22 will not. Name the repositories you care about with `consumer_repo_ids`.
2 repositories cannot be judged yet: 0 of them should clear on their own, 2 will not. Check the ids, or index the repositories.
No repository checked has a coverage gap.
```

The second `truncated` entry above, and the summary below, are what a
request that named its own `consumer_repo_ids` gets. It is not told to name
them again:

```text
12 repositories cannot be judged yet: 3 of them should clear on their own, 9 will not. Check whether their framework entry points are modeled.
```

## Proof

`TestCoverageGapTextCoversEveryKnownState`, `TestCoverageGapTextWording`,
`TestCoverageSummary` (complete, singular, mixed, capped, cut with fewer than
the cap, every gap unindexed, unindexed beside other gaps, capped and all
clearing), and the handler tests
`TestCrossRepoDeadCodeConsumerCoverageReportsPerRepositoryState` and
`TestCrossRepoDeadCodeCompleteCoverageHasEmptyIncompleteArray` pin the wording
and the wire shape. Before the change they failed on empty text and on the
missing fields; after it they pass.

No-Regression Evidence: the text is built in memory from at most 25 gaps per
response (the existing cap), with one pass over the list and no allocation
beyond a short string per gap. The scope is the cross-repo dead-code response
shaping only. There is no SQL change, no new query, no schema change, and no
change to the coverage statement or its read plan, so no query plan, lock,
claim, queue, or worker path can move. No timing is claimed.

No-Observability-Change: no metric, span, log key, status field, worker, or
queue stage is added or changed. The existing request span and the
`consumer_coverage` response fields already tell an operator that a coverage
gap fired; this change only makes the same state readable.
