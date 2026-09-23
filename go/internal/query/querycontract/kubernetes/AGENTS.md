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
clients under `relationship["reason"]`, not internal labels, so changing a value
is a contract change.

Run the search before you believe anything about where these strings live,
including anything written here. Measured at `2d68f1cbb`:

```
rg -l 'k8s_service_name_namespace|k8s_service_selector_match' .
```

Six files, and one of them is `apps/console/src/api/eshuGraphDeployment.truth.test.ts`
-- a TypeScript test asserting the value on the wire. Changing a constant breaks
a console test as well as four Go ones.

An earlier version of this file claimed both strings appeared in `select_match.go`
alone. That was wrong because the check behind it carried `--glob '!*_test.go'`
and searched only one of the two literals -- two filters, each hiding part of the
answer, under a sentence that reported the whole.

`docs/public/languages/kubernetes.md` documents the capability and the fallback
rule, so read it when the BEHAVIOUR changes. The comment that used to sit above
these constants said the page "registers" them; it did not, and this package's
own docs repeated that before it was checked.

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

The #6060 wrappers that once carried these names on package `query` were
deleted with the move, not repointed. #6642's rule is that a move repoints its
callers; it does not leave a forwarding layer behind. Do not add one back for a
future move.
