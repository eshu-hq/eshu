# codedivergence

## Purpose

`go/internal/query/codedivergence` owns the code-divergence finding
contract: parallel_implementation.exact and .renamed findings assembled
from fingerprint equality groups.

## Where this fits

```
parse -> emit facts (fingerprints) -> grouping SQL (ContentReader)
  -> AssemblePage here -> HTTP handler (codequery) / MCP tools
```

The grouping SQL lives with `ContentReader` (package `query`); this package
owns everything the SQL returns turns into: members, reasons, scores,
suppressions, finding ids, and investigate follow-ups.

## The score contract

Score is members × token count, decomposed without remainder into
`reasons[]`: one stream reason plus one additional-copy reason per extra
member, with zero-weight signal reasons (package span, large body) listed
for judgment. `score == sum(reasons)` always — a unit test mutates each
reason and asserts the sum tracks. No hidden terms.

## Suppression catalogue

Every rule is a named function with its own regression test (a pair it must
suppress and a near-miss it must not), and assembly reports per-rule counts:

- `below_floor` — under the 50-token floor
- `generated_file` — protobuf outputs, `.generated.` infixes, generated trees
- `vendored_path` — vendor, third_party, node_modules
- `test_file` — per-language test filenames, unless the caller opts in
- `trivial_accessor` — getter/setter-shaped names at most twice the floor
- `wrapper_family` — five or more same-name copies (intentional parallels)

## Notes

Finding ids derive from (repo_id, kind, fingerprint): stable across
generations. Truth level is always `derived`. The grouping path never reads
`source_cache`.

## Convention outliers (#6839)

`parallel_implementation.convention_outlier` findings select cohort members
missing a call the cohort majority makes. Cohorts come in trust order —
`interface` (Functions contained in one interface's implementers),
`router` (Functions with HANDLES_ROUTE edges under one endpoint mount),
`package` (Functions in one package directory) — and every finding names
its source. Selection (`SelectOutliers`) admits one verdict per callee at or
above the share floor with at least one non-caller; confidence inherits the
weakest majority CALLS edge and inferred-edge majorities are labelled.
Outliers reaching the callee through a wrapper carry the
`outlier_wrapper_mediated` ambiguity signal (coordinate: the wrapper_bypass
surface owns the canonical-wrapper verdict). Cohorts above the member cap
truncate in entity-id order and report the cutoff; nothing samples
silently. On the findings page the track emits after the stat-ranked
stream is exhausted, in limit-bounded slices through the same offset
accounting, so every finding emits exactly once and pages stay bounded.
Score stays members × tokens with reasons summing exactly.
