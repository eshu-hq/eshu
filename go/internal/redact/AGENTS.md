# AGENTS.md — internal/redact guidance for LLM assistants

## Read first

1. `go/internal/redact/README.md` — package contract and security invariant.
2. `go/internal/redact/redact.go` — exported API and marker construction.
3. `go/internal/redact/policy.go` — collector-neutral classification API.
4. `go/internal/redact/redact_test.go` and `policy_test.go` — deterministic
   and fail-closed tests.

## Invariants this package enforces

- **Fail closed** — empty and unsupported sensitive values must still return a
  redaction marker.
- **No raw leakage** — marker strings must not contain raw input, source text,
  or reason text.
- **Keyed deterministic evidence** — the same key, raw value, reason, and source
  must produce the same marker, and changing key, reason, or source must change
  the marker digest.
- **Unknown-schema safety** — scalar values under unknown schema coverage are
  redacted; non-scalar values are dropped.
- **Uninitialized policy safety** — zero-value or otherwise uninitialized
  `RuleSet` values fail closed rather than preserving fields.
- **Unknown shape safety** — unknown `FieldKind` values are dropped rather than
  preserved.
- **Collector-neutral** — keep collector-specific key lists, provider schemas,
  and telemetry counters in callers.

## Common changes and how to scope them

- **Add a scalar encoding** — extend `scalarBytes`, add a table-driven test, and
  ensure unsupported structs, maps, and slices still avoid serialization.
- **Add map helpers** — only do this when a concrete collector caller needs it.
  Keep helper behavior shallow and explicit so callers decide which fields are
  sensitive.
- **Add sensitive-key classification behavior** — extend `RuleSet` only when the
  behavior is provider-neutral. Use caller-supplied versioned key lists; never
  embed AWS, Terraform, or cloud-provider lists here.
- **Change marker format** — treat this as a compatibility change. Existing
  facts may depend on stable marker strings across generations.

## Anti-patterns specific to this package

- Serializing arbitrary structs, maps, or slices in `Scalar`.
- Adding Terraform, AWS, or provider-specific sensitive-key policy here.
- Emitting logs, metrics, or spans from this package.
- Returning raw input on empty, nil, unsupported, or malformed values.
- Hardcoding production redaction keys in code or tests.
