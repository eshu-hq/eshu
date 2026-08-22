# #6212 — runtime impact of the deferred-defect fixes

The `perf-evidence` gate flags one changed file as hot-path surface:
`go/internal/ifa/materializededges/materialized_edges_handles_route.go`.
This records why that change cannot move the runtime contract, and how that
was established rather than assumed.

No-Regression Evidence: no measurement is reported because there is nothing
whose latency could have moved. A benchmark here would time a format verb in a
branch that does not execute, which is the fabrication this section exists to
avoid. Three facts carry it, each verified on this branch.

First, the change is one character. The complete diff of that file is a single
line, `%v` -> `%q`, inside the format string of a
`return false, fmt.Sprintf(...)`. No control flow, no allocation on the success
path, no call added or removed.

Second, that line is on the failure branch. It renders only when
`handlesRouteRowsToExpectedEdges` reports at least one unresolved
`(repo_id, path)` pair — the case where HANDLES_ROUTE cannot bind because no
workload-materialization Endpoint exists at that key. When the gate passes, the
`fmt.Sprintf` never runs at all. `%q` on a `[]string` costs marginally more than
`%v` because it quotes and escapes each element; that cost is paid once, on a
slice the same guard has just reported as small, in a run that is already
failing.

Third, no runtime service reaches this code. Enumerating every main package
under `cmd/` and asking which links `internal/ifa/materializededges` returns
exactly one: `cmd/ifa`, the Ifá gate harness. None of the five runtime services
(`cmd/api`, `cmd/mcp-server`, `cmd/ingester`, `cmd/reducer`,
`cmd/bootstrap-index`) links it, so no serving or ingestion path executes this
file whatever the verb.

Backend/version and input shape are therefore not applicable to a throughput
claim: the code runs only under the two Ifá proof gates, against the committed
nine-fact symbol-runtime cassette, and only when that fixture fails to resolve
an Endpoint.

The other files this PR changes are shell case modules, a markdown catalog, a
Go static-registry test and two Go test files. None is compiled into a runtime
binary, and `perf-evidence` does not flag them.

Observability Evidence: the change improves operator-facing output rather than
altering it structurally. Unresolved keys are `repoID + "\x00" + path`, so the
previous `%v` put a raw NUL byte into gate output and the CI log, where it can
truncate or corrupt the surrounding line. `%q` renders the same value as
`["repo-1\x00/widgets"]`. Both verbs were measured before either was changed —
`%v` leaks the NUL, `%q` escapes it — and
`TestHandlesRouteUnresolvedDiagnosticEscapesNUL` now pins the rendered message
so the escaping cannot regress silently. No span, metric, counter, log field or
status surface is added, removed, renamed or re-cardinalised.
