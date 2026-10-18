// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package selection

// UpstreamClassification is the probe verdict for a scope whose live row is
// Confirmed not_listed (#8454): what the upstream provider says about the
// repository the listing no longer names. The values match the
// repository_selection_observations upstream_classification CHECK constraint.
// The zero value ("") means unclassified: no probe has reported yet, which
// the readers render as absent, never as evidence.
type UpstreamClassification string

const (
	// UpstreamTransferred means the repository still exists upstream under a
	// different owner: the probe answered 200 with a full_name whose owner
	// differs from the selector's.
	UpstreamTransferred UpstreamClassification = "transferred"
	// UpstreamRenamed means the repository still exists upstream under the
	// same owner with a different name: the probe answered 200 with a
	// full_name whose owner matches the selector's but whose name differs.
	// SuccessorSlug carries the new full_name; whether it resolves to a
	// selected scope is decided at read time only.
	UpstreamRenamed UpstreamClassification = "renamed"
	// UpstreamStillPresent means the repository still exists upstream under
	// the same full_name the scope stores: the probe answered 200 with no
	// name change, so the listing missed a present repository.
	UpstreamStillPresent UpstreamClassification = "still_present"
	// UpstreamNotFoundOrInaccessible means the probe answered 404. It never
	// reads "deleted": a 404 also covers a private repository the listing
	// credential cannot see.
	UpstreamNotFoundOrInaccessible UpstreamClassification = "not_found_or_inaccessible"
)
