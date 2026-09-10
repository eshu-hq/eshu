# Code Family Move — Evidence (#6060 PR3)

Covers the PR3 move of the CodeHandler family from root `internal/query`
to `internal/query/codequery` (analysis behind the `deadcode` leaf), the
#6618 destutter of touched filenames, and the separate bash-5.3
here-string fix in the ifa fault harness.

## Performance and observability

No-Regression Evidence: the move is behavior-preserving by construction.
`git diff origin/main --name-status -M` shows 232 renames; every production
statement keeps its bytes — the non-rename deltas are comment/path-key
updates, 15 lint-proven-dead wrapper deletions, and two test assertions
made non-vacuous. The 12 queryplan `source_sha256` digests verify
identical/broken/missing = 12/0/0, so every pinned row reader, probe
query, and shipped SQL text is byte-identical to the base. Cypher text,
query parameters, bounds, and call graphs are untouched; no worker,
batch, lease, timeout, or backend knob changed. Package tests green on
the moved tree: query, codequery, deadcode, entity, queryplan, cmd/api,
cmd/mcp-server.

No-Observability-Change: no span, metric, tracer, or pprof identifier is
added or renamed by this diff. The 117 telemetry-mentioning added lines
in the diff all sit inside moved files carrying their pre-existing
identifiers; the only telemetry-mentioning added line in a genuinely new
file is prose ("call-graph-metrics" in a comment). `handler_tracing.go`
is a pure rename of `code_handler_tracing.go` with identical span names,
and the root tracing-parity test was repointed at the new path and
passes. An operator reading dashboards at 3 AM sees exactly the same
signals as before this PR.
