# AGENTS.md — go/cmd/token-diff

Scoped agent instructions for this directory. See `README.md` for ownership
and operational context, `doc.go` for the full contract.

## Do not reintroduce a line-based classifier

The whole reason this package exists is that a line-based "starts with `//`"
comment classifier is unsafe (owner-confirmed, issue #6647): it cannot tell
a `//go:build`/`//go:generate`/`//go:embed`/`//line`/`// +build` directive
from a plain comment, and it cannot tell a raw-string line that happens to
start with `//` (embedded Cypher, SQL, or any other query text) from a real
Go comment. If you are tempted to "simplify" this back to a shell diff over
`+`/`-` prefixed lines, don't -- read `doc.go`'s "Why not a line-based diff"
section first, and re-run `go test ./cmd/token-diff` plus the shell-level
acceptance cases in
`scripts/lib/test-verify-parser-relationship-kit-dsl-comment-only-cases.sh`
before shipping any change here.

## Keep the exemption rules in lockstep

If you change what counts as "droppable" here (`isDroppableComment`,
`hasCgoImport`, or the SEMICOLON-by-kind comparison in `tokensEqual`), update
both `doc.go`'s "Rule" list and the parallel shell-level self-test cases so
the two stay in sync. The acceptance list this package is graded against
(PASS: comment-only, whitespace-only, blank-line-only, trailing-comment,
tab-indented comment; FAIL: any directive comment, block comment, raw-string
content, cgo preamble, added/deleted/renamed files, a code change under a
comment change) is not exhaustive by construction -- treat a new adversarial
case the same way the existing ones were found: think about what a real Go
program could mean by a `//`-prefixed line, not just what today's fixtures
cover.

## Keep the import rename exemption narrow

`rename.go` (`-allow-internal-import-rename`) exempts one internal import
substitution per file and nothing more. It cannot tell a move from a swap;
the shell caller's `is_internal_package_move` proves the move from git, and
both halves are required. Keep the stdout verdict line stable, since the
caller parses it. Do not widen it to aliased imports, several
renames, non-internal paths, or text rewriting without an owner decision. Any
change to its rules updates `doc.go`'s "Internal import rename" list,
`rename_test.go`, and
`scripts/lib/test-verify-parser-relationship-kit-import-rename-cases.sh`
together. Each refused shape must stay a test that expects exit 1.

## Fail closed, always

Any new error path (a new os.ReadFile call, a new parser/scanner entry
point) MUST return exit 2, never exit 0, on failure. This tool feeds a
blocking pre-PR gate; a silent exit-0 on an internal error would exempt a
real behavior change.
