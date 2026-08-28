# #6276 — the narrowed-domain count is a table now, not prose in three files

`RetractEdges` narrows the whole-scope retract for four domains. Until this
change, that four was written down only in English, in three comments, and
nothing checked any of them:

```
$ rg -n 'four narrowed domains|four fenced domains' go/
go/internal/storage/cypher/edge_writer_nil_fence_whole_scope_test.go:22
go/internal/storage/cypher/edge_writer_nil_fence_whole_scope_test.go:37
go/internal/storage/cypher/edge_writer_logging.go:232
$ rg -n 'narrowedDomains|fencedDomains|wholeScopeNarrowedDomains' go/
(no matches)
```

The test that exercises the narrowing looped over a hardcoded literal of the
same four names, so a fifth narrowing site would have falsified all three
comments and left the loop passing on its own list.

## Why re-deriving it gives the wrong answer

`domainHasRepoWideRetract`
(`go/internal/reducer/shared_projection_worker_refresh_fence.go:91`) returns
true for **seven** domains, not four. The other three — `handles_route`,
`runs_in`, `invokes_cloud_action` — are fenced but not narrowed: they fall
through to `buildRetractStatement` with the batch-wide repo-id list. A fifth
look-alike, `code_calls`, is deliberately excluded with a "do not fix this for
symmetry" note at its branch.

So anyone re-deriving the number under the fencing predicate gets seven and
concludes the comments were wrong. They were not. Only a reader who walks the
non-test call sites of `collectWholeScopeRefreshRepoIDs` can tell the two groups
apart, and that is not a derivation anyone should have to repeat.

## What changed

`go/internal/storage/cypher/edge_writer_retract_scope.go` holds one table:

```go
var wholeScopeRetractDomains = map[string]bool{
	reducer.DomainInheritanceEdges:   true,
	reducer.DomainRationaleEdges:     true,
	reducer.DomainSQLRelationships:   true,
	reducer.DomainShellExec:          true,
	reducer.DomainHandlesRoute:       false,
	reducer.DomainRunsIn:             false,
	reducer.DomainInvokesCloudAction: false,
}
```

Both groups are in it, because the trap is the boundary between them, not either
group on its own.

The production dispatch reaches its narrowed half through one helper,
`narrowedWholeScopeRepoIDs`, which is now the only place
`logWholeScopeRetractSkipped` is called from. A domain outside the narrowed half
gets an error there, not a skip: skipping would be a silent lost retract, which
is the failure the whole mechanism exists to make visible.

Three checks bind the table to the code:

- `TestRetractEdgesNilFenceShapeSkipsWholeScopeDelete` loops over
  `wholeScopeNarrowedDomains()`. A domain registered as narrowed that the
  dispatch does not narrow fails here.
- `TestFencedButNotNarrowedDomainsStillBindBatchWideRepoIDs` loops over the
  other half and asserts the opposite behaviour on the same unmarked batch. This
  is the one that catches the seven-versus-four mistake: narrowing one of those
  three without registering it makes its retract bind an empty list, and the
  test says so.
- `TestWholeScopeNarrowingHasOneSanctionedCallSite` parses the package's
  non-test files and counts callers of `collectWholeScopeRefreshRepoIDs` and
  `logWholeScopeRetractSkipped`. A hand-rolled narrowing branch that bypasses
  the helper entirely — invisible to both behavioural loops, because it would be
  consistent with itself — fails here.

Both loops are floored by `TestWholeScopeRetractDomainsHalvesAreNonEmpty`, and
the AST walk floors its own file count. Ranging over an empty slice is not an
error in Go; it passes having checked nothing.

The three comments no longer state a count. They point at the table.

## Verification

Run on this branch, macOS (Apple silicon, 32 GiB), after the final edit. No
scaled or remote run was involved and none is claimed.

```
$ cd go && go test ./internal/storage/cypher ./internal/reducer ./cmd/reducer -count=1
ok  	github.com/eshu-hq/eshu/go/internal/storage/cypher	1.105s
ok  	github.com/eshu-hq/eshu/go/internal/reducer	2.882s
ok  	github.com/eshu-hq/eshu/go/cmd/reducer	1.484s
exit 0

$ scripts/dev/precommit-go.sh lint <the five changed files>
lint: 1 package(s) from 5 path(s)
0 issues.
exit 0
```

### Mutation proof

Every guard was made to fail on purpose, with `go build` clean first so the red
is behavioural rather than a compile error, then restored to green.

