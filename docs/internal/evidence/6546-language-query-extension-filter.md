# #6546 — language-query extension filter admitted every row on NornicDB

`POST /api/v0/code/language-query` and `execute_language_query` answered a
`language: "go"` request with files of every language on the default backend.
The Directory, File and entity builders in
`go/internal/query/language_query_cypher.go` OR-ed one `f.name ENDS WITH
'<ext>'` term per registered extension into the WHERE of a multi-node MATCH,
and the pinned NornicDB build (`timothyswt/nornicdb-cpu-bge:v1.2.3@sha256:4dfa887d…`)
evaluates ENDS WITH as `true` for every row of such a MATCH, so the predicate
read as `<language test> OR true`. `buildRepositoryCypher` carries no extension
term, so it never admitted every row, but its two equalities
(`f.language = $language OR f.language = $language_title`) never reached the
parser spellings either: a `csharp` repository query answered zero repositories
and a `typescript` one left out every `tsx` file. It binds the same spelling
list now.

Root-Cause Evidence: the issue's bisection on a 180-file corpus (105 `.go`):
`MATCH (f:File) WHERE f.name ENDS WITH '.go'` returns 105 rows and
`MATCH (f:File)<-[:CONTAINS]-(d:Directory) WHERE f.name ENDS WITH '.go'`
returns 180; `f.name CONTAINS '.go'` returns 105 in both shapes; and
`f.name ENDS WITH '.go' AND f.language = 'python'` returns 0 in the one-node
shape and 60 in the two-node shape, which is every Python file. The AND-ed
equality still filtering shows the operator becomes true rather than the WHERE
being dropped. The eight-node live fixture the route's proofs used was entirely
Python, so a predicate admitting every row returned only Python rows and passed.

## Theory proof

Throwaway shim, run before the builder was touched, against a fresh pinned
container: 20 repositories and 200 directories, with 4,000 files (one of six
languages each, 667 Go) and 4,000 functions seeded under the first 14 of them
through the projector's node and edge shapes. Every shape below was executed through `NewNeo4jReader(...).Run`, the
production read path, with `limit` above the corpus size. Rows and wrong rows
are exact; times are the median of three warm runs.

| Shape | File rows / wrong | Directory `sum(file_count)` | Function rows / wrong | File median |
| --- | ---: | ---: | ---: | ---: |
| shipped: `= OR = OR f.name ENDS WITH '.go'` | 4000 / 3333 | 4000 | 4000 / 3333 | 13.2 ms |
| `f.language = $language OR f.language = $language_title` | 667 / 0 | 667 | 667 / 0 | 3.2 ms |
| `f.language IN $languages` | 667 / 0 | 667 | 667 / 0 | 2.8 ms |
| `… OR f.name CONTAINS '.go'` | 667 / 0 | — | — | 5.2 ms |
| `… OR right(f.name, 3) = '.go'` | 667 / 0 | — | — | 2.3 ms |
| single-node pre-filter, `WITH f MATCH (f)<-[:REPO_CONTAINS]-(r)` | deadline (10 s) | — | — | — |

The property predicate is correct in every builder shape and the `IN` list
costs the same as the two equalities warm (Directory, five runs each after the
first: 0.65 ms to 0.85 ms for both). On the first, cold, Directory read the
`IN` form took 55 ms to the equalities' 28 ms and the shipped shape's 96 ms;
that is one sample each and is recorded as such. `IN` was chosen because the
bound list can carry the parser spellings the extension fallback used to reach:
`tsx` for a `typescript` request, `jsx` for `javascript`, and `c_sharp` for
`csharp`, which the C# parser writes and the DSL's canonical name never
matched. The `CONTAINS` form is honoured but unanchored, and the pre-filter
form exceeds the route's graph-read deadline, so neither was pursued.

## Regression proof

`go/internal/query/language_query_mixed_language_nornicdb_live_test.go::TestLiveNornicDBLanguageQueryAdmitsOnlyTheRequestedLanguage`
seeds one repository holding `alpha.go`, `beta.go`, `gamma.py`, `delta.tsx`
(language `tsx`), `epsilon.cs` (`c_sharp`), `zeta.ts` and `eta.hcl`, with one
Function per file, and asks each builder for one language.

Before the change, every File and entity case returned all seven files, the
Directory case counted 7, and the Repository builder, run against the same
fixture with its previous two-equality text, missed the parser spellings:

```text
File go returned files [alpha.go beta.go delta.tsx epsilon.cs eta.hcl gamma.py zeta.ts], want exactly [alpha.go beta.go]
Directory go counted 7 file(s), want 2
Repository csharp returned 0 row(s), want the one seeded repository; zero means the builder missed the parser's spelling: []map[string]interface {}{}
Repository typescript counted 1 file(s), want 2
```

After it, `File go` and `Function go` return 2 rows, `File python` 1,
`File typescript` and `Function typescript` 2 (`delta.tsx`, `zeta.ts`),
`File csharp` 1, `File hcl` 1, `Directory go` counts 2, and `Repository go`,
`Repository csharp` and `Repository typescript` each return the one repository
with `file_count` 2, 1 and 2. The whole
`live_nornicdb_language_imports_grant` suite, grant squeezes included, passes
against the same store (`go test ./internal/query -tags
live_nornicdb_language_imports_grant -run TestLiveNornicDB -count=1`, exit 0).

The shipped text is frozen in
`go/internal/query/auth_scoped_language_query_shipped_text_test.go::TestLanguageQueryUnscopedCypherTextIsFrozen`,
and the spelling list in
`go/internal/query/language_registry_test.go::TestGraphLanguageSpellings`.

No-Regression Evidence: same corpus and same read path as the theory table,
File builder median 13.2 ms shipped against 2.8 ms with `f.language IN
$languages`; the difference is the 3,333 rows the fixed predicate no longer
returns, not a faster executor. The route's `limit`, ORDER BY and deadline are
unchanged. The Repository builder swaps its two equalities for the same `IN`
list; that swap measured 3.2 ms against 2.8 ms on the File builder above, and
on the live fixture the unscoped Repository read answered in 680 µs with the
new text against 676 µs with the previous one (one warm sample each, from the
grant suite's `buildRepositoryCypher unscoped` line and the new
`Repository go` case).

No-Observability-Change: no metric, span, log event, or status field moves.
The route keeps its `query.graph_read.warning` deadline event and the
language-query span; operators diagnose the path as before.

## Follow-ups

- The upstream report to NornicDB with the bisection table is the issue's
  remaining deliverable and is not part of this change.
