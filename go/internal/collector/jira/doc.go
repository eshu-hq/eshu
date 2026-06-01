// Package jira contains the claim-driven Jira work-item evidence collector.
//
// The collector emits source facts only: work item records, Jira changelog
// transitions, remote links attached to issues, and bounded Jira metadata
// definitions for projects, issue types, statuses, workflows, fields, and
// metadata warnings. It pages bounded search and changelog reads, redacts
// private issue text, user identifiers, custom-field identifiers, metadata
// names, and raw URLs, and reports page, redaction, permission, and rejection
// counters on the Jira fetch span. Reducers own all incident, deployment, code,
// and pull-request correlation truth downstream.
package jira
