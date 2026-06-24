// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

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
//
// For a confidently typed GitHub pull-request or GitLab merge-request remote
// link, the collector resolves the URL to a canonical repository id before the
// raw URL is redacted and persists only that id as linked_repository_id on the
// work_item.external_link fact. The raw URL stays redacted; the id is the same
// generation-independent identifier Eshu stores for every repository and
// carries no raw URL, query parameter, credential, or user identity.
// Un-canonicalizable or ambiguous links omit the field rather than guess.
package jira
