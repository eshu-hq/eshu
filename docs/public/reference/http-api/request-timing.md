# Request Timing (Server-Timing Trailer)

Every `/api/v0` application response that reaches the handler carries a
`Server-Timing` trailer with the handler total, for example:

```http
Server-Timing: total;dur=8730.0
```

`dur` is milliseconds with one decimal. The only stage today is `total`: the
time the request-metrics middleware brackets around the handler. It does not
split Postgres versus graph time. The value travels as a trailer rather than
a response header because response headers flush on the handler's first body
write; trailers are delivered for buffered and streaming (SSE) responses
alike. Read it after consuming the full body (with curl, `curl -v` shows the
trailer on the chunked transfer). The middleware gates the announcement:
statuses that cannot carry a body (204, 304) never announce, and neither does
a handler that returns without writing (nor any HEAD response), so the value
lands as a regular response header rather than a trailer: a trailer-only
reader would miss it, while a header reader still sees it. 204/304 responses
carry neither. This is deliberate as well as compatible: a `Trailer` header on
a bodyless response crashes Node-based proxies with
`ERR_HTTP_TRAILER_INVALID` (seen on a 204 in the SSO E2E gate). One known
exception: an explicit body-allowed status with no following body still
announces, as before; no `/api/v0` handler is known to do this. Rejections
written outside the middleware
(auth, rate limiting, checkpoint gating, such as a 401/403/429) carry no
trailer. Both the API server and the MCP server wrap their `/api/v0` muxes in
the same middleware, so both emit it. Probes, `/metrics`, and other
admin-surface responses do not carry it.
