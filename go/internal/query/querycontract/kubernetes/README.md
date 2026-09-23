# kubernetes

Does this Service select that workload, and on what evidence.

## Why it exists

The Service -> workload `SELECTS` edge is built from four places: the content
relationship builder in `internal/query`, the deployment trace in
`internal/query/impact`, the GitOps helpers in `internal/query/impacttrace`,
and the port fake in `internal/query/querytestutil`. Before the move they
reached one implementation in `querycontract` through a set of unexported
wrappers in package `query` (`k8s_match_alias.go`, added by #6060). The wrappers
were compatibility only; #6642 retires aliases, so the implementation now has a
name callers use directly.

Keeping one implementation is the point. Namespace equality gates matching, so
two derivations of a namespace that disagree by a trailing space produce two
different graphs from the same input.

## The two reasons, and why the distinction is load-bearing

| reason | what it asserts | when |
| --- | --- | --- |
| `SelectReasonSelectorMatch` | proof: the Service's selector is a subset of the workload's pod-template labels | both sides captured their selector state |
| `SelectReasonNameNamespace` | inference: name and namespace match | the Service's selector state was never captured |

Both surface on the wire under `relationship["reason"]`, so a consumer can tell
a proven edge from an inferred one. `docs/public/languages/kubernetes.md`
registers them; change either constant and that page changes with it.

## Mixed vintage

`SelectorPresent` and `PodTemplateLabelsPresent` record whether the row captured
the field at all, which an empty string cannot express on its own. When a match
would pair a captured side with an uncaptured one, the edge is dropped rather
than guessed, and `LogSelectMixedVintageDrop` records the drop at debug level.

That is the one case where a missing edge is correct behaviour rather than a
bug, and the log line is how an operator tells the two apart at 3 AM. An edge
that disappeared after a partial re-index will have a drop line; one lost to a
real defect will not.

## Dependencies

Three symbols from the parent package and nothing else: `EntityContent`,
`K8sSelectCandidate` and `SafeStr`. The parent does not import this package,
which is what makes the extraction one-way.

Measured at this head: 112 references across 12 files in 4 packages, and 44
tests naming `K8s` in `internal/query` and `internal/query/impact`.
