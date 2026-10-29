# Claim-plane client seam — performance and observability evidence

Stage-1b (#8875) adds `claim_plane_client.go`, a `ControlStore`
implementation that speaks workflow.v1 HTTP to eshu-workflow. The file is
hot by location (`go/internal/workflow/*.go`) but touches no existing hot
path: it is purely additive, nothing in-tree constructs it, and every
existing test passes unchanged.

Performance Evidence: `BenchmarkClaimPlaneClientClaimNext`
(`claim_plane_client_test.go`) drives one grant round trip per iteration
against an `httptest` stub over loopback. Backend: Go 1.26.10
linux/arm64, shared dev host, `-benchtime 10000x -count 2`. After
measurement: ~109-112 us/op, ~19 KB/op, ~238 allocs/op. Input shape: one
pending item granted immediately (no pacing, no quota); the number bounds
client-side marshal, HTTP, and translate-back cost with server work near
zero. Terminal queue/row counts: none — the benchmark asserts no store or
queue interaction. Why safe: the seam adds a new call path only; the
in-tree `WorkflowControlStore` SQL, lock order, and reaper are untouched,
so no existing latency or contention behavior can regress. First real
server-side numbers come from the Stage-1 cutover rehearsal against the
deployed claim-plane binary, not from this stub bound.

Observability Evidence: the client wraps every failure with the operation
name and maps protocol envelopes to caller errors (`stale_fence`,
`lease_expired`, `claim_conflict` collapse to `ErrClaimRejected`;
pacing surfaces as `ClaimPacingDeferral` with the backoff, the type the runner backoff path matches). Server-side, each
operation carries an OpenTelemetry span plus Prometheus counters
(`workflow_claim_grants_total`, `workflow_claim_mutations_total`,
`workflow_commit_pins_total`, `workflow_claim_reaps_total`) and
structured slog lines, served on the admin `/metrics` listener. No new
dashboard or alert is needed for the seam: it emits no signals of its own
and inherits the claim-plane counters above.
