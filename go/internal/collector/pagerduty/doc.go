// Package pagerduty normalizes PagerDuty incident context and optional live
// PagerDuty configuration validation into durable source facts.
//
// The package owns PagerDuty incident, lifecycle log-entry, and related
// change-event evidence collection. When enabled per target, it also emits
// observed service and service-integration incident-routing facts for no-IaC
// fallback and freshness validation. Emitted facts preserve provider-native
// identifiers, bounded status fields, timestamps, service references, and
// sanitized source URLs with reported confidence while redacting or
// fingerprinting names, routing keys, token-like query parameters, and private
// values. They are source evidence, not canonical incident, deployment,
// work-item, or code truth; reducers and query read models own later
// correlation with runtime artifacts, commits, pull requests, Terraform
// declared/applied routing evidence, and Jira work items.
package pagerduty
