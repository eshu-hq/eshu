# AGENTS.md — internal/redact guidance for LLM assistants

## Read first

1. `go/internal/redact/README.md` — package contract and security invariant.
2. `go/internal/redact/redact.go` — exported API and marker construction.
3. `go/internal/redact/policy.go` — collector-neutral classification API.
4. `go/internal/redact/registry.go` — hosted governance surface matrix.
5. `go/internal/redact/redact_test.go`, `policy_test.go`, and
   `registry_test.go` — deterministic and fail-closed tests.

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
- **Registry errors stay safe** — hosted governance canary checks may name a
  surface and class, but must not echo the raw canary or payload.

## Common changes and how to scope them

- **Add a scalar encoding** — extend `scalarBytes`, add a table-driven test, and
  ensure unsupported structs, maps, and slices still avoid serialization.
- **Add map helpers** — only do this when a concrete collector caller needs it.
  Keep helper behavior shallow and explicit so callers decide which fields are
  sensitive.
- **Add sensitive-key classification behavior** — extend `RuleSet` only when the
  behavior is provider-neutral. Use caller-supplied versioned key lists; never
  embed AWS, Terraform, or cloud-provider lists here.
- **Add a hosted governance surface** — update `registry.go`, add a
  `registry_test.go` case, and update
  `docs/public/reference/hosted-redaction-registry.md`.
- **Change marker format** — treat this as a compatibility change. Existing
  facts may depend on stable marker strings across generations. Since #5859
  this covers the persisted map's FIELD NAMES as well as the marker string:
  `IsRedactedValue` recognizes a round-tripped marker by its `"marker"` key,
  and `go/internal/storage/postgres` reads it to keep a redacted value out of
  drift comparison. Renaming that field silently turns every redacted value
  back into comparable garbage, and no test in this package would catch it.
- **Change what `IsRedactedValue` accepts** — it deliberately matches only the
  `{marker,reason,source}` object shape, never a bare marker string. Widening
  it to a prefix match would misclassify genuine values; see the godoc for the
  fingerprint fields that legitimately store a bare marker.

## Anti-patterns specific to this package

- Serializing arbitrary structs, maps, or slices in `Scalar`.
- Adding Terraform, AWS, or provider-specific sensitive-key policy here.
- Emitting logs, metrics, or spans from this package.
- Returning raw input on empty, nil, unsupported, or malformed values.
- Hardcoding production redaction keys in code or tests.
