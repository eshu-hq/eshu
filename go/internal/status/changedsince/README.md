# Changed-Since Status

## Purpose

`internal/status/changedsince` owns the changed-since delta contract: a
bounded, closed-vocabulary diff of one prior generation's fact set against
the current active generation's fact set, for both a repository-scope
ingestion scope and a service-scope materialization lineage. It exists so
the diff's classification set, evidence categories, counts, sample bounding,
and unavailable-reason vocabulary have one authoritative home, shared by the
repository- and service-scope query handlers instead of drifting apart.

## Ownership boundary

This package owns the changed-since type contract only: filters, summaries,
category deltas, and the closed classification/category enums. It does not
compute a diff — the SQL that produces `ChangedSinceCounts`/`Samples` lives
in `internal/query/freshness`; this package only defines the shapes that
query surface fills in and the bounds (`Normalize`, `MaxSampleLimit`) it must
respect.

Unlike the other status families, neither `RawSnapshot` nor `Report`
aggregates this package's types today — `internal/query/freshness` imports
it directly. The one-way rule still holds structurally (root and every
sibling leaf may import `changedsince`; `changedsince` may import neither
the root nor a sibling leaf), but root's own `Report` currently carries no
changed-since field to populate.

## Exported surface

- `Classification`, `Classifications` — the closed delta verdict set (added,
  updated, unchanged, retired, superseded)
- `Category`, `Categories` — the repository-scope evidence categories (files,
  content entities, facts)
- `Filter` — bounds a repository-scope summary to one scope and prior
  reference, with `Normalize`, `HasScopeSelector`,
  `HasConflictingScopeSelectors`, `HasSinceReference`
- `Counts`, `Sample`, `CategoryDelta` — exact per-classification counts,
  bounded sample handles, and the per-category delta shape
- `UnavailableReason` — the fail-closed reason vocabulary:
  `UnavailableRetentionExpired` (the baseline was pruned) and
  `UnavailableBaselineNotComparable` (a delta generation at either end of the
  window, #7282)
- `Summary` — the bounded repository-scope changed-since answer;
  `SinceIsDelta`/`CurrentIsDelta` name the delta side of a
  `baseline_not_comparable` refusal
- `ServiceCategories`, `ServiceFilter`, `ServiceSummary` — the service-scope
  variant, reusing the same classification/counts/sample/unavailable shapes
- `MaxServiceScopeCandidates` — the bound on the admitted scope ids a
  service-scope answer lists when more than one ingestion scope holds a
  lineage for the service id and no `ScopeID` selected one (#6475)
- `Timestamp` — RFC3339 UTC formatting shared with the generation lifecycle
  drilldown contract
- `MaxSampleLimit`, `DefaultSampleLimit` — the per-classification,
  per-category sample cap and default

See `doc.go` for the full godoc contract.

## Dependencies

Standard library only (`strings`, `time`). This package has no dependency on
`internal/status/shared`.

## Telemetry

None. This package performs no I/O; it defines the diff contract that
`internal/query/freshness` fills in from Postgres.

## Gotchas / invariants

- `Counts` are always exact — only `Samples` are bounded by `SampleLimit`.
  A `Truncated[classification]` entry means the sample list is incomplete,
  never that the count is.
- `CategoryDelta.Unavailable` must be set, never inferred from an
  all-zero-count category, whenever the category could not be diffed (for
  example the current active generation is missing or still pending). A
  caller that treats a missing-but-zero category as "unchanged" hides a real
  gap.
- `HasConflictingScopeSelectors` exists because `Filter` accepts either
  `ScopeID` or `Repository` but not both; callers must fail closed on a
  conflict rather than intersecting the two, since intersecting can retain
  evidence scoped to an obsolete selector.
- `Categories` (repository-scope) intentionally excludes the service-scope
  categories (`CategoryOwnership`, `CategoryDeployment`, `CategoryRuntime`,
  `CategoryDependencies`, `CategoryDocs`, `CategoryIncidents`,
  `CategoryVulnerabilities`) — those only ever appear in `ServiceCategories`
  / a `ServiceSummary`'s `Categories` field, never in a repository-scope
  `Summary`.
- `ServiceFilter` has no `SinceObservedAt` fallback, unlike `Filter` — a
  service-scope diff always needs a prior service generation id, since
  service generations come from re-materialization, not an external clock a
  caller can name.
- `ServiceFilter` carries the caller's grant and an optional `ScopeID`
  (#6475). The reader binds them in SQL on the lineage row's `scope_id`; the
  handler never filters rows itself. `ServiceSummary.OutsideGrant` is
  telemetry-only (`json:"-"`) and must never reach a response body.
- `ServiceCategories` reports all seven evidence families (vulnerabilities
  landed in #1990); a new family's category constant belongs here, in
  lockstep with the SQL grouping by `evidence_family` in
  `internal/query/freshness`.

## Related docs

- `docs/internal/naming.md` — the nesting rules this leaf was created under
- Issue #6775 — the `internal/status` nest that introduced this package

## Evidence

No-Regression Evidence (#6949 batch 6, changedsince family): this change moves
the 32 `changedsince.*` compat entries' Go importers (9 type aliases, 19
consts, 3 vars, and 1 func, reached as both `status.*` and `statuspkg.*`)
off the transitional compat spellings onto the destuttered `changedsince.*`
names, and deletes the emptied `compat_changedsince.go`. Two files already
imported the leaf; no `changedsince` shadowing locals exist, so no renames
were needed. No type shape, wire field, SQL text, or executable statement
changes: across 28 code files, every hunk requalifies an identifier or edits
an import path only (410 refs; zero non-mechanical changed lines outside the
compat deletion and this doc reword). The status README's changed-since
section is reworded to the leaf names; the historical service-scope design
doc is untouched.
Measurement: packages ok on both sides with identical sets (ledger:6949-changedsince-batch6-after).
Sibling before-row `6949-changedsince-batch6-before`; baseline `315fab3199`
vs measurement commit `41ed1b42da` (this Evidence section and the two ledger
rows are the only later changes), with the ok-package set byte-identical
after timing strip.
`go test -list` inventory is identical on both sides (3904 tests).
Backend/version: go1.26.2 linux/amd64, in-memory; no backend touched. Input
shape: n/a (no runtime input). Terminal queue/row counts: none — no queue,
lease, Cypher, or SQL path is touched. Contract gates green on the branch:
`verify-openapi.sh` (261/261 routes), `verify-contracttest.sh`,
`verify-package-docs.sh`, and `verify-performance-evidence.sh`. The change
is safe because it cannot alter runtime behavior: the compiler resolves the
same types through their new paths, and the compat deletion is
compile-enforced total — any missed caller would fail the build.
No-Observability-Change: no metric, span, log line, or status output changes;
telemetry evidence is the unchanged test outcomes above.
