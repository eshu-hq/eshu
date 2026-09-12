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
non-HTTP scheme, a bare relative path with no leading `/`, a path carrying a
backslash (`/\evil.test` — WHATWG URL parsing treats `\` as `/` in http(s)
URLs, so this resolves to host `evil.test` exactly like `//evil.test` does),
and any path carrying CR, LF or TAB (header injection).

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

## Security fix: the `/\` open-redirect bypass

`ReturnPath` rejected a leading `//` but not a leading `/\`. WHATWG URL
parsing treats `\` as `/` in http(s) URLs, so a browser resolves
`/\evil.test` to host `evil.test` exactly like `//evil.test` — an
unauthenticated attacker's `?return_to=/%5Cevil.test` link redirected a
victim off-site after completing SAML, OIDC, or GitHub sign-in, borrowing
Eshu's login credibility for a phishing landing page. Fixed by adding `\` to
the existing `strings.ContainsAny` character-class check in `returnpath.go`
— a one-character validation tightening, not a new code path.

No-Regression Evidence: every existing `ReturnPath`/`SAMLHandler`/
`OIDCLoginHandler`/`GitHubLoginHandler` return-path test still passes
unchanged; the fix only narrows what a caller-supplied value may contain and
touches no other branch. New regression coverage: `TestReturnPath`'s
backslash rows in `returnpath_test.go`; `TestSAMLHandlerLoginRejectsMaliciousReturnTo`'s
new `/\evil.example.com/steal` case and `TestSAMLHandlerACSRejectsBackslashReturnToPath`
(the legacy-stored-row case, since `ConsumeSAMLRequest` can still return a
pre-fix value and the redirect-time re-check must catch it); the equivalent
start- and callback-side pairs for OIDC
(`TestOIDCLoginHandlerStartRejectsBackslashReturnTo`,
`TestOIDCLoginHandlerCallbackRejectsBackslashReturnToPath`) and GitHub
(`TestGitHubLoginHandlerStartRejectsBackslashReturnTo`,
`TestGitHubLoginHandlerCallbackRejectsBackslashReturnToPath`) in
`go/internal/query`. All confirmed failing before the fix and passing after.

No-Observability-Change: this package still emits no metric, span, or log
(see Telemetry above); the fix changes only which values `ReturnPath`
accepts, not what it reports.

## Related docs

- `doc.go` — the godoc contract for `ReturnPath`.
- `AGENTS.md` — scoped agent instructions for this directory.
- Issue #5388 — why the check was extracted, and the tightening it anticipates.
