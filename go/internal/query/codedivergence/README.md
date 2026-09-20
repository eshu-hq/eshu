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
