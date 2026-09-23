# Query Package Moves Evidence (#6818)

Issue #6818 removes stuttering package names under `go/internal/query` by
moving packages to plain names. Each move rewrites import paths and package
qualifiers in the files that import the moved package. Some of those importers
are "hot" to `scripts/verify-performance-evidence.sh` because they contain
Cypher or goroutine fan-out, even though the move leaves their behavior
unchanged. This note records that evidence, with one section per move. Later
#6818 moves append a section here.

## `query/queryspan` to `query/tracing`

Baseline and after states are named by git tree and blob hashes, not by
commit. Those hashes come from content, so they stay the same when the branch
is rebased or squashed, and anyone can resolve them with `git cat-file`. The
old package is still on `origin/main`, so its hashes can be re-derived there.
Get the after hashes from the merged commit with `git rev-parse
<commit>:<path>`.

| State | Path | Object |
| --- | --- | --- |
| Before (tree) | `go/internal/query/queryspan` | `d4b0105f3919847a95c153481a5b96f9bddc713e` |
| After (tree) | `go/internal/query/tracing` | `4c63aa1d588144128c66bb6a07821478b790e93e` |
| Before (blob) | `go/internal/query/queryspan/handlerspan.go` | `8e433940753f43cc6de10ee131ba1aece051e34d` |
| After (blob) | `go/internal/query/tracing/handler.go` | `91e9df160dd12981bacb38a5e22e56bb1fcf47e2` |
| Before (blob) | `go/internal/query/impact/contract.go` | `06f23775ca861a26a4ade98ae8f212e58044237a` |
| After (blob) | `go/internal/query/impact/contract.go` | `85394ab62e44f16c25efbbe05b2cad8ac8b4e4d3` |
| Before (blob) | `go/internal/query/impact/resource_investigation.go` | `2c8a0487eb739d7927d0a9d6e9b7a8ffdfc4b36b` |
| After (blob) | `go/internal/query/impact/resource_investigation.go` | `9d18fc5dcfda8e15c02cb5c38ac28a74532a37d5` |

The before hashes are the same at the pre-rebase merge base `9e542becc` and at
the current merge base with `origin/main`. The move commits cited in earlier
drafts (`d2aeb0589`, `c594ca6ed`, `af9e66c82`) are pre-rebase references and
are not on the branch. No backend or version applies. The change is
compile-time only: no query runs differently, and no Postgres or NornicDB work
is involved.

The gate flagged two importers as hot:

- `go/internal/query/impact/contract.go` is hot for its Cypher text (the
  `MATCH (provider:Repository ...)-[:EXPOSES_ENDPOINT]->(endpoint:Endpoint)`
  read).
- `go/internal/query/impact/resource_investigation.go` is hot for its bounded
  goroutine fan-out (`sync.WaitGroup`, `chan error`, three `go func()`).
  It has no Cypher text.

In both files the diff changes two lines: the import
`github.com/eshu-hq/eshu/go/internal/query/queryspan` becomes
`.../query/tracing`, and the `queryspan.` qualifier on
`StartHandlerSpanWith` and `HandlerTracer` becomes `tracing.`.

No-Regression Evidence: the Cypher text and the concurrency code are
unchanged. The proof applies the substitution to each before blob and checks
that the result hashes to the after blob:

```bash
check() { # check <before-blob> <after-blob>
  git cat-file -p "$1" \
    | sed 's#query/queryspan"#query/tracing"#; s/\([^A-Za-z0-9_]\)queryspan\./\1tracing./g; s/^queryspan\./tracing./g' \
    | git hash-object --stdin | cmp -s - <(printf '%s\n' "$2")
}
check 06f23775ca861a26a4ade98ae8f212e58044237a \
  85394ab62e44f16c25efbbe05b2cad8ac8b4e4d3 # contract.go: exit 0
check 2c8a0487eb739d7927d0a9d6e9b7a8ffdfc4b36b \
  9d18fc5dcfda8e15c02cb5c38ac28a74532a37d5 # resource_investigation.go: exit 0
```

That leaves no other changed byte, so no Cypher line and no goroutine,
channel, or WaitGroup line changed. A second check used `go/scanner` to
extract every string literal outside the import block, from both the base and
head versions of each file. It found the same list both times: 113 literals
in `contract.go` (sha256 prefix `a2ffd812bb2ee4ec` on both sides) and 38 in
`resource_investigation.go` (`fef564adc3f0b6ff` on both sides). The Cypher
statement is one of those literals, so the query sent to the graph is the
same. `go test ./internal/query/impact/... ./internal/query/tracing/...
./internal/query/language/... -count=1` passed (368 tests, exit 0). The
concurrency shape, fan-out width, and error channel capacity are unchanged, so
no performance measurement applies.

