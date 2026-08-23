# docs-cli-env-refs agent guidance

- Preserve the precision-first exclusions documented in README.md and the
  wrapper header unless a failing regression proves a safe expansion.
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
