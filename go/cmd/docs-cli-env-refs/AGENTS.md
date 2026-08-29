# docs-cli-env-refs agent guidance

- Preserve the precision-first exclusions documented in README.md and the
  wrapper header unless a failing regression proves a safe expansion.
- `pinnedSkippedEshuLines` is pinned in BOTH directions on purpose. When the
  gate reports growth, the first move is to rewrite the documented example into
  the supported grammar; re-pinning is the fallback and needs the reason in the
  change. Never widen `minAttributedEshuSegments` downward to make a red run
  green — a falling attributed count is the scanner losing coverage.
- `stripCommandPrefixes` strips `NAME=value` assignments and a bare `sudo`
  before the `eshu` test, and `eshuCommandFields` and `mentionsEshuCommand`
  MUST keep sharing it. When they disagreed (#6230), a prefixed line was
  neither attributed nor skipped, so its flags went unchecked while both
  counters still read healthy. Widening the prefix set needs a fixture proving
  a non-Eshu command behind the same prefix stays out of scope.
- The simple-list grammar in `commandSegments` is a deliberate
  under-approximation. Widening it needs a hostile collision test first: a later
  segment carrying a flag that is invalid there but valid on an earlier command,
  proving the gate still fails. A conservative skip is correct; a splitter that
  attributes a flag to the wrong command is the #6108 defect.
- Tests must exercise `envregistry.Default()` and a real built Eshu CLI, not a
  copied flag allowlist.
- Keep baseline updates deterministic and burn-down-only. The `-update` path
  must reject new debt before rewriting the baseline.
- Treat `scripts/docs-cli-env-refs-ceiling.txt` as the frozen #6023 initial
  debt set. Never regenerate it from the current docs or mutable baseline.
