# AGENTS.md — go/internal/collector/repo/git/env

## Read first

1. `go/internal/collector/repo/git/env/README.md` — what lives here
2. `go/internal/collector/repo/git/AGENTS.md` — the `git -> leaf`
   import direction

## Rules

- This package is a leaf. `git` imports it; it must never import `git`
  or any other Eshu package. Standard library only.
- It holds the environment coercions the config loader shares across its
  sections (first-non-empty, string/bool parsing). Pure functions only: no
  process environment reads, no logging, no metrics.
- A new helper with one call site stays with that caller; only shared
  coercions move here.
