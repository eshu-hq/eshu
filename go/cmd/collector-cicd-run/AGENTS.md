# AGENTS.md - collector-cicd-run command guidance

## Scope

This command wires the hosted GitHub Actions CI/CD run collector binary. It
loads workflow collector instance JSON, resolves `token_env` credential
references, builds `ghactionsruntime.ClaimedSource`, and commits through
`collector.ClaimedService`.

## Rules

- Do not log, expose, or persist credential values.
- Keep repository names, run IDs, artifact names, and provider URLs out of
  metric labels.
- The command must use workflow claims and claim fencing; do not write directly
  to Postgres.
- Configuration must keep credential references (`token_env`) separate from
  resolved runtime tokens.
- Add focused config tests for new environment variables or target shapes.
