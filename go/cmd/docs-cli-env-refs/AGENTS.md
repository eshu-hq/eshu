# docs-cli-env-refs agent guidance

- Preserve the precision-first exclusions documented in README.md and the
  wrapper header unless a failing regression proves a safe expansion.
- Tests must exercise `envregistry.Default()` and a real built Eshu CLI, not a
  copied flag allowlist.
- Keep baseline updates deterministic and burn-down-only in normal check mode.
