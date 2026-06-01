# AGENTS.md - collector-pagerduty command guidance

## Scope

This command wires the hosted PagerDuty incident-context and optional
live-config-validation collector binary. It loads workflow collector instance
JSON, resolves credential environment references, builds
`pagerduty.ClaimedSource`, and commits through `collector.ClaimedService`.

## Rules

- Do not log, expose, or persist credential values.
- Keep incident IDs, service names, integration names, escalation-policy names,
  routing keys, token environment names, and provider URLs out of metric labels.
- The command must use workflow claims and claim fencing; do not write directly
  to Postgres outside `collector.ClaimedService`.
- Configuration must keep credential references (`token_env`) separate from
  resolved runtime tokens.
- Add focused config tests for new environment variables, credential shapes, or
  target limits.
