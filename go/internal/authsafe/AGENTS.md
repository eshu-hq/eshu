# AGENTS — internal/authsafe

Scoped rules for the shared sign-in request-value checks. Load
`golang-engineering`.

## Invariants

- **Everything here is a pure function.** No state, no configuration, no
  network, no store. A connector must be able to call it without wiring, and
  the whole surface must be coverable by a table test. If a check needs a
  provider or a config, it belongs with its connector.
- **No dependencies beyond the standard library.** This package is imported by
  `internal/query`, `internal/githublogin` and `internal/oidclogin`; giving it
  dependencies pulls them into all three.
- **Never re-copy a check out of here.** The entire reason this package exists
  is that the return-path check had byte-identical copies spread across the
  three packages above, so any tightening needed an edit in every one of them
  (#5388). If a caller needs a variant, add a parameter or a second function
  here — do not fork it locally.
- **Do not write the copy count anywhere.** It has been wrong twice: the
  extraction first found three copies, a review found a fourth
  (`safeOIDCReturnPath`), and then this file still said three after the others
  were corrected. The number is not the invariant; "this is the single copy"
  is. If you need to check, sweep for the shape rather than the names:

  ```
  rg -n 'HasPrefix\(path, "//")' --glob '!*_test.go' go/
  ```

  That should return exactly one hit — `returnpath.go`.
- **A rejection is the empty string, always.** Callers substitute their own
  configured default. Do not return a "safe" fallback path from here: this
  package does not know what a given flow's default landing page is, and
  inventing one would send users somewhere the connector did not choose.

## When changing `ReturnPath`

It is an open-redirect check, so loosening it is a security change, not a
refactor. Each rejection in its doc comment names the escape it closes; if you
remove one, say which escape you are re-opening and why it is covered elsewhere.

The `..` case is a documented non-goal with a test pinning today's behaviour
(traversal is allowed and stays in-origin). Tightening it is a legitimate
change — it is what #5388 flagged as possible future work — but it must update
that test deliberately rather than incidentally, and every caller inherits it at
once, which is the point.
