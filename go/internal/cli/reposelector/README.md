# reposelector

## Purpose

Repository-selector matching for the `eshu` CLI. An operator names a
repository by whatever they have to hand — the canonical ID, the repository
name, an `org/name` slug, or a filesystem path — and this package turns that
into the canonical repository ID the API keys everything else by.

Two entry points, because two different callers need it:

- `Resolve(client, selector)` fetches the repository listing and returns the
  single matching ID. `eshu vuln-scan repo` and every `--repo`-flagged command
  reach it through `go/cmd/eshu/repository_selector.go`.
- `Matches(entry, selector)` is the same rule applied to one already-fetched
  entry. `eshu first-run` and `eshu hosted setup` hold the listing already and
  only want the predicate.

`ListResponse` and `Entry` are the `/api/v0/repositories` wire shapes both
paths decode. Two files in `go/cmd/eshu` decode `ListResponse` themselves
rather than going through `Resolve`: `first_run.go` and `hosted_setup_cmd.go`,
which each need the whole listing, not one ID.

## Ownership boundary

This package owns matching and the listing read. It does not own anything
cobra-shaped: flag names, flag reading, the `--repo-id` short-circuit that
skips resolution entirely, streams, and exit codes stay in
`go/cmd/eshu/repository_selector.go`, which is `package main` and therefore
cannot be imported from here.

It does not own the server-side selector either. `go/internal/query` has its
own unrelated `resolveRepositorySelector` for HTTP path parameters; the two
share a name and a concept, not code.

## Exported surface

See `doc.go` for the godoc contract. The whole caller-facing surface is
`Getter`, `ListResponse`, `Entry`, `Resolve`, and `Matches`.

The matcher internals — `matcher`, `newMatcher`, its `matches` method, and
`pathMatches` — stay unexported. Nothing outside this package calls them, and
`reposelector_test.go` is an in-package test, so it reaches them without an
export.

`Getter` is declared here, at the point of use, rather than shared from
`go/cmd/eshu`: that package is `package main`, and its `*APIClient` resolves
the service URL, API key, and profile from cobra flags, the environment, and
the on-disk config. The wrapper resolves all of that and passes the built
client in. `*APIClient` satisfies `Getter` as written, and the pattern matches
`admin.Client`, `scan.Client`, `trace.EnvelopeFetcher`, and
`docs.EnvelopeGetter`.

## Dependencies

Standard library only: `fmt`, `path/filepath`, `slices`, and `strings`. No
intra-repo import, no cobra, no environment read, no process stream, no
subprocess. `TestPackageStaysProcessNeutral` in `doc_lockstep_test.go` pins
that as a set equality, so a new import fails the guard until the sentence
above is revisited too.

## Telemetry

None. Resolution runs inline with a single CLI invocation and reports through
its returned error, which the wrapper maps to the operator's exit code.

## Gotchas / invariants

- **`Resolve` reads one page and ignores `Total`.** The request is
  `GET /api/v0/repositories` with no `limit`, so the server's default page
  size applies (100, see `go/internal/query/repository_list_page.go`) and the
  decoded `Total` is never consulted. A repository past the first page
  resolves as `no matching repository`. This is pre-existing behaviour, moved
  verbatim; do not write a test asserting the listing is complete.
- **Symlink resolution touches the real filesystem.** `newMatcher` calls
  `filepath.EvalSymlinks` on the selector and, when that succeeds, on each
  candidate path. This is deliberate — it is what lets an operator name a
  repository by the path they are standing in when that path reaches the
  checkout through a symlink — but it makes matching host-dependent. Tests
  must not depend on symlink resolution succeeding.
- **Identity fields are matched exactly, path fields are canonicalized.**
  `ID`, `Name`, and `RepoSlug` compare byte for byte; only `Path` and
  `LocalPath` go through `filepath.Clean` and symlink resolution. A repository
  whose `Name` looks like a path must not be canonicalized into matching a
  different path — `TestRepositorySelectorCanonicalizesOnlyPathFields` pins
  it.
- **An ambiguous selector is an error, not a pick.** Multiple matches return
  the matching IDs sorted, so the operator can re-run with an exact one.
  Silently taking the first match would let the CLI report on a repository the
  operator never named.
- **A `nil` client is an error, not a panic** — but only for a nil `Getter`
  interface. A typed-nil `*APIClient` passed in as a `Getter` is non-nil to
  this package and would panic in `Get`; the wrapper owns building a real
  client, and `apiClientFromCmd` never returns nil.

## Related docs

`go/cmd/eshu/AGENTS.md` covers the wrapper side. `docs/public/reference/cli-reference.md`
documents the `--repo` / `--repo-id` flags operators actually type.
