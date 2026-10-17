# Commit trailers

## Dirgate-Growth

The dirgate gate (`go/cmd/dirgate`) lets a Go package directory grow past
`max(40, its non-test .go count at the base)` only when the commit that adds the
files carries this trailer, one line for each directory. It also names growth
that landed concurrently and was never waived; a one-commit repair PR carries the
trailer for that.

```text
Dirgate-Growth: <dir> +<N> #<issue> <reason>
```

- `<dir>` is relative to `go/`, for example `internal/storage/postgres`.
- `<N>` is the growth the gate says the directory needs, at least 1. The sum for
  one directory must match exactly. Too little, too much, and an
  unused trailer are all RED.
- `#<issue>` names the issue that owns the split. Use `owner/repo#N` for another
  repository.
- `<reason>` says why the files cannot go in a subpackage now.

Rules:

- A trailer cannot authorize later growth. Add it in the commit that adds the files.
- Moving files out, swapping files, and renaming inside a directory need no trailer.
- A move that creates a new directory over 40 files needs one.
- Two PRs that each add a file to the same directory each carry their own `+1`.
- Do not start any other commit-message line with `Dirgate-Growth:`. The gate
  reads every message in the range and rejects a malformed line.
- Write the trailer in the commit message, not the PR body. Local gates cannot read the body.
- Never edit `go/cmd/dirgate/baseline-cap.tsv` or `baseline-naming.tsv`. They are write-once.
- Full rules, events, and repair steps: `docs/public/reference/local-testing/dirgate-growth-trailer.md`.
