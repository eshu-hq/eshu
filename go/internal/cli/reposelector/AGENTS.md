# AGENTS.md — internal/cli/reposelector guidance for LLM assistants

## Read first

1. `doc.go` — the package contract: what a selector is, and why identity
   fields and path fields are matched differently.
2. `README.md` — ownership boundary, the exported surface and why the matcher
   internals are not part of it, and the two pre-existing behaviours that look
   like bugs and are not yours to fix here.
3. `go/cmd/eshu/repository_selector.go` — the cobra wrapper. It reads the
   `--repo` / `--repo-id` flags that each command file declares for itself
   (`analyze.go:96`, `analyze.go:315`), and owns the `--repo-id`
   short-circuit that skips resolution entirely. It does not declare the flag
   names; adding a selector flag to a new command means declaring it in that
   command's file.
4. `go/cmd/eshu/AGENTS.md` — the wrapper's package rules, including why
   command logic lives out here at all (epic #6053, issue #6059).

Do not confuse this package with `go/internal/query`'s own
`resolveRepositorySelector`. That one resolves an HTTP path parameter
server-side against the graph. Same concept, different code, different owner.

## Invariants this package enforces

- **Identity fields match exactly; path fields are canonicalized.** `ID`,
  `Name`, and `RepoSlug` compare byte for byte. Only `Path` and `LocalPath`
  go through `filepath.Clean` and symlink resolution.
  `TestRepositorySelectorCanonicalizesOnlyPathFields` fails if a name that
  looks like a path starts matching a path.
- **Ambiguity is an error.** `Resolve` returns the sorted matching IDs rather
  than picking one, so the CLI can never report on a repository the operator
  did not name.
- **Duplicate IDs in one listing collapse.** The `seen` set means a repository
  returned twice does not read as ambiguous.
- **The package stays process-neutral.** No cobra, no environment, no process
  stream, no subprocess, no file write.
  `TestPackageStaysCobraAndEnvFree` in `doc_lockstep_test.go` pins the direct
  import set and the `os` / `fmt` selector sets as set equalities, so a new
  import or a new `os` call fails until the README sentence it widens is
  revisited too.

## Common changes and how to scope them

- **Add a new selector form** (a new field, a new normalization) → add it to
  `matcher.matches` or `pathMatches`, extend `Entry` if it needs a new wire
  field, and update the identity-vs-path sentence in `doc.go` and `README.md`
  in the same change. A new form that canonicalizes an identity field is a
  contract change, not a tweak.
- **Change how the listing is fetched** (paging, a `limit`, honouring
  `Total`) → this is a real behaviour change with an operator-visible effect,
  not a refactor. It needs its own issue, a regression test that fails first,
  and the `Resolve` gotcha in `README.md` rewritten. Do not fold it into an
  unrelated change.
- **Need something from `package main`** → inject it as a parameter the way
  `Getter` is injected. Nothing here may import `go/cmd/eshu`, and nothing
  here may read a flag.

## Anti-patterns specific to this package

- **Exporting a matcher internal.** `matcher`, `newMatcher`, `matches`, and
  `pathMatches` have no caller outside this package, and the test is
  in-package. "Exported so the test can reach it" is not a justification here
  and has been a review finding on sibling extractions.
- **Widening `Getter`.** It is one method because one method is what this
  package calls. Adding `Post` or `GetEnvelope` because the concrete
  `*APIClient` happens to have them puts a method on the interface that
  nothing here uses.
- **"Hardening" the symlink call.** `filepath.EvalSymlinks` reading the real
  filesystem is the feature, not an oversight. A test that depends on symlink
  resolution succeeding will be flaky in a sandbox; write it against the
  cleaned-path branch instead.
- **Formatting an error for a terminal.** Errors returned here are wrapped by
  the caller and rendered by the wrapper. Nothing in this package writes to
  stdout or stderr.

## What NOT to change without an ADR

- The `resolve repo selector %q: ...` error prefixes. They are what an
  operator sees, and `TestRunAnalyzeDeadCodeFailsOnAmbiguousRepoSelector`
  (`go/cmd/eshu/analyze_test.go:287`) asserts the ambiguous-match string
  verbatim, so changing the wording fails that test.
- The exact-vs-canonicalized split between identity and path fields. Widening
  it changes which repository an existing selector resolves to, silently, for
  every operator script already using one.
