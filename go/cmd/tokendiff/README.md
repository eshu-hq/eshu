# tokendiff

A small CLI that decides whether two versions of a Go source file are
behavior-identical, for `scripts/verify-parser-relationship-kit.sh`'s
comment-only-edit exemption (issue #6647). See `doc.go` for the full godoc
contract; this file covers ownership and operational context.

## Why this exists

`parser-relationship-kit`'s language-query-source rule requires a
`docs/public/reference/language-query-dsl.md` update whenever a file matching
`go/internal/query/language*.go` (and its siblings) changes. That is correct
for a real behavior change, but a pure doc-comment edit cannot change what
the DSL does, and the gate has no way to tell the two apart from a line-based
diff alone -- see `doc.go`'s "Why not a line-based diff" section for the
specific unsafe patterns (`//go:build`, a raw-string line starting with
`//`, and others) that ruled out a shell-only classifier.

## Ownership

This tool has exactly one caller:
`scripts/lib/parser_relationship_comment_only_diff.sh`, sourced by
`scripts/verify-parser-relationship-kit.sh`. That shell layer owns
everything git-shaped (resolving the gate's `$base`, reading the base blob
with `git show`, and treating an added/deleted/renamed path as a real
change without ever invoking this tool). This package owns only the pure
comparison: given two files' bytes, are they token-identical under the
comment-only exemption rules.

## Operational notes

- Invoked with `env -u GOROOT go run ./cmd/tokendiff -base <path> -head
  <path>` (or a built binary) from `go/`, matching this repo's other
  script-invoked Go helpers (see `go/cmd/heredoc-budget`).
- No flags beyond `-base`/`-head`; no config file, no state, no network
  access. Pure function of two file paths' contents.
- Exit codes are the contract: 0 exempt, 1 real change, 2 error. Every
  caller must treat 1 and 2 identically (fail closed) -- see `doc.go`.

## Testing

`go test ./cmd/tokendiff -count=1` covers the cases enumerated in the
package doc comment, plus the fail-closed path on an unparseable file. The
shell-level acceptance cases (the full adversarial list: directive
comments, raw strings, added/deleted/renamed files, the dead-code rule
staying separate, cgo) live in
`scripts/lib/test-verify-parser-relationship-kit-dsl-comment-only-cases.sh`,
run through the real gate end to end rather than through this binary
directly.
