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

No-Regression Evidence: a call with its id set takes the same path as before;
the added work is one `strings.Contains` on the route path, and the schema
lookup runs only when that path has an empty segment. Response budget, timeout,
and envelope handling are unchanged, and `TestDirectCallWithPathIDStillSucceeds`
pins that a call with its id set still succeeds.

Observability Evidence: the backstop logs `mcp tool dispatch got a redirect, not
a handler answer` at warn level with `tool`, `status`, `path`, and `location`,
so an operator can find the route builder that produced it. The pre-dispatch
refusal is a caller error like the other `<field> is required` route errors and
adds no signal. No new metric or span.
