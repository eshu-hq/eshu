# #8978: `.npmrc` under the metadata policy

Part of #8807. The design record is
[8813-credential-file-exposure.md](../design/8813-credential-file-exposure.md).
This note holds the measurements for the change. Every figure was produced on
this branch on a shared 16-core arm64 host (Go 1.26.10), with other jobs
running, so absolute numbers carry noise; the comparisons are within one run.

## Performance Evidence

Performance Evidence: binding `.npmrc` to `metadata` makes the default parser
registry governed (`Registry.HasContentStorePolicy` is true). Before, the
snapshot stage returned at one flag check for every repository. Now it looks up
each file's policy through `Registry.LookupByPath`.

| Benchmark (input: 5,000 files named `src/pkg/file.go`, no `.npmrc`) | Time | Allocs |
|---|---|---|
| Before, `BenchmarkContentStoreUngoverned/snapshot_stage_5000_files` on origin/main (261fcd2258) | 8.8 ns/op | 0 |
| After, same benchmark on a registry with every policy reset to store | 8.8 ns/op | 0 |
| After, `BenchmarkContentStoreGovernedDefault/snapshot_stage_5000_files_no_npmrc` on the default registry | 1.09 ms/op | 5,001 (80 KB) |
| Emit stage per file, before (nil map) | 8.2 ns/op | 0 |
| Emit stage per file, after (empty map) | 5.9 ns/op | 0 |

The governed cost is about 0.22 us and one 16-byte allocation per file, from the
defensive copy `LookupByPath` makes of the definition. The two figures are not
the same work: the before figure is a flag check that skips the loop, and the
after figure runs the loop. It is a new fixed cost per file, about 22 ms for a
100,000-file repository. An uncloned lookup measured 0.89 ms and 1 allocation
per 5,000 files, and was reverted because `TestPathDispatchesKeysMatchLookupByPath`
pins the `LookupByPath` source.

The parser, `BenchmarkParseConfig/npmrc` on a 10,000-line file with 2,000
registry lines, 8,000 credential, comment, and other lines: 5.7 ms/op, 77 MB/s,
4.3 MB and 42,005 allocations per parse. It runs once per `.npmrc` file.

## No-Regression Evidence

No-Regression Evidence: the baseline benchmark ran on origin/main at 261fcd2258 and the
touched paths are the snapshot stage and the engine dispatch. A repository with
no `.npmrc` emits the same facts as before; the only runtime differences are the
per-file policy lookup above and one registry-fingerprint move, which makes each
repository re-collect once. Neo4j and Postgres are untouched: no Cypher, SQL,
lease, queue, claim, or concurrency setting changes. The seeded `.npmrc` proof,
the content-version pin, and `go test` for `internal/parser/...`,
`internal/collector/repo/...`, `internal/collector/git/...`, `internal/content`,
`internal/mcp`, `internal/relationships`, and `cmd/eshu` pass on this branch.

`snapshot_content_version_gate_test.go` pins the emitted facts for
`testdata/content_version`, now with a `.npmrc`: 317 facts before, 320 after (the
file fact, the content fact, and the registry-URL entity). The content version
stays at `git-collector.v10`; the fingerprint moves because the registry gains a
name.

## Observability Evidence

Observability Evidence: no new signal. The collector's existing
`eshu_dp_collector_content_store_files_total{policy="metadata"}` counter and the
`git content store policy applied` log line count the metadata rows, and the
`redactor_failed` path cannot occur for `metadata`. An operator who finds no
registry fact for a `.npmrc` checks the parser skip rules in
`go/internal/parser/npmrc/README.md`; the parser logs nothing, because any log of
a skipped line could quote it. A path the guard withheld is visible on the row as
`registry_path_withheld`.
