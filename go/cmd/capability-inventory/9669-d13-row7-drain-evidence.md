# Evidence: #9669 D13 row-7 drain gate wiring is performance-neutral

Refs #9669. This lane points the capability-inventory CLI at the
satellite's served OpenAPI document (authed fetch) and wires the
verify/generate gates plus the commit hook through the
capability-stub-satellite harness, which serves the in-tree assembly
behind the same RequireAuth-shaped gate. The CLI fetch path is
unchanged (same client, timeouts, fail-closed errors); only the
document source moves from a local parse to an HTTP fetch of
byte-identical content. The new stub_satellite.go helper carries
`//go:build ignore`, so it is never compiled into any binary, test,
vet, or lint run — only an explicit `go run` of that file (which the
wrapper performs) compiles it.

No-Regression Evidence: capability-inventory package green before and
after. Baseline is `go test ./cmd/capability-inventory/ -count=1` ok
at the parent commit plus the stub-satellite harness vectors passing
standalone. After measurement is the same package green at this head
plus the harness test 11 passed / 0 failed and `verify` + byte-identical
`generate` proven through the harness (162 entries / 647 surfaces,
`cmp` clean). Backend is Go 1.26.11 with an httptest loopback double;
no graph backend, worker, queue, or batching knob is touched, so there
is no before/after latency delta to measure. Input shape is unchanged
(the served document is the same assembly the satellite serves,
parity-pinned per row 7); row counts are unchanged.

No-Observability-Change: no span, metric, log, or status surface is
added or altered. The stub writes only usage errors to stderr on
misinvocation and never logs the token (it never leaves the bearer
comparison; covered by the redaction tests). The CLI paths emit no
new telemetry.
