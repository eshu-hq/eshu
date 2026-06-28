# refreshworkflow — Agent Instructions

This package contains the locally-provable tests for the R-6 credentialed
cassette refresh workflow (epic #4102, issue #4108). It has **no runtime code**;
its sole contents are `doc.go`, `README.md`, this file, and
`refreshworkflow_test.go`.

## What lives here and why

The R-6 CI workflow (`refresh-cassettes.yml`) cannot be exercised without live
provider credentials. These tests prove the two properties it depends on,
offline:

1. **Canonical-diff legibility** — single field change → one changed line.
2. **Redaction** — secrets replaced at all depths by `replay.Canonicalize`.

Both tests drive the real `replay.Canonicalize` production path. They are not
mocks and they are not re-implementations. If you break `Canonicalize` or
`WithRedactedKeys`, these tests fail.

## Allowed changes

- Add new test cases to `refreshworkflow_test.go` that cover additional
  canonical or redaction properties.
- Update `doc.go` and `README.md` if the R-6 workflow or its properties change.
- Do **not** add non-test Go files here. If you need a helper shared across
  tests in this package, keep it in a `_test.go` file in this package.

## Verification

```
cd go && go test ./internal/replay/refreshworkflow -count=1
```

All tests must pass without credentials, Docker, or network access.
