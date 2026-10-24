# MCP initialize protocol-version negotiation

Issue #8800. The `initialize` handler used to answer a fixed protocol version.
It now asks `negotiateProtocolVersion` (`go/internal/mcp/protocol_version.go`)
for the answer, so a requested version the server supports is echoed and any
other request gets the server's latest supported version. Only `2024-11-05` is
supported, so every reply is still `2024-11-05`; the change is one negotiation
point and a log line, not a behavior change for any client.

## Evidence

No-Regression Evidence: `initialize` runs once per client session and is not on
a per-request path. The new work is one `slices.Contains` over a one-element
list. Measured with `go test -bench 'Negotiate|Truncate' -benchtime=2s -count=3`
on Go 1.26.10, linux/arm64, 16 cores, shared host:

| Benchmark | Result (3 runs) | Allocations |
|---|---|---|
| Unsupported request (`2025-06-18`) | 2.44 to 2.56 ns/op | 0 |
| Supported request (`2024-11-05`) | 1.85 to 1.89 ns/op | 0 |
| Log truncation of a 4 KB value | 72 to 75 ns/op | 1 (80 B) |

The baseline is the previous fixed constant, which cost nothing. The added cost
is under 100 ns once per session, on a path with no graph, queue, or database
work, so there is no input shape or row count to report.

Observability Evidence: an unsupported requested version writes one info log
line with `requested_protocol_version` (cut to 64 bytes on a rune boundary) and
`negotiated_protocol_version`. No metric is added, because the requested value
is client-chosen and would be an unbounded label. The log fields are listed in
`docs/public/reference/mcp-reference.md`.
