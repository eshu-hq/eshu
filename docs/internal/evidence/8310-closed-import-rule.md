# #8310: closed import-name rule for plain package calls

## Summary

#8310 closes the import-local-name rule for the #7601 plain-call shape
(`local()`, `new Local()`, `<Local />`, member objects): a name keys a
package call only for the allowed occurrences, with a file-level guard for
`with`, sloppy `eval`, and identifier escapes. Bare reads of import-bound
names no longer key. `gitGenerationContentVersion` moves `v7` to `v8` with a
re-pin so synced stores keep the new output.

## Performance

No-Regression Evidence: interleaved base `cae18b491` and head `5c92f5540`
test binaries, 8 rounds each (`ImportCalls`) and 8 rounds each
(`MemberCalls`), `-cpu 1 -benchtime 10x` (plus 4 alloc rounds for
`ImportCalls`), go1.26.6 linux/arm64 on a 16-core host at load 1.2 to 2.8,
2026-10-17.

| Benchmark | Base median (min to max) | Head median (min to max) | Delta | Allocs/op |
| --- | --- | --- | --- | --- |
| `BenchmarkParsePathTypeScriptPackageImportCalls` | 384.9 ms (384.3 to 385.6) | 369.2 ms (368.6 to 372.4) | -4.1% | 992,476 to 1,022,319 (+3.0%) |
| `BenchmarkParsePathTypeScriptPackageMemberCalls` | 99.0 ms (98.7 to 99.3) | 96.9 ms (96.7 to 97.0) | -2.1% | 280,698 to 279,227 (-0.5%) |

The closed rule adds one source rescan per file that holds plain keys
(the `ImportCalls` alloc increase), while dropped bare-read keys remove
downstream stamping work (the wall-time decrease on both benches). No
hot-path budget is consumed: both benches are faster at the head, and
the member bench allocates less.

Observability Evidence: no new signal. Resolved edges carry
`resolution_method=import_binding`, and the existing `code call symbol
definition load` log counts package keys and producer scopes. The
unverified/debug paths are unchanged.

## Limits

- Deployed-graph recall re-measure is NOT_CHECKED (needs an image deploy
  and reindex).
- The member rule keeps its `safeRead` positions while the plain rule
  drops them; the rules guard different hazards (review P3, accepted).
