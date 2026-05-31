// Package pagerduty normalizes PagerDuty incident context into durable source
// facts.
//
// The package owns PagerDuty incident, lifecycle log-entry, and related
// change-event evidence collection. Emitted facts preserve provider-native
// identifiers, bounded status fields, timestamps, service references, and
// sanitized source URLs with reported confidence. They are source evidence,
// not canonical incident, deployment, work-item, or code truth; reducers and
// query read models own later correlation with runtime artifacts, commits,
// pull requests, and Jira work items.
package pagerduty
