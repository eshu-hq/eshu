# Evidence: pacing-signal boundary remap (#10377, #10489)

## No-Regression Evidence (decode-path remap, no workers/queues/batches touched):

This change alters only the claim-plane response decode path: unknown
pacing-signal wire values widen to the protective global default with
the directive preserved. No worker count, batch size, lease TTL, retry
budget, queue, lock ordering, or transaction boundary changed; the
request count per operation is identical (one request, one observation —
`postClassified` preserves the single-`Observe` contract).

- Baseline: `go test ./internal/workflow/
  ./internal/collector/extensionhost/remote/claim/ -run
  'TestClaimPlaneClient|TestClaimPacing|Pacing|TestClaimNext'`
  on the pre-change tree: green (the two new RED tests excluded —
  they fail pre-fix with the specified modes: verbatim signal with
  outcome ok; remote `CallError invalid`).
- After: same command plus full `./internal/workflow/`,
  `./internal/collector/extensionhost/remote/claim/`, and
  `./internal/telemetry/` package suites: exit 0, zero failures,
  including after a clean rebase onto current `origin/main`.
- Backend/version: unit (no live backend); wire shapes unchanged —
  the remap consumes the already-decoded string, so the satellite
  contract and all existing known-signal fixtures take observably
  identical paths (proven: every pre-existing pacing/compat test passes
  unmodified with response untouched and outcome unchanged).
- Input shape: stubbed unknown signals (`FUTURE_99_XYZ`) on both
  operations plus paced/unpaced/blank edges; blank inputs behave
  exactly as before on each path.
- Terminal queue/row counts: not applicable — no queue, table, or
  batch is touched; no items are created, moved, or reaped by this
  change.

Why safe: the remap fires only on inputs that previously took the
wrong path (silent-success label / hard invalid failure). Every
previously-valid input follows the same statements as before, with
the same outputs.

## Observability Evidence:

- In-tree: remaps replace `ok` with the bounded outcome
  `pacing_signal_unknown` on the existing
  `WorkflowClaimPlaneRequests` counter and
  `WorkflowClaimPlaneRequestDuration` histogram (no new
  instruments; registration test green). Raw wire values render in
  the deferral log line only, never in a metric label.
- Remote: remaps surface as `*PacedError` with `Signal=global` and
  the raw value in the bounded, scrubbed detail text; they merge
  into global-paced backoff counts downstream (the grant-decision
  Observer contract cannot carry pacing events — see the code
  comment).
