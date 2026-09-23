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
a proven edge from an inferred one, and changing a value is a wire change.

The literals are pinned outside this package, including across a language
boundary. Measured at `2d68f1cbb`:

| literal | also asserted in |
| --- | --- |
| `k8s_service_name_namespace` | `apps/console/src/api/eshuGraphDeployment.truth.test.ts`, `content_relationships_k8s_test.go`, `entity_content_iac_fallback_test.go`, `impacttrace/impact_trace_deployment_k8s_test.go` |
| `k8s_service_selector_match` | `content_relationships_k8s_test.go`, `content_relationships_k8s_truncation_test.go`, `impacttrace/impact_trace_deployment_k8s_test.go` |

The console test asserts the string on the wire
(`expect(selectsEdge?.evidence).toContain("reason: k8s_service_name_namespace")`),
so changing a value breaks a TypeScript test, not only Go ones.
`docs/public/languages/kubernetes.md` documents the SELECTS capability and
states the fallback rule ("a known selector is authoritative and never falls
back, even on a name/namespace coincidence") but does not quote the strings. So
a behaviour change needs that page read; a rename does not.

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

Measured at this head, with the counting rule stated so a re-measure agrees:

```
rg -o 'kubernetes\.[A-Z][A-Za-z0-9_]*' --no-filename -g '*.go' go | wc -l   -> 112
rg -l 'querycontract/kubernetes"' -g '*.go' go | wc -l                      ->  12 files, 4 packages
cd go && go test -list '.*' ./internal/query/ ./internal/query/impact/ | rg -ic k8s -> 44
```

`-g '*.go'` is load-bearing. Without it the first command returns 119, because
prose elsewhere in the repository names these identifiers too. A count whose
scope is not written down invites the next reader to "correct" a number that
was right.
