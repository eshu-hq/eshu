# Evidence: #9669 D13 seam wiring is performance-neutral

Refs #9669, Refs #10491. This lane swaps handler field types to pin
types (D3 `AdminIdentityReadAdmission` + `GovernanceAuditReadAdmission`,
D6 `StatusExtraReadAdmission` + `SupplyChainReadAdmission` + `ImpactPacketReadAdmission`,
G2-five read admissions) and composes narrow Postgres adapters behind
them. The per-request addition is one struct literal plus nil checks —
no I/O, locks, goroutines, batching, Cypher, queue, or worker changes —
and error paths keep the standing 503/omit behavior covered by the
fail-closed tests.

No-Regression Evidence: full drain pin-test surface green before and
after. Baseline is the 13 `TestQueryRootDrained` pin-test packages plus
the `go/internal/query/...` suite green at the frozen base (core
e2496962df). After measurement is the same suites green at this head:
13/13 drain packages ok plus `go test ./internal/query/... -count=1`
(all packages ok) and the new frozen method-set, seal, and fail-closed
wire-shape tests. No graph backend, worker, queue, or batching knob is
touched, so there is no before/after latency delta to measure: the
change adds no executable path beyond the struct literal. Input shape
is unchanged (same routes and params; suppression/parity tests pin the
filter threading); row counts are unchanged (no statement touched).

No-Observability-Change: no span, metric, log, or status surface is
added or altered (verified: the lane diff adds no log/metric calls).
The pins declare no telemetry; the fail-closed probes assert through
the existing handler error paths, which are unchanged.
