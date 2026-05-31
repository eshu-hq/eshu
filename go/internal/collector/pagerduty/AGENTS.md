# AGENTS.md - pagerduty collector guidance

## Scope

This package owns PagerDuty incident-context source facts for the `pagerduty`
collector family. It is a source-evidence boundary only.

## Rules

- Do not log, persist, or emit PagerDuty token values.
- Do not put incident IDs, incident titles, service names, escalation-policy
  names, URLs, or token environment names in metric labels.
- Keep collection bounded by configured time windows, limits, and service
  allowlists.
- Emit only source facts. Do not create Jira, GitHub, runtime, image, commit,
  graph, or query truth from this package.
- Preserve provider-native IDs in payload and stable identity so retries and
  duplicate delivery remain idempotent.
- Strip token-like query parameters before placing URLs in facts or source
  refs.
- Add focused tests for new provider payload shapes, redaction paths, failure
  classes, and request pagination behavior before implementation changes.
