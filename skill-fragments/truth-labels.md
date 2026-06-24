---
id: truth-labels
version: 1.0.0
byte_citation: docs/public/reference/truth-label-protocol.md#10-21
description: |
  API/MCP/CLI responses carry a `truth` envelope with `exact`,
  `derived`, or `fallback` levels. High-authority capabilities MUST
  return `unsupported_capability` rather than silently downgrading.
---

# Eshu Truth Labels

Every API, MCP, and CLI response that returns Eshu-claimed data MUST
carry a `truth` envelope. The envelope distinguishes a result that came
from persisted facts at full fidelity from a result that was computed
on the fly or that fell back to a degraded source.

The three valid levels are:

- `exact` — the answer is the persisted fact row, returned byte-for-byte
  from the source of truth.
- `derived` — the answer was computed from one or more persisted facts
  through a bounded query, projection, or materialization step.
- `fallback` — the answer was served from a partial, cached, or
  downgraded source because the high-authority source was unavailable.

A high-authority capability that is unsupported in the active runtime
profile MUST return `unsupported_capability` rather than emit a
silently downgraded `fallback` result. Silent downgrade of a
high-authority claim is a wire-contract bug.
