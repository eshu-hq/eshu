# AGENTS.md - collector-security-alerts command guidance

## Scope

This command wires the hosted provider security-alert collector binary. It
loads workflow collector instance JSON, resolves credential environment
references, builds `alertruntime.ClaimedSource`, and commits through
`collector.ClaimedService`.

## Rules

- Do not log, expose, or persist credential values.
- Keep repository names and provider URLs out of metric labels.
- The command must use workflow claims and claim fencing; do not write directly
  to Postgres.
- Configuration must keep credential references (`token_env`) separate from
  resolved runtime tokens.
- Add focused config tests for new environment variables or credential shapes.
