# authsafe

Shared request-value checks for sign-in flows. Pure functions, no dependencies,
no state.

## Why this package exists

A sign-in flow takes its post-login destination from the request. That value is
attacker-controlled, and an unchecked one is an open redirect: send a victim to
the real login page with a return path pointing at your host, let them
authenticate for real, and the redirect delivers them somewhere hostile carrying
the login flow's own credibility.

The check for that was correct but copied three times — `safeGitHubReturnPath`
in `internal/query`, and `safeReturnPath` in each of `internal/githublogin` and
`internal/oidclogin`, byte-identical. Three copies of a security check is the
shape where one gets tightened and the others quietly do not
([#5388](https://github.com/eshu-hq/eshu/issues/5388)).

## Surface

| Function | Purpose |
| --- | --- |
| `ReturnPath(path string) string` | Returns `path` when it is safe to redirect to after sign-in, `""` when it is not. Callers treat `""` as "use the configured default landing path". |

`ReturnPath` rejects an empty or whitespace-only value, anything not starting
with `/` (absolute URLs and non-HTTP schemes), a leading `//` (which a browser
reads as a host, not a path), and any value containing CR, LF or TAB (header
injection).

It does **not** resolve `..`. A path like `/app/../admin` stays inside this
origin — the browser normalises it before the request — so traversal is the
router's concern. That is a deliberate boundary, pinned by a test, not an
oversight. If the `..` tightening #5388 suggested ever lands, it lands here once.

## Adding to this package

Something belongs here when more than one sign-in flow needs it AND it is a
pure function of a request value. Anything needing configuration, a store, or a
provider belongs with its connector instead.
