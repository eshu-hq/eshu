# Code Seam Export — Closure And No-Regression Evidence (#6060)

This records the measurements behind the code-family seam (#6060 lane A, PR 1 of
3) so a reader does not have to take the counts on trust. All of it was
re-derived on `origin/main` at `2d8258ecc`, not carried over from the issue.

## How the closure was derived

A `go/types` pass over `go/internal/query` using `golang.org/x/tools/go/packages`
with `NeedTypes|NeedTypesInfo|NeedSyntax`, loading the package twice: once with
`Tests: false` and once with `Tests: true`. Both loads report zero package
errors, so no symbol is missing because a file failed to type-check.

For every identifier use, the pass compares the file that declares the object
with the file that reads it, against the 39-file move set. A use crosses the
seam when exactly one side is in the move set. Object kinds are taken from
`go/types`, so a method is distinguished from a function and a struct field from
a package-level variable. Filename prefixes are not used to decide membership:
`code_` names neither guarantee nor exclude a symbol, which is why the pass reads
the type information rather than the file names.

## What it found

| Direction | Count | Note |
| --- | --- | --- |
| Declared in the move set, read by staying production files | 40 | across 15 staying files; `deadCodeIncomingEdge` already has `querycontract.DeadCodeIncomingEdge`, so 39 are new exports |
| Declared in staying root files, read by the move set | 49 | 20 already forward to `querycontract`; 29 do not |
| Receiver/method splits | 2 | both named in the issue decision; there is no third |

The two splits:

- `ContentReader.crossRepoDeadCodeUngrantedConsumers` is declared in
  `code_dead_code_cross_repo_filter.go`, which moves, while its receiver
  `ContentReader` is declared in `content_reader.go`, which stays.
- `structuralInventoryRequest.queryLimit` is declared in
  `content_reader_structural_inventory.go`, which stays, while its receiver
  `structuralInventoryRequest` is declared in `code_structural_inventory.go`,
  which moves.

The issue's stop condition was that a closure showing more root files must move
would mean the seam is bigger than #6530 and the sequence needs re-scoping. The
closure shows two splits and no third, so that condition does not fire.

## Why this seam carries direction-B work when #6530 did not

#6530 exported only the impact family's own symbols. It needed no work in the
other direction because `internal/query/impact` imports just `impacttrace`,
`querycontract` and `queryspan` -- everything the impact family read from root
had already been hoisted by earlier lanes. The same holds for `codeowners` and
`codeshaping`, both of which were chosen as method-free helper sets with no root
coupling.

The `CodeHandler` family is the first code-lane move with real root coupling. A
subpackage cannot import root without a cycle through `type CodeHandler =
<pkg>.Handler`, so exporting a root symbol in place does not make it reachable
after the move. The 29 unhomed symbols are hoisted into leaf packages here, with
root keeping lowercase forwarders so no staying caller changes.

## What this seam does not remove

The moving files still call 103 symbols declared in the six
`family_code_shim*.go` files. Those are compatibility forwarders into
`codemodel`, `codeshaping` and `codeowners`, which earlier lanes already
created, and the issue's sequence deletes the shims with the move rather than
here.

This is worth stating because the obvious summary of this PR -- "the code family
no longer depends on root" -- would be wrong. It depends on root in exactly one
remaining way, through forwarders that are scheduled for deletion and that
already name the package each call site will resolve to after the move. The
move PR therefore carries a 103-site name substitution in addition to the file
move itself.

## No-regression: the query text is byte-identical

Every backquoted Go literal containing a Cypher or SQL keyword was collected
across the whole `go/internal/query` tree at the base commit and at this
branch's head, line comments stripped, whitespace runs normalised, and compared
as a sorted multiset. The check is tree-wide rather than per file on purpose: a
per-file diff cannot show that a statement is unchanged once its declaration has
moved between files.

```
base origin/main 2d8258ecc: 889 literals (760 distinct)
  digest 2865463fa1884fec07075cc7b3c34a113cfa94835c7992883530c271335be1e3
head:                       889 literals (760 distinct)
  digest 2865463fa1884fec07075cc7b3c34a113cfa94835c7992883530c271335be1e3

IDENTICAL -- sorted multiset diff is empty, exit 0
```

This is why `crossRepoDeadCodeUngrantedConsumerProbeQuery` is exported as an
alias rather than relocated alongside the method that reads it. Moving the
statement would have been the tidier-looking change and would have left this
digest unchanged in total, but it would have moved query text between files for
no behavioural reason, which the hot-path evidence gate then has to be told to
ignore. Aliasing keeps the statement in exactly one place.

No statement is added, removed, or reordered, and no anchor, predicate, `LIMIT`
or projection changes. Because the statements are identical against an unchanged
schema, no latency figures are quoted; a measured delta here would be sampling
noise presented as a result.

## Two cross-boundary symbols that do not exist on main

Both structural splits this PR makes create a cross-boundary reference that the
preflight closure could not have found, because neither exists until the split
is made. They are recorded here because "the census was clean" is not evidence
about a tree the census never saw.