No-Observability-Change: the package move leaves the span name, attributes,
and tracer name unchanged. `tracing/handler.go` differs from
`queryspan/handlerspan.go` only in its package clause. `git diff
d4b0105f3919847a95c153481a5b96f9bddc713e
4c63aa1d588144128c66bb6a07821478b790e93e` compares the two package trees
directly: the Go files differ only in the package clause and the godoc lead,
and the Markdown files only in the package name, the renamed file, and a note
recording the move. The tracer is still
`otel.Tracer("eshu/go/internal/query")`, and the span still carries
`http.route`, `eshu.capability`, and `service.namespace`. Saved span queries
and dashboards that match on these names still work. The
`docs/public/observability/telemetry-coverage.md` row now points at
`handler.go`.

## Performance and observability evidence for the `selector` leaf

No-Regression Evidence: six files move from `queryselector/` to
`selector/` (four Go files plus the `doc.go`/`README.md`/`AGENTS.md`
trio), 45 importer files repoint the import path and the `queryselector.`
qualifier to `selector.`, and 11 of those importers rename a
`selector`-named parameter or local to `rawSelector` where it would
otherwise shadow the new package qualifier. No call site, argument,
allocation, or loop bound changes.

Before tree `04a017c57463c9697fe0a8346003b5208437a21a`
(`9cf27f072:go/internal/query/queryselector`), after tree
`57c21635d3ff595bb75cc9d0e0f4770c9c5ca516`
(`HEAD:go/internal/query/selector`). Moved-file blob pairs
(before → after):

- `doc.go`: `3bf21fd1` → `ba207755` (package clause plus the godoc
  `Package queryselector` lead line only)
- `selector.go`: `4b6b97a0` → `988a1e80` (package clause only)
- `entity_repo_identity.go`: `2a177fe0` → `2c8401a0` (package clause only)
- `entity_repo_identity_test.go`: `aaf3531b` → `92abee14` (package clause only)
- `AGENTS.md`: title line only; `README.md`: byte-identical.

Importer substitution proof, base `9cf27f072`, run from the worktree root:

```bash
git diff --name-only 9cf27f072 HEAD -- '*.go' \
 | while read f; do git cat-file -e "9cf27f072:$f" 2>/dev/null && echo "MOD $f"; done \
 | while read cls f; do
     rg -q rawSelector "$f" && continue
     git show "9cf27f072:$f" | sed 's/queryselector/selector/g' | cmp -s - "$f" && echo "CLEAN $f"
   done
```

32 importer files are byte-exact under `s/queryselector/selector/g`.
Two more (`codequery/search_authz_test.go`, `repository/authz_test.go`)
match under the same sed plus one gofumpt import-line resort each
(`query/selector` sorts after `query/querytestutil`, while
`query/queryselector` sorted before it). The remaining 11 importer
files (including `repository/selectors.go`, both `repository_selector.go`
families, `contentread/content_handler.go`, `iac/handler.go`, and five
`supply/chain` scopes) carry the extra `selector` → `rawSelector`
parameter rename; every changed line in their diffs contains the
`selector` or `rawSelector` token, so no other byte changed.

`scripts/verify-performance-evidence.sh` reports no hot
Cypher/concurrency/runtime files changed, so no benchmark applies: the
Cypher text inside the moved files is untouched and the call graph is
identical up to the qualifier rename. The two `query-source-coverage.yaml`
rows for the moved files rename only the `file:` key (`queryselector/` →
`selector/`); their `source_sha256` pins hash the extracted symbol source,
which excludes the package clause, so they stay valid. One hot-callsite pin
outside the moved files IS touched: `entity/handler.go:(*Handler).ResolveEntity`
calls through the renamed qualifier twice inside the function body, so its
symbol-source digest moves `91a04b4e…` → `07158242…`. The refresh is
qualifier-only (the function diff shows exactly two `queryselector.` →
`selector.` token changes and nothing else), the new digest was recomputed
independently with `go/ast` extraction plus sha256 (matching the
`manifestSymbolSource`/`sourceNodeText` method), and the full
`go test ./internal/queryplan/ -count=1` plus
`scripts/verify-query-plan-regression.sh` pass with no other pin drifting. `scripts/verify-moved-file-refs.sh` reports 4 vacated
Go paths against base `9cf27f072` with no dangling references (dated
evidence and design docs that name the old path stay pinned via four
new `scripts/moved-file-refs-allowlist.txt` rows), and
`scripts/verify-package-docs.sh` reports the moved doc trio present.
`go build ./...` (exit 0) plus `go test ./internal/query/... -count=1`
(all ok, no failures) prove the repointed call graph still resolves and
behaves.

No-Observability-Change: no span name, attribute, metric, or log line
changes; the move touches no telemetry emission point.
