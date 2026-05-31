# Jira Collector Agent Notes

This package owns Jira source collection only. Keep it independent from
PagerDuty, GitHub PR verification, deploy systems, graph writes, and reducer
truth.

- Preserve Jira provider-native keys in stable fact identities.
- Keep credentials and raw tokens out of facts, logs, errors, and metric labels.
- Add tests before changing REST paths, pagination, redaction, failure classes,
  or envelope identity.
- Metrics must stay low-cardinality: provider, status class, and fact kind are
  acceptable; site IDs, issue keys, summaries, URLs, and user identities are not.
