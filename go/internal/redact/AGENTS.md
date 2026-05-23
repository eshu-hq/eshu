# internal/redact

## Read First

1. `go/internal/redact/README.md`
2. `go/internal/redact/doc.go`
3. `go/internal/redact/redact.go`
4. `go/internal/redact/policy.go`
5. `go/internal/redact/redact_test.go`
6. `go/internal/redact/policy_test.go`

## Package Rules

- Redaction MUST fail closed. Empty, nil, unsupported, malformed, unknown
  schema, uninitialized policy, and unknown field-kind inputs must not return
  raw values.
- Markers MUST stay keyed and deterministic for the same key, raw value, reason,
  and source. Changing key, reason, or source must change the digest.
- Marker strings MUST NOT contain raw input, reason text, source text, provider
  names, or schema paths.
- `NewKey` MUST reject blank key material and copy caller-provided material.
  Never hardcode production redaction keys.
- `Scalar` MUST NOT serialize arbitrary structs, maps, slices, or composite
  values. Unsupported values hash only a safe type class.
- `RuleSet` MUST remain collector-neutral. Provider-specific sensitive-key
  lists, schema bundles, and redaction counters belong in callers.
- This package MUST NOT emit logs, metrics, spans, facts, or status payloads.

## Proof

- Add table-driven tests for every scalar encoding, rule decision, and marker
  compatibility change.
- Run `cd go && go test ./internal/redact -count=1` for package changes.
- Run `go run ./cmd/eshu docs verify ../go/internal/redact --limit 1400 --fail-on contradicted,missing_evidence`
  for docs changes in this package.
