// Package jira contains the claim-driven Jira work-item evidence collector.
//
// The collector emits source facts only: work item records, Jira changelog
// transitions, and remote links attached to issues. Reducers own all incident,
// deployment, code, and pull-request correlation truth downstream. Project,
// status, workflow, and field metadata are follow-up Jira work-item evidence
// contracts; they must add fact names, schema helpers, and fixtures before live
// provider collection expands.
package jira
