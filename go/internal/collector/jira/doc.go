// Package jira contains the claim-driven Jira work-item evidence collector.
//
// The collector emits source facts only: work item records, Jira changelog
// transitions, and remote links attached to issues. Reducers own all incident,
// deployment, code, and pull-request correlation truth downstream.
package jira
