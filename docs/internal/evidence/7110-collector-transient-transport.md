# #7110 transient transport errors: retry instead of exiting the collector

The performance-evidence gate is content-based: it flagged
`go/internal/collector/confluence/client.go`, `confluence/retry.go`,
`ociregistry/ociruntime/source.go`, and `sdk/transport.go` as runtime files.
The change adds an error classifier on the failure path of two source
collectors. It adds no Cypher, SQL, queue claim, lease, batching, worker count,
or concurrency knob.

## What the branch changes

- `sdk.IsTransientTransportError` classifies connection resets, refused or
  aborted connections, broken pipes, unexpected EOF, and network timeouts as
  transient. It is false when the parent context is done, for
  `context.Canceled`, and for TLS or certificate failures.
- The Confluence HTTP client wraps a transient transport error (request or
  mid-body decode) in `RetryableHTTPError` with `StatusCode` 0, so the existing
  source-level backoff schedules a retry instead of `collector.Service.Run`
  returning a fatal error.
- The non-claimed OCI registry `Source.Next` skips the target for the cycle and
  returns an idle poll on the same class of error. Claimed scans still return
  the error to `ClaimedService`.

## Why it is safe

No-Regression Evidence: the classifier runs only when a request already failed,
so the success path of both collectors executes no new code and makes no new
provider call. Backoff reuses the existing Confluence schedule (1s base, 1m
max, deterministic jitter); the OCI skip waits one existing poll interval, so a
persistently failing target is retried at most once per poll cycle and never in
a tight loop. There is no new goroutine, lock, or shared state.
Regression tests: `TestSourceBacksOffOnTransportErrorFetchingOnePage`
(no provider call while backoff is active),
`TestSourceNextIsolatesTransientTransportFailureOnPing` (next target still
scans), and negative guards proving cancellation, a cancelled parent context,
and certificate failures stay fatal. `go test -race` over the changed packages
is clean.

## Operator signals

No-Observability-Change: Confluence records the retryable transport failure on
the existing `eshu_dp_confluence_sync_failures_total` with the bounded
`failure_class=transport_error` value plus the existing
`confluence sync retry scheduled` warn log. OCI registry scans record the
existing `eshu_dp_oci_registry_scan_duration_seconds` with the bounded
`result=retryable_transport` value plus a bounded warn log carrying provider
and failure class only. No new instrument or label key is registered; the new
label values are documented in
`docs/public/reference/telemetry/metrics-ingestion-collectors.md`.