| Mutation | subs | build | test | What reddened |
| --- | ---: | ---: | ---: | --- |
| `DomainShellExec: true` to `false` | 1 | 0 | 1 | halves floor, and `…StillBindBatchWideRepoIDs/shell_exec` |
| `DomainHandlesRoute: false` to `true` | 1 | 0 | 1 | halves floor, and `…NilFenceShapeSkipsWholeScopeDelete/handles_route` |
| hand-rolled fifth narrowing branch for `handles_route`, bypassing the helper | 1 | 0 | 1 | `…StillBindBatchWideRepoIDs/handles_route`, plus "called 2 time(s) from RetractEdges, want 1" and "UNSANCTIONED caller RetractEdges" |
| helper returns `skip=true` instead of an error for an unregistered domain | 1 | 0 | 1 | `…RejectsUnregisteredDomain` |
| AST walk suffix `.go` to `.golang` | 1 | 0 | 1 | "parsed only 0 non-test file(s); the file walk has collapsed" |

The third row is the trap the issue describes, reproduced and caught.

The AST guard reads the files from disk through `go/parser`, so a `-overlay`
mutation would not reach it and would pass vacuously. Every mutation above
edited the real file.

No-Regression Evidence: no query, statement, or binding changed. The four
narrowing branches keep the same collector, the same empty-list early return,
the same warning, and the same builders; only the three lines each of them
spelled inline now live in one helper. The Cypher text and bound parameters are
untouched, and the domain-by-domain assertions across the package's retract
tests pass unchanged (`go test ./internal/storage/cypher -count=1`, exit 0). The
added work per retract call is one lookup in a seven-entry map, off the per-row
path — the same class of no-op the #6166 narrowing itself was measured as.

No-Observability-Change: no metric, span, or log line changes.
`logWholeScopeRetractSkipped` keeps its message, its four fields, and its
firing condition; it moved call site, not behaviour, and the nil-fence test
still asserts the warning text on every narrowed domain.

## Review follow-up — the table was never compared to the fence

The table's rows are exactly the domains `domainHasRepoWideRetract` fences, and
that was the whole basis for calling it a mirror of the fence — but the two sets
were enumerated in different packages and nothing compared them. An eighth
fenced domain left out of the table never reaches `narrowedWholeScopeRepoIDs`
or the table, so none of the three guard tests iterate over it; on an unmarked
legacy per-edge row it binds the batch-wide repository list to a
whole-repository DELETE, the #6166 over-delete, with every test in the file
green. (A domain added to the fence with no `buildRetractStatement` case fails
loudly at the builder's `default`, so the silent direction is the one worth a
test.)

The fence is now written down once. `domainHasRepoWideRetract` reads a
`repoWideRetractDomains` map instead of a `switch`, and the new exported
`RepoWideRetractDomains()` returns that same map's keys sorted, so the predicate
and the accessor cannot disagree.
`TestWholeScopeRetractDomainsCoversFencedSet` compares the accessor's set with
`wholeScopeRetractDomains` in both directions.

| Mutation | subs | `go vet` | test | What reddened |
| --- | ---: | ---: | ---: | --- |
| eighth domain added to the reducer fence, not to the table | 1 | 0 | 1 | `…CoversFencedSet`: "mutation_probe_domain is fenced by the reducer's repo-wide retract but absent from wholeScopeRetractDomains" |
| `reducer.DomainShellExec` row deleted from the table | 1 | 0 | 1 | `…CoversFencedSet`: "shell_exec is fenced … but absent from wholeScopeRetractDomains" |
| table row added for a domain the reducer does not fence | 1 | 0 | 1 | `…CoversFencedSet`: "wholeScopeRetractDomains lists mutation_probe_domain, which the reducer does not fence" |

Verification after the final edit, exit codes captured directly:

```
go vet ./internal/storage/cypher/ ./internal/reducer/                    exit 0
go test ./internal/storage/cypher/ ./internal/reducer -count=1           exit 0
ESHU_PERFORMANCE_EVIDENCE_BASE=origin/main scripts/verify-performance-evidence.sh   exit 0
```

Benchmark Evidence: the predicate changed shape, so it was measured rather than
assumed. A throwaway shim in the reducer package benchmarked the map lookup
against the switch it replaced over a six-domain mix (three fenced, two
unfenced, one unknown), `-benchtime=2000000x -count=5` on darwin/arm64, Apple
M4 Pro: map 6.65-7.22 ns/op, switch 1.59-2.13 ns/op, so about +5 ns per call.
Both call sites are per-partition-batch, not per-row —
`shared_projection_worker.go` calls it once per partition batch and
`buildRepoWideRetractRefreshIntents` once per materialization pass — against
batches that each do Postgres round-trips and graph writes in the millisecond
range. The same shim asserted the map and the switch agree on every domain in
`AllDomains()`, and that `RepoWideRetractDomains()` returns only domains the old
switch fenced; it was deleted after the measurement.

No-Observability-Change: the review follow-up adds no metric, span, or log line,
and changes no logging condition.

Refs #6276
Refs #6261
