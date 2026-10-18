# Request Timing (Server-Timing Trailer)

Every `/api/v0` application response carries a `Server-Timing` trailer with
the handler total, for example:

```http
Server-Timing: total;dur=8730.0
```

`dur` is milliseconds with one decimal. The only stage today is `total`: the
time the request-metrics middleware brackets around the handler. It does not
split Postgres versus graph time. The value travels as a trailer rather than
a response header because response headers flush on the handler's first body
write; trailers are delivered for buffered and streaming (SSE) responses
alike. Read it after consuming the full body (with curl, `curl -v` shows the
trailer on the chunked transfer). Probes, `/metrics`, and other admin-surface
responses do not carry it.
