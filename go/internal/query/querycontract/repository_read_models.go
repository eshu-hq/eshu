// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

// The repository-story and evidence read models live here for the same reason
// as the documentation ones: the shared ContentStore double that answers them
// has to be constructible from outside package query (#6060, epic #6053).
// Package query keeps an unexported alias for each, so its call sites are
// unchanged.
//
// Each of these is answered through a narrow optional port that package query
// type-asserts the ContentStore against. Available is what carries "this store
// could not answer" through a signature that has no room to say so, and a
// handler must check it before reading Rows -- an unavailable read model is
// not the same as one that legitimately found nothing.

// RepositoryEntryPointReadModel carries content-derived repository entry
// points.
type RepositoryEntryPointReadModel struct {
	Available bool
	Rows      []map[string]any
}

// RepositoryDeploymentEvidenceReadModel carries content-derived deployment
// evidence rows for one repository.
//
// Truncated reports that Limit cut the row set, so the response says the
// evidence is partial rather than presenting a short list as complete.
type RepositoryDeploymentEvidenceReadModel struct {
	Available bool
	Rows      []map[string]any
	Limit     int
	Truncated bool
}

// RelationshipEvidenceReadModel carries the evidence row for one resolved
// relationship.
type RelationshipEvidenceReadModel struct {
	Available bool
	Row       map[string]any
}

// ServiceStoryTargetSupportFilter selects the documentation support evidence
// attached to one service story.
type ServiceStoryTargetSupportFilter struct {
	Repository string
	TargetKind string
	TargetID   string
	ServiceID  string
	Limit      int
}

// ServiceStoryTargetSupportReadModel carries the support block a service story
// embeds. A nil Support means the store had nothing to attach.
type ServiceStoryTargetSupportReadModel struct {
	Support map[string]any
}
