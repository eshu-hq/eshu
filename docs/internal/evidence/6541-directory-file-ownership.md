# 6541: does the directory seek still decide file ownership?

Companion to [6541-directory-query-s2.md](6541-directory-query-s2.md), which
carries the shape, the measurements and the backend defects. This file answers
one review finding (P1-A on PR #6700): the #6541 statement admits a row by
`d.repo_id` and counts the files CONTAINS-linked to that directory without
re-checking any File, so does a torn or stale projection let it count a file the
caller was not granted?

Three things were established from the projector source.

**A File carries its own owning reference.** `f.repo_id` is written by all six
canonical File statements (`canonical_node_cypher.go:166-259`), so an
`f.repo_id = rid` re-check is available with no schema change.

**`Directory.repo_id` is not write-once.** Directory identity is `path` alone --
`MERGE (d:Directory {path: row.path}) SET d.repo_id = row.repo_id`
(`canonical_node_cypher.go:144`) -- so `repo_id` is a mutable property on a node
any projection whose file paths reach that directory may re-point.

**The phases do commit separately.** `buildPhases` orders `directories` ->
`directory_edges` -> `files` (`canonical_node_writer.go:309-311`), and the
production phase-group executor runs one `ExecutePhaseGroup` per phase
(`canonical_node_writer.go:175-210`), which is the path
`canonical_node_cypher.go:122-143` names as the production projector's. Only the
atomic `GroupExecutor` path puts all node phases in one transaction. So a reader
can see a committed new `repo_id` on a directory still holding the previous
generation's CONTAINS edges: the prune that clears them
(`canonicalNodeRefreshCurrentDirectoryFileEdgesCypher`,
`canonical_node_cypher.go:82`) is keyed on the CURRENT generation's file paths
only. The review's phase-ordering claim is correct.

**What that does and does not allow.** It cannot disclose a file: the statement
returns Directory rows plus `count(f)`, never a File's id, name or path. It can
inflate `file_count`. The statement this PR removed reached those same files
through the same File-to-Directory CONTAINS hop -- its grant landed on a
Repository found by walking OUT of `d`, so a file miscounted under `d` was
miscounted there too -- so the count exposure is not introduced by this change.
Closing it needs `f.repo_id` as a second predicate on a measured hot-path
statement, which needs its own plan profile and corpus measurement and is not
taken here.

**One real ownership defect was found and fixed.** A file fact whose
`relative_path` climbs out of the repository root used to take the directory
chain with it: `qualifyPath` only concatenates while `path.Dir` cleans, so
`../beta/src/leak.go` under `/repos/alpha` produced the directory
`/repos/beta/src`, and `buildDirectoryChain` created that sibling repository's
directories stamped with `repo-alpha` and linked `repo-alpha`'s file to them.
Both halves break a grant: the sibling's directory is re-pointed away from the
caller granted it and counted for the caller granted alpha.
`isRepositoryLocalRelativePath` (`canonical_codegraph_extract.go`) now skips such
a row, matching the existing empty-`relative_path` skip. Production discovery
cannot emit such a path -- every `relative_path` it writes comes from
`filepath.Rel` against the repository root
(`go/internal/collector/discovery/filesystem_walk.go`) -- so this is a guard on a
malformed or hostile fact, not a reproduction of an observed run.

RED then GREEN, `go/internal/projector`:

```
go test ./internal/projector -run 'TestCanonicalDirectoryChainStaysInsideTheRepositoryRoot|TestCanonicalFileRowsShareTheirDirectoryRowsRepositoryID' -count=1
```

Before the guard: `--- FAIL: TestCanonicalDirectoryChainStaysInsideTheRepositoryRoot`,
naming `/repos`, `/repos/beta` and `/repos/beta/src` as directories outside
`/repos/alpha` carrying `repo_id "repo-alpha"`, and the file linked to
`/repos/beta/src`; exit 1. After: both PASS, exit 0. The second test is the
positive pin -- every File the projector emits shares its Directory's `repo_id`
-- and it passed on both sides, which is what says the first test's failure was
the escape and not the invariant generally.

No-Regression Evidence: no Cypher text, anchor, index dependency, parameter or
fan-out changed for this finding. `buildDirectoryCypher`'s statement is
byte-identical (`cypher_shipped_text_test.go` frozen baselines pass unchanged),
and the queryplan `source_sha256` for it does not move because
`manifestSymbolSource` parses without `parser.ParseComments`
(`go/internal/queryplan/source_validation.go:92-119`), so a doc comment is
outside the digest. The projector guard is one `path.Clean` prefix test per file
fact on a path the same loop already walks with `path.Dir`.
Observability Evidence: unchanged. The route's span (`SpanQueryLanguageQuery`)
and `Handler.logDirectoryRead` are untouched; a skipped file fact leaves no new
signal because it is skipped on the same branch as an empty `relative_path`,
which has never been logged either.
