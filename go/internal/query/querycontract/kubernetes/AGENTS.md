# AGENTS — go/internal/query/querycontract/kubernetes

## Do not add a second way to answer the match question

Four packages build the Service -> workload `SELECTS` edge and they must agree.
If a caller needs a variation, add a parameter or a named entry point here; do
not reimplement matching at the call site. Two implementations that agree today
diverge on the next selector change, and the symptom is a graph that contradicts
itself rather than a failing test.

The same applies to `Namespace`. It is three lines and it looks inlineable. It
is not: namespace equality gates matching, so a second trim is a second truth.

## Changing either reason constant is a wire change

`SelectReasonNameNamespace` and `SelectReasonSelectorMatch` are surfaced to
clients under `relationship["reason"]`, not internal labels. They are registered
in `docs/public/languages/kubernetes.md`. Changing a value means changing that
page in the same PR and treating it as a contract change.

## The presence flags are not redundant with the values

`SelectorPresent` beside `Selector`, `PodTemplateLabelsPresent` beside
`PodTemplateLabels`. An empty selector string means two different things — the
Service has no selector, or the row predates selector capture — and only the
flag separates them. Collapsing a flag into an empty-string check silently turns
every pre-upgrade row into a selectorless Service and drops real edges.

`LogSelectMixedVintageDrop` is the observability for that path. A change that
stops dropping mixed-vintage matches, or stops logging the drop, needs to say
what replaces the operator's ability to explain a missing edge.

## Keep the dependency one-way

This package imports its parent for `EntityContent`, `K8sSelectCandidate` and
`SafeStr`. The parent imports nothing from here, and that is what let the
package be extracted at all. Verify before adding a parent-side reference:

```
cd go && go test -c -gcflags=-e -o /dev/null ./internal/query/querycontract/
```

It must stay at exit 0 with this directory removed from the parent's build.

## No compatibility aliases

The wrappers this package replaced (`internal/query/k8s_match_alias.go`) were
deleted with the move. #6642's rule is that a move repoints its callers; it does
not leave a forwarding layer behind. Do not add one back for a future move.
