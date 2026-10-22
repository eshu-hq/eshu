# Dispatch status handling

Only a 2xx answer from the internal handler is a tool success. A 4xx or 5xx
answer, and any 3xx, comes back as `isError: true`. No MCP-reachable route
redirects on purpose: the only `http.Redirect` calls in `internal/query` are the
OIDC, SAML, and GitHub browser login handlers, which no tool routes to. A 3xx therefore
means the request never reached its handler (#8772).

The usual cause is a required id argument left empty in a path template, such as
`/api/v0/workloads//context`, which the mux answers with a `307`. `resolveRoute`
refuses a route whose path has an empty segment before it builds a request and
names the empty required arguments from the tool's `InputSchema`
(`workload_id is required`). The status check in `dispatchToolWithOptions` is
the backstop for any redirect that still gets through.

Two tools, `get_relationship_evidence` and `investigate_service`, end in `/`
rather than `//` when their id is empty. The mux answers those with a 404, which
was already an error, so the empty-segment guard leaves them alone.

No-Regression Evidence: baseline is `origin/main` `de1e830767`; after is the
head that carries this note. Measured on Go 1.26.10, linux/arm64, 16 CPUs, a
shared host with a load average near 20 (not quiet, so single runs swing by up
to 30%). Three alternating base/head rounds, each
`go test ./internal/mcp -run '^$' -bench 'BenchmarkDispatch(PathIDSupplied|NoPathID|EmptyPathID)$' -benchmem -count=5`,
with the same `dispatch_redirect_bench_test.go` copied into a detached
`origin/main` worktree. The input is one direct `dispatchTool` call through a
real `ServeMux` that returns a 12-byte JSON body. Each cell is the median of the
three per-round medians of five runs:

| Benchmark | Base ns/op | Head ns/op | Base B/op, allocs/op | Head B/op, allocs/op |
|---|---|---|---|---|
| `PathIDSupplied` (`get_workload_story`, id set) | 6065 | 6321 | 5854, 81 | 5853, 81 |
| `NoPathID` (`list_indexed_repositories`) | 6186 | 6699 | 5757, 75 | 5758, 75 |
| `EmptyPathID` (`get_workload_story`, no id) | 6426 | 1717 | 6005, 90 | 1744, 31 |

A successful call allocates exactly what it did before (same allocs/op, B/op
within 1). Its ns/op difference is inside the host noise: the per-round medians
span 5944-7837 on base and 5988-6813 on head for `PathIDSupplied`, and head was
the faster side in the quietest round. The added work is one `strings.Contains`
on the route path. The empty-id call used to run a request to a 307 and report
success; it is now refused before any request is built, which is about 3.7
times faster with a third of the allocations. The schema lookup behind that
refusal is a map built once with `sync.OnceValue`; the first version rebuilt the
whole tool catalog per refused call (about 300 us and 533 KB), which the
benchmark caught. Response budget, timeout, and envelope handling are
unchanged. `TestDirectCallWithPathIDStillSucceeds` pins that a call with its id
set still succeeds.

Observability Evidence: a handler status of 3xx, with an empty, plain, or envelope body, logs `mcp tool dispatch got a
redirect, not a handler answer` at warn level with the fields `tool`, `status`,
`path`, and `location`; grep for that message to find the route builder that
produced it. The caller sees `isError: true` with the text `HTTP 307: ...`. A
call that leaves a required path id empty is refused before dispatch: the caller
sees `isError: true` with text such as `workload_id is required`, and no log
line or metric is emitted for it, the same as the other `<field> is required`
route errors. No new metric or span.
