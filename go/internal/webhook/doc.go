// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package webhook verifies GitHub, GitLab, Bitbucket, Azure DevOps, Jenkins,
// CircleCI, Buildkite, Drone, PagerDuty, and Jira webhook authentication
// material and normalizes provider payloads into repository or incident-source
// refresh decisions and bounded Actions wake-up identities.
//
// The package deliberately stops before persistence or queue handoff. Trigger
// describes a repository-refresh decision; IncidentFreshnessTrigger describes
// an incident-source refresh decision. ActionsTrigger carries bounded workflow
// identity after its caller verifies authentication, without a refresh decision.
// Stored variants add the durable status fields owned by storage implementations.
// A verified webhook is a wake-up signal for normal collection, not graph truth.
// Repository refreshes still require repository snapshotting; Actions and
// incident-source collectors fetch authoritative provider data. Repository
// normalizers ignore merge events without a merge commit and default-branch
// delete pushes rather than rewriting them into another commit target. Merged GitHub pull-request events also carry bounded
// pull-request number, URL, and title provenance so read models can connect a
// refreshed commit to provider-owned PR evidence without treating the webhook
// as graph truth. PagerDuty and Jira incident freshness triggers carry only
// bounded event, scope, and resource identifiers; their payloads wake the normal
// claim-driven collectors and do not emit facts directly. Jira freshness
// normalization accepts only issue created, updated, and deleted events, and
// fingerprints issue self URLs when Jira omits issue ID and key values.
package webhook
