# authsafe

## Purpose

Holds the check that decides whether a post-login redirect destination is safe.

It exists because that check used to exist four times, byte for byte:
`safeGitHubReturnPath` in `internal/query`, `safeOIDCReturnPath` in
`internal/query`, and `safeReturnPath` in each of `internal/githublogin` and
`internal/oidclogin`. Four copies of one security check is the shape where
somebody tightens one and the other three quietly stay loose — and #5388 already
names a candidate tightening (rejecting `..`), which would have meant finding
and editing all four.

## Ownership boundary

Owns the rule for a *relative redirect target*, nothing else. It does not
authenticate, does not decide who may log in, and does not know which provider
is asking.

Path traversal is deliberately outside that boundary. A path containing `..`
still resolves inside this origin, so it is the router's concern; see the
non-goal pinned in `returnpath_test.go`.

## Exported surface

`ReturnPath(path string) string` — returns the path when it is a safe
same-origin redirect, and `""` when it is not. Callers treat `""` as "no
redirect", never as an error.

It rejects: an absolute URL, a protocol-relative host (`//evil.test`), a
non-HTTP scheme, a bare relative path with no leading `/`, and any path carrying
CR, LF or TAB (header injection).

## Dependencies

`strings` only. Nothing in this package imports another Eshu package, which is
what lets all six call sites share it without an import cycle — the three
consumers are peers, so the check cannot live in any one of them.

## Telemetry

None. `No-Observability-Change:` this package emits no metric, span or log; a
rejected path is reported to the caller as `""` and the calling handler owns
whatever it records about the request.

## Gotchas / invariants

- **`""` is a value, not a failure.** A caller that treats it as an error will
  turn a hostile redirect into a 500 instead of a normal sign-in.
- **The rule is one function on purpose.** Adding a second entry point here —
  a "lenient" variant, a per-provider override — recreates the drift the
  package was made to remove.
- **The golden-corpus filter excludes this package** with a stated reason: the
  gate authenticates with a static `ESHU_API_KEY` and never performs a sign-in,
  so this code is unreachable from it. See
  `scripts/lib/golden-corpus-filter-exclusions.txt`.

## Related docs

- `doc.go` — the godoc contract for `ReturnPath`.
- `AGENTS.md` — scoped agent instructions for this directory.
- Issue #5388 — why the check was extracted, and the tightening it anticipates.
