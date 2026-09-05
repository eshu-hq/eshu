# Eshu Failure Classes To Name Explicitly

Every review must state whether the diff could trigger any of these classes and
where the proof lives:

- false-green tests;
- unexercised production subjects hidden behind helper tests;
- golden-corpus or B-12 snapshot drift;
- stale generated artifacts or stale discovery registries;
- workflow or local-gate parity gaps;
- NornicDB planner fallback or version-skewed optimizer assumptions;
- route, API, MCP, CLI, or OpenAPI mismatch;
- public report redaction or classifier overreach;
- materialization, graph projection, or query-surface disagreement;
- concurrency, lease, retry, idempotency, or ordering bugs;
- telemetry coverage gaps or missing operator-facing evidence;
- private-data, secret, or AI-attribution leakage.

Naming a class is not the same as clearing it. For each class the diff could
plausibly trigger, cite the specific evidence that rules it out — the test that
exercises the production subject, the gate run that covers the snapshot, the
trace flag that proves the fast path. "Not applicable" needs a reason; classes excluded by the same scope boundary
may share one explanation.
