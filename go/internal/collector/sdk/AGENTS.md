# AGENTS.md - internal/collector/sdk guidance

## Read first

1. `README.md`
2. `doc.go`
3. `failure.go`
4. `http.go`
5. `docs/public/guides/collector-authoring.md`
6. `docs/public/reference/telemetry/index.md`

## Invariants this package enforces

- Provider response bodies, raw URLs, credential values, account IDs, resource
  names, and source record IDs must not appear in `HTTPError.Error` or
  `ProviderFailure.Error`.
- HTTP helpers must close response bodies on success, retry, and failure paths.
- Retry helpers must keep retry bounds explicit; this package must not add
  unbounded loops or hidden sleeps.
- The package must remain independent of Eshu internal facts, storage,
  workflow, telemetry, graph, reducer, query, and command packages.

## Common changes and how to scope them

- Add a common failure class only when at least two collectors need the same
  bounded workflow/status label.
- Add an HTTP helper only when provider-specific request traversal remains in
  the owning collector package.
- Add retry behavior with tests that prove the retry count, returned failure
  class, and response-body closure path.

## Failure modes and how to debug

- A provider body or credential in an error string is a privacy bug in this
  package or its caller's status wrapper.
- A retry loop that ignores `MaxRetries` is a collector availability bug; fix
  the SDK test before changing adopters.
- A graph, reducer, query, or storage import means the SDK boundary has leaked.

## Anti-patterns specific to this package

- Adding provider payload structs or endpoint pagination loops.
- Adding telemetry labels that include high-cardinality source values.
- Sleeping inside `DoJSON`; callers that need pacing or backoff own the clock
  and tests.

## What NOT to change without ADR review

- The internal-only package boundary.
- The no-Eshu-internal-dependency rule for this SDK package.
- The rule that provider-specific redaction remains in the owning collector.
