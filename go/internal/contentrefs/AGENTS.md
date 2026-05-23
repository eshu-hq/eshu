# AGENTS.md — internal/contentrefs guidance for LLM assistants

## Read first

1. `go/internal/contentrefs/README.md` — purpose, exported surface, invariants
2. `go/internal/contentrefs/hostnames.go` — `Hostnames`, gate logic,
   false-positive filters
3. `go/internal/contentrefs/service_names.go` — `ServiceNames`, keyword gate,
   denylist
4. `go/internal/storage/postgres/content_writer_references.go` — main caller;
   shows how extracted values feed Postgres lookup tables

## Invariants this package enforces

- **Line gate before regex** — `lineLikelyContainsHostname` (`hostnames.go:119`)
  and `lineLikelyContainsServiceName` (`service_names.go:56`) must return true
  before the extraction pattern runs. Do not remove these gates; they prevent
  treating every line of source code as a potential hostname or service name.
- **Sorted, deduplicated output** — both functions sort and deduplicate before
  returning. Callers that insert into Postgres rely on stable ordering for
  idempotent upserts.
- **No I/O, no errors** — neither function opens files, hits Postgres, or
  returns errors. Keep it that way.

## Common changes and how to scope them

- **Add a new hostname-context keyword** — add to `hostnameKeyPattern` or
  `hostnameEnvKeyPattern` in `hostnames.go`. Add a test case in
  `service_names_test.go` or a new `hostnames_test.go` (currently absent) that
  demonstrates the new keyword is recognized and that false positives are still
  rejected.

- **Add a new service-name keyword** — add to `serviceNameLineKeywords` in
  `service_names.go`. Run `go test ./internal/contentrefs -count=1`.

- **Extend the false-positive denylist** — add to `falsePositiveTLDs`,
  `falsePositiveSegments`, or `falsePositiveServiceNames` as appropriate. Run
  tests to verify the new entry is rejected and real hostnames / service names
  in the same line are still extracted.

- **Change the service-name length or part-count thresholds** — modify
  `isLikelyFalsePositiveServiceName` at `service_names.go:37`. Update the test
  table for the boundary cases.

## Failure modes and how to debug

- Symptom: expected hostname not appearing in Postgres lookup tables →
  cause: line failed the gate check → inspect `lineLikelyContainsHostname`
  for the source line; add the missing keyword if legitimate.

- Symptom: code property chains (e.g., `response.endpoint`) appearing as
  extracted hostnames → cause: CamelCase filter missed a segment or the TLD
  denylist lacks the term → add to `falsePositiveTLDs` or `codeCompoundKeywords`
  in `hostnames.go`.

- Symptom: `ServiceNames` returns names with only two hyphen-separated parts
  → cause: the three-part minimum check in `isLikelyFalsePositiveServiceName`
  is not running — likely the line gate is missing the relevant keyword →
  add the keyword to `serviceNameLineKeywords`.

## Anti-patterns specific to this package

- **Adding I/O or Postgres calls** — this package must remain pure pattern
  extraction. Any storage integration belongs in the calling projector stage.
- **Removing the line-gate functions** — gates keep extraction noise out of
  the index. Removing them will cause every plain word on every line to be
  evaluated and many false positives to reach Postgres.
- **Returning errors** — the contract is error-free. Adding an error return
  breaks every caller without delivering safety value, since there is no
  recoverable failure state in pure string pattern matching.
