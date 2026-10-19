# Request Timing (Server-Timing Trailer)

Every `/api/v0` application response that reaches the handler carries a
`Server-Timing` trailer with the handler total and its Postgres and graph
stages, for example:

```http
Server-Timing: total;dur=8730.0, pg;dur=8710.5, graph;dur=3.2
```

`dur` is milliseconds with one decimal. The stage set is fixed at three and
`total` leads, so a total-only reader keeps working on the value prefix:

- `total`: the handler wall time the request-metrics middleware brackets
  around the handler.
- `pg`: time attributed to the request's Postgres search phase, recorded
  by the search seams through the request's stage clock. The span brackets
  the seam body: store reads plus grant checks and in-handler merge shaping.
- `graph`: accumulated graph store-call time on the request, recorded the
  same way.

Only the search seams record today, so any route reaching them reports
nonzero `pg`/`graph`: `POST /api/v0/code/search` and
`POST /api/v0/content/entities/search` (including their rank-walk and
unscoped shapes), and any other route sharing a seam such as
`POST /api/v0/code/symbols/search`. Routes that never reach a recording
seam report zero. Selector-resolution reads are not attributed.

Each stage sums its own calls, so store calls that run in
parallel add rather than union: `pg`+`graph` may exceed `total`, and that is
expected accounting, not clock skew. `total` stays wall time either way.

The value travels as a trailer rather than
a response header because response headers flush on the handler's first body
write; trailers are delivered for buffered and streaming (SSE) responses
alike. Read it after consuming the full body (with curl, `curl -v` shows the
trailer on the chunked transfer). The middleware gates the announcement:
statuses that cannot carry a body (1xx, 204, 304) never announce, and neither
does a handler that returns without writing (nor any HEAD response), so the
value lands as a regular response header rather than a trailer: a
trailer-only reader would miss it, while a header reader still sees it. (For
HEAD with an already-sent header block the value drops instead.) 204/304
responses carry neither. This is deliberate as well as compatible: a `Trailer`
header on a bodyless response crashes Node-based proxies with
`ERR_HTTP_TRAILER_INVALID` (seen on a 204 in the SSO E2E gate). One known
exception: an explicit body-allowed status with no following body still
announces, as before; no `/api/v0` handler does this (the tree's only such
shape, the MCP 202, serves outside this middleware). Rejections written
outside the middleware
(auth, rate limiting, checkpoint gating, such as a 401/403/429) carry no
trailer. Both the API server and the MCP server wrap their `/api/v0` muxes in
the same middleware, so both emit it. Probes, `/metrics`, and other
admin-surface responses do not carry it.
