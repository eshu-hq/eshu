// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package webhook verifies GitHub, GitLab, Bitbucket, Azure DevOps, Jenkins,
// PagerDuty, and Jira webhook authentication material and normalizes provider
// payloads into repository or incident-source refresh trigger decisions.
//
// The package deliberately stops before persistence or queue handoff. Trigger
// describes the provider decision; StoredTrigger adds the durable status fields
// owned by storage implementations. A verified webhook is a wake-up signal for
// the normal Eshu collection flow, not graph truth and not a shortcut around
// repository snapshotting. Provider merge events without a merge commit and
// default-branch delete pushes are ignored rather than rewritten into another
// commit target. Merged GitHub pull-request events also carry bounded
// pull-request number, URL, and title provenance so read models can connect a
// refreshed commit to provider-owned PR evidence without treating the webhook
// as graph truth. PagerDuty and Jira incident freshness triggers carry only
// bounded event, scope, and resource identifiers; their payloads wake the normal
// claim-driven collectors and do not emit facts directly. Jira freshness
// normalization accepts only issue created, updated, and deleted events, and
// fingerprints issue self URLs when Jira omits issue ID and key values.
package webhook
