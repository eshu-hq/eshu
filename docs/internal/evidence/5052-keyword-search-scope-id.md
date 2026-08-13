# #5052 — keyword search returned nothing when the caller used a scope id

## The defect

A caller can name a repository two ways: by its canonical id
(`repository:r_xxxxxxxx`) or by the id of the ingestion scope that owns it
(`git-repository-scope:repository:r_xxxxxxxx`). Scope resolution accepted both
and handed back the owning scope plus the canonical repository id — but the
retrieval request kept whichever id the caller had typed, and that id became the
search anchor.

Retrieval then bounds candidates by comparing that anchor against the repository
identity stored on every document. Two places enforce it, and both hold the
canonical id:

- `searchdocs.Document.RepoID` for the in-memory backend, and
- `eshu_search_index_documents.repo_id` in the persisted BM25 query
  (`d.repo_id = $4` in `buildEshuSearchIndexQuery`).

So a scope-addressed request compared a scope id against a canonical id, matched
no document, and returned an empty result set. The failure was quiet: the
response still advertised the full corpus in `indexed_document_count`, because
that number comes from a separate statement that never applies the anchor. From
the outside it was indistinguishable from an empty index.

## Status: already fixed on main

The fix landed on `main` in **#6076** (`bdfccfa96`), which squash-merged the
three commits of branch `5052-bm25`. The production change is
`semanticSearchCanonicalAnchorRequest` in
`go/internal/query/semantic_search_scope_resolution.go`, which rebinds the
anchor to the resolved canonical repository id before retrieval runs, plus a
fail-closed guard in `resolveScope` so a scope-prefixed id can never survive as
`repositoryID`.

This document exists because that PR shipped without one. Its numbers lived only
in a commit message, with no captured exit code or transcript. Everything below
was re-run against the merged code, not copied forward.

Verified at `4ed13e70e` (`origin/main`), macOS on Apple silicon, Go toolchain
from the repo `go.mod`.

## What the guards prove — including that they can fail

A regression test that still passes with the fix removed guards nothing, so both
guards were mutated and watched go red.

Baseline, all six tests in
`go/internal/query/semantic_search_scope_anchor_test.go`:

```
$ cd go && go test ./internal/query/ \
    -run 'TestSemanticSearchKeyword|TestSemanticSearchScopeIDWithService|TestSemanticSearchResolveScope' -count=1
ok  	github.com/eshu-hq/eshu/go/internal/query	1.151s
BASELINE_EXIT=0
```

### Mutation 1 — remove the anchor rebinding

`req.Scope.RepoID = repositoryID` replaced with `_ = repositoryID`, which is the
pre-fix behaviour:

```
M1_EXIT=1
--- FAIL: TestSemanticSearchKeywordReturnsResultsForAllScopesScopeID
    keyword search returned 0 results for a term present in 2 indexed documents;
    body={"data":{"query":"refund",
      "repo_id":"git-repository-scope:repository:r_payments",
      "anchor":{"kind":"repository","id":"git-repository-scope:repository:r_payments"},
      "results":[], "indexed_document_count":2, ...}}
--- FAIL: TestSemanticSearchKeywordScopeIDWithBothGrantsReturnsCanonicalIDs
--- FAIL: TestSemanticSearchKeywordReturnsResultsForDirectScopeGrant
--- FAIL: TestSemanticSearchScopeIDWithServiceKeepsServiceAnchor
    response repo_id = "git-repository-scope:repository:r_payments",
    want canonical "repository:r_payments"
FAIL
```

Four of the six fail. `TestSemanticSearchKeywordCanonicalRepoIDStillMatches`
keeps passing, which is correct — a canonical-id request never needed rebinding,
and that test is the control proving the fix does not change it.

This is also the first of the two commit-message repros, observed directly: a
two-document corpus with the term in both documents returns `"results":[]`
alongside `"indexed_document_count":2`.

### Mutation 2 — remove the fail-closed guard

The `strings.HasPrefix(requestedID, semanticSearchIngestionScopePrefix)` early
return deleted from `resolveScope`:

```
M2_EXIT=1
--- FAIL: TestSemanticSearchResolveScopeNeverReturnsScopeIDAsCanonicalRepository
    --- FAIL: .../grant_names_the_scope_id_as_both_a_scope_and_a_repository
        resolveScope() repositoryID = "git-repository-scope:repository:r_payments",
        want a canonical repository id or empty; a scope id here rebinds the
        anchor to an identity no document carries
FAIL
```

Exactly the intended subtest, and only that one.

### Restored

```
REVERT_M2_EXIT=0
RESTORED_EXIT=0
ok  	github.com/eshu-hq/eshu/go/internal/query	5.311s
```

## The persisted-SQL half

The Go tests cover the in-memory enforcement point. The second one lives in SQL,
so it was reproduced against a real database: PostgreSQL **16.14**
(`postgres:16`, Debian build, aarch64) in a throwaway container on a private
port, seeded with 94 documents all carrying the canonical repo id, with the term
`refund` in 5 of them.

The query is the text of `buildEshuSearchIndexQuery` with `Anchor.Kind =
ScopeKindRepo`, run twice — once with the anchor bound to the canonical id (what
the fix does) and once bound to the scope id (what the bug did):

| anchor binding | `results` | `indexed_document_count` |
| --- | ---: | ---: |
| `repository:r_payments` (canonical) | **5** | 94 |
| `git-repository-scope:repository:r_payments` (scope id) | **0** | 94 |

```
RUN_A_EXIT=0   canonical -> results 5,  indexed_document_count 94
RUN_B_EXIT=0   scope-id  -> results 0,  indexed_document_count 94
```

The `indexed_document_count` staying at 94 in both is the silent part of the
bug, reproduced: the corpus number and the result set disagree because they come
from different statements and only one of them applies the anchor.

## What this evidence does not cover

Stated plainly, because the gap matters more than the numbers:

- **The SQL run is hand-bound, not driven through Go.** The query text was
  transcribed from `buildEshuSearchIndexQuery` and its placeholders bound with
  `psql` variables. It proves the behaviour of the query shape; it does not
  exercise the Go code that decides what goes into `$4` and `$7`. The Go tests
  cover that decision, on the other backend.
- **The corpus is synthetic.** Documents come from `generate_series` with
  uniform length and term frequency, so the absolute BM25 scores mean nothing.
  Only the 5-versus-0 gate is being asserted.
- **The scratch schema is a subset of the real migration.** `ingestion_scopes`
  and `scope_generations` were reduced to the columns the foreign keys need, and
  the `fact_records` content-hash backfill and `term_key` backfill blocks in
  `003b_eshu_search_index.sql` were skipped as irrelevant to the predicate under
  test.
- **No end-to-end run.** The original report — a real repository indexed, then
  keyword search through the API or MCP returning nothing — was not reproduced.
  No Eshu stack was started. Both repros here are below that level.
- The PostgreSQL version matching the 16.14 named in the original commit message
  is a coincidence of the `postgres:16` tag; the version string above is the one
  actually reported by the container.

No-Regression Evidence: this is a correctness fix, not a performance change. The
rebinding is a string comparison and an assignment on a path that already did a
scope lookup; the fail-closed guard removes one database round trip on the
all-scopes miss rather than adding one. No latency claim is made or measured
here.

Observability Evidence: `semanticSearchCanonicalAnchorRequest` sets the span
attribute `search.anchor_rewritten_to_canonical_repository`, so an operator
looking at a search trace can tell which id form the caller used and whether the
anchor was rewritten. That attribute is what distinguishes the two caller shapes
when a search comes back empty for a real reason.