`structuralInventoryRequest.queryLimit` was declared in
`content_reader_structural_inventory.go`, beside the three call sites that read
it. Relocating it onto its receiver's own file -- which is what makes the type
and its methods travel together -- moved it away from those callers, which stay.
It is renamed `QueryLimit` at its declaration.

`crossRepoDeadCodeUngrantedConsumerProbeQuery` is declared in
`code_dead_code_cross_repo_filter.go`, which moves. Relocating
`ContentReader.crossRepoDeadCodeUngrantedConsumers` onto its staying receiver
left the statement on the far side of the boundary from its only production
reader. It gets an exported seam alias.

Both were found by re-running the same `go/types` closure against the tree after
the splits, rather than trusting the preflight snapshot. A refactor that
relocates code changes the boundary the closure measures.

## Verification

Every command below postdates the last edit in this branch. Exit codes are
captured directly rather than read after a pipe.

```
go build ./internal/query/...                                  exit 0
go vet   ./internal/query/...                                  exit 0
go test  ./internal/query/... -count=1                         exit 0   15 ok, 0 FAIL
go test  ./cmd/api ./cmd/mcp-server ./internal/mcp ./cmd/eshu  exit 0
go test  ./internal/queryplan -count=1                         exit 0
```

The last line is there because leaving it out hid a real failure. The
queryplan source-coverage manifest pins the SHA256 of a function's SOURCE TEXT,
not of its query literal, so renaming `grant.access` to `grant.Access` inside
`(*LanguageQueryHandler).queryByLanguageWithSemanticFilter` reddened
`TestHotCypherManifestCoversEveryProductionQueryCall` while every other check
above stayed green. Neither `./internal/query/...` nor the cross-package suites
compile that package, and the repository's own "Common checks" list does not
name it either.

That entry is re-pinned here. Only `source_sha256` moved: the disposition
(`class: label_inventory`, `max_results: 200`) is carried across unchanged,
because only an identifier changed and the query literal did not, which the
byte-identical multiset above independently confirms. `queryselector/AGENTS.md`
draws exactly this line -- a re-pin is legitimate once the Cypher is proven
unchanged, and a changed query needs a real audit instead.

Twenty-two entries in that manifest are pinned to `code_*.go` files, so the
later move touches all of them. A `git mv` does not change a function's source
text, so those SHAs should survive and only the `file:` paths need the
subpackage prefix -- the shape lane B's move already produced, as
`impact/entity_map_resolver.go` and `impacttrace/cloud_resource_candidates.go`.

The recursive form is deliberate: a bare `./internal/query` would not compile
the leaf packages this seam depends on. The cross-package line is there because
changing an exported `internal/query` behaviour has broken `cmd/mcp-server` and
`oidcbearer` before.

`go vet ./...` never compiles a build-tagged file, so each tag is vetted
separately. There are 14 tagged `code_*` files carrying 8 distinct tags; a
`code_*_live_test.go` glob finds only 13 of them, missing
`code_grant_clause_attachment_live_seed_test.go`.

```
go vet -tags call_graph_metrics_slo_live        ./internal/query/...   exit 0
go vet -tags live_import_cycle_proof            ./internal/query/...   exit 0
go vet -tags live_nornicdb_call_chain           ./internal/query/...   exit 0
go vet -tags live_nornicdb_complexity_grant     ./internal/query/...   exit 0
go vet -tags live_nornicdb_dead_code_incoming   ./internal/query/...   exit 0
go vet -tags live_nornicdb_relationship_story   ./internal/query/...   exit 0
go vet -tags live_nornicdb_relationships_proof  ./internal/query/...   exit 0
go vet -tags live_story_property_proof          ./internal/query/...   exit 0
```

`gofmt -l go/internal/query` lists `code_call_graph_metrics.go`. That file is
not touched by this branch and is listed identically in a pristine worktree at
`origin/main` 2d8258ecc, so it is pre-existing and is deliberately left alone
rather than swept into this diff.

### A guard that stays untouched

`TestWriteGraphReadErrorCapabilitiesExistInMatrix` failed partway through this
work: `unresolvable WriteGraphReadError capability argument at
code_repository_selector.go:53:6`. A control run of the same test in a detached
worktree at `origin/main` 2d8258ecc passed (`ok ... 1.693s`), and line 53 is
byte-identical in both trees, so the failure was caused by this branch and not
inherited.

The cause was an exported seam forwarder for `applyRepositorySelectorForAccess`.
The sweep resolves a `capability` parameter by tracing every caller of the
enclosing function; the forwarder was a third caller that forwards its own
`capability` parameter and that nothing in production calls, and the sweep skips
test files. One unresolvable caller makes the chain unresolvable.

The fix is on this branch's side of the guard: the function is exported at its
declaration and the forwarder is deleted, which leaves the sweep the same two
resolvable callers it has on main. The guard is byte-identical to `origin/main`.
Widening it to tolerate an uncalled forwarder was considered and rejected: it
would have made the sweep fail open for every zero-caller function in the tree,
permanently, which is a strictly worse outcome than the failure it silences.
