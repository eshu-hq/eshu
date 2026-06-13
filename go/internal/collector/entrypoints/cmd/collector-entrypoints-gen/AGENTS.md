# Collector Entrypoints Gen Agent Notes

Read these before changing this command:

- `AGENTS.md`
- `docs/internal/agent-guide.md`
- `go/internal/collector/entrypoints/AGENTS.md`

Keep the command a thin wrapper around `internal/collector/entrypoints`.
Generation logic belongs in the package, not in flag parsing code.

Do not make `-check` write files. Do not include secrets, private target IDs, or
customer-specific material in verifier output.
