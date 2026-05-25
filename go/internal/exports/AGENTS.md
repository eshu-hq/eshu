# AGENTS.md — internal/exports guidance for LLM assistants

## Read first

1. `go/internal/exports/README.md` — package contract, format extension
   workflow, and verification commands.
2. `go/internal/exports/doc.go` — the godoc summary and the three invariants
   (bounded scope, determinism, redaction).
3. `go/internal/exports/exports.go` — format-neutral types every exporter
   accepts.
4. `go/internal/exports/registry.go` — registry, panic-on-double-register
   behavior, and `ErrUnsupportedFormat`.
5. `go/internal/exports/sarif.go` — the SARIF v2.1.0 writer used as the
   reference implementation.
6. `go/internal/exports/testdata/sarif/` — golden fixtures that lock the
   wire contract.

## Invariants this package enforces

- **One target per snapshot.** `Scope.Validate` rejects zero or multiple
  identifier fields. Do not relax this: exporters cannot safely filter a
  snapshot that targets two things at once, and the scope-drop defense in
  depth would silently keep evidence from a second target.
- **Scope-drop is defense in depth, not policy.** Exporters drop findings
  whose `RepositoryID`/`SubjectDigest`/`PackageID`/CVE/AdvisoryID disagrees
  with `snapshot.Scope`. The caller is still responsible for assembling a
  scope-correct snapshot. Do not weaken the drop on the theory that callers
  always get scope right.
- **Determinism is byte-stable.** Findings, rule lists, locations, advisory
  sources, and tags are all sorted before serialization. No format may
  serialize a Go map directly; convert to a struct or a sorted key list
  first.
- **Redaction never mutates caller data.** `applyPathRedaction` operates on
  a deep-copied Locations slice produced by `cloneFinding`. Any new
  format-specific writer that accepts paths must clone before mutating.
- **No I/O outside the writer.** This package does not read the DB, log,
  emit metrics, or open spans. Observability lives in the caller so the
  same exporter is reusable from CLI, MCP, and HTTP.
- **Reserved formats fail loudly.** `Registry.Export` returns
  `ErrUnsupportedFormat` for reserved format constants. Do not register a
  stub exporter that returns empty bytes — callers depend on the explicit
  error to surface `unsupported_capability`.

## SARIF-specific invariants

- **Rule key is `RuleID()`**: AdvisoryID, then CVEID, then FindingID. The
  same advisory can show up in many findings but exists once in
  `tool.driver.rules` with a stable `ruleIndex` that every result references.
- **Severity → SARIF level**: critical/high → `error`, medium → `warning`,
  low → `note`, none/unknown → `none`. Do not invent a new mapping; change
  the table in `severityToLevel` if SARIF guidance evolves.
- **`partialFingerprints` keys are versioned** (`eshu/findingId/v1`, etc).
  Changing a key without bumping `/v1` invalidates downstream dedupe; bump
  the suffix and document the migration.
- **Properties prefixed `eshu.`** for vendor-specific values so they do not
  collide with SARIF reserved property names.

## Common changes and how to scope them

- **Add a new SARIF property.** Extend `sarifRuleProps` or
  `sarifResultProps` with an `omitempty` field, populate it in
  `buildRuleProperties`/`buildResultProperties`, regenerate goldens with
  `-update-golden`, inspect the diff, and update README if it changes the
  wire contract.
- **Add a new exporter.** Follow the README "Adding a new format" section.
  Always start with the empty/single/multi golden trio.
- **Add a new scope kind.** Add the constant, extend `Scope.Validate` and
  `kindMatchesIdentifier`, then extend `findingMatchesScope` for every
  exporter (or add a shared helper if the matching rule is identical
  across formats).
- **Change severity normalization.** Add the alias to `NormalizeSeverity`
  and add a table-driven case in `TestNormalizeSeverity`. Do not change
  the canonical `Severity` constants — they appear in rule property
  payloads and in fixture diffs.

## Failure modes and how to debug

- **Golden test fails after an intentional change** → run the test with
  `-update-golden`, inspect the diff, confirm the wire contract change is
  intentional, then commit the regenerated golden alongside the code.
- **Out-of-scope evidence leaks into output** → look at
  `findingMatchesScope` for the scope kind; the test
  `TestSARIFExporter_DropsOutOfScopeFindings` should be failing too. Do
  not patch the caller — fix the matcher.
- **Output is non-deterministic** → look for a Go map being serialized
  directly. Convert it to a struct with explicit `json:` tags or to a
  sorted slice of key/value pairs.
- **Redaction marker leaks raw path bytes** → the redactor implementation is
  wrong; redact.RuleSet-backed implementations should call
  `redact.String`/`redact.Bytes` rather than returning prefixed raw paths.

## Anti-patterns specific to this package

- **Importing storage packages.** This package does not call Postgres,
  NornicDB, or the query package. All inputs arrive in a `Snapshot`.
- **Adding telemetry inside the writer.** Callers wrap with spans and
  counters. Adding telemetry here entangles CLI, MCP, and HTTP into one
  observability stack and breaks the `No-Observability-Change` contract for
  serializer-only edits.
- **Serializing Go maps directly to JSON.** Even when `partialFingerprints`
  is a `map[string]string`, Go's encoder sorts string keys deterministically;
  any other map shape must be converted to a sorted slice or struct.
- **Using `fmt.Sprintf` for floats.** Use `strconv.FormatFloat` (or the
  JSON encoder) to avoid locale-dependent output.
- **Returning a stub for a reserved format.** The reserved constants exist
  so callers can plan API contracts; only register an exporter once it
  passes the empty/single/multi golden trio.

## What NOT to change without an ADR

- The `Format` constant values. They appear in API responses, CLI flags,
  and operator docs; renaming is a wire break.
- The `eshu/*/v1` `partialFingerprints` key prefixes. Downstream SARIF
  consumers use these for dedupe across runs.
- The `eshu.` SARIF property prefix. Removing it collides with SARIF
  reserved property names.
- The deterministic ordering rules in `sortFindings`,
  `sortLocations`, and `sortAdvisorySources`. Changing the sort key
  reorders every existing golden and forces a downstream-visible diff.
