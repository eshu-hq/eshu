---
paths:
  - "go/internal/telemetry/**/*.go"
  - "docs/public/observability/telemetry-coverage.md"
  - "scripts/verify-telemetry-coverage.sh"
---

# Telemetry contract

**Load `telemetry-coverage-discipline`.**

The four artifacts move together: the X1 contract doc, the X2 verifier, the X3 CI
gate, and the X4 dashboard. Registering an instrument without a doc row fails the
gate, and a doc row naming a metric that was never registered fails it the other
way.

Adding any new `.go` file under an instrumented collector triggers the blocking
coverage gate. If the new file genuinely reuses existing signals, that is what
the `No-Observability-Change:` marker is for — it is a positive assertion naming
the existing metrics, not a TODO.
