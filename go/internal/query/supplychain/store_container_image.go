// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package supplychain

import (
	"context"
)

// ContainerImageIdentityStore reads reducer-owned container image identity
// facts.
type ContainerImageIdentityStore interface {
	ListContainerImageIdentities(context.Context, ContainerImageIdentityFilter) ([]ContainerImageIdentityRow, error)
}

// ContainerImageIdentityFilter bounds identity reads to an image digest, image
// reference, source repository, OCI repository, or reducer outcome.
type ContainerImageIdentityFilter struct {
	Digest             string
	ImageRef           string
	SourceRepositoryID string
	RepositoryID       string
	Outcome            string
	AfterIdentityID    string
	Limit              int
	// AllowedSourceRepositoryIDs carries the scoped-token grant set (the union
	// of granted repository and ingestion-scope ids). When empty the read is
	// unrestricted (shared token, all-scope admin, or local dev mode). When
	// populated the query keeps only identities whose source_repository_ids
	// overlap the granted set, so a scoped caller never sees image identities
	// it cannot attribute to a granted git repository. Identity facts key on
	// the OCI repository_id and an OCI registry ingestion scope, neither of
	// which is a durable join to a git-repo grant, so source_repository_ids
	// overlap is the only correct attribution and uncorrelated images stay
	// invisible to scoped tokens.
	AllowedSourceRepositoryIDs []string
}

// ContainerImageIdentityRow is one durable image identity fact decoded from
// the reducer-owned read model.
type ContainerImageIdentityRow struct {
	IdentityID          string
	Digest              string
	ImageRef            string
	RepositoryID        string
	SourceRepositoryIDs []string
	SourceRevision      string
	// SourceRevisionProvenance names where SourceRevision came from
	// ("oci_config_source_label" or "ci_run_commit"), letting a consumer keep
	// the in-image-label tier distinct from the weaker CI-run-commit fallback
	// (#5423). Empty when no revision was resolved.
	SourceRevisionProvenance string
	WorkloadIDs              []string
	ServiceIDs               []string
	Outcome                  string
	Reason                   string
	IdentityStrength         string
	CanonicalID              string
	CanonicalWrites          int
	SourceLayers             []string
	EvidenceFactIDs          []string
	MissingEvidence          []string
	SourceFreshness          string
	SourceConfidence         string
}

func (f ContainerImageIdentityFilter) HasScope() bool {
	return f.Digest != "" || f.ImageRef != "" || f.SourceRepositoryID != "" ||
		f.RepositoryID != "" || f.Outcome != ""
}

// ContainerImageIdentityAggregateStore reads cheap-summary aggregates over
// reducer-owned container image identities. It replaces the page-and-iterate
// caller workflow for ecosystem-level questions like "how many images
// resolved by exact digest vs tag?" or "which repositories have the most
// container images?".
type ContainerImageIdentityAggregateStore interface {
	CountContainerImageIdentities(context.Context, ContainerImageIdentityAggregateFilter) (ContainerImageIdentityAggregateCount, error)
	ContainerImageIdentityInventory(
		context.Context,
		ContainerImageIdentityAggregateFilter,
		ContainerImageIdentityInventoryDimension,
		int,
		int,
	) ([]ContainerImageIdentityInventoryRow, error)
}

// ContainerImageIdentityInventoryDimension names the grouping dimension for
// the inventory aggregate.
type ContainerImageIdentityInventoryDimension string

const (
	// ContainerImageIdentityInventoryByOutcome groups by reducer outcome
	// (exact_digest / tag_resolved).
	ContainerImageIdentityInventoryByOutcome ContainerImageIdentityInventoryDimension = "outcome"
	// ContainerImageIdentityInventoryByIdentityStrength groups by reducer
	// identity_strength.
	ContainerImageIdentityInventoryByIdentityStrength ContainerImageIdentityInventoryDimension = "identity_strength"
	// ContainerImageIdentityInventoryByRepository groups by repository_id.
	ContainerImageIdentityInventoryByRepository ContainerImageIdentityInventoryDimension = "repository_id"
)

// ContainerImageIdentityAggregateMaxLimit caps inventory result pages.
const ContainerImageIdentityAggregateMaxLimit = 500

// ContainerImageIdentityAggregateFilter narrows aggregate reads. An aggregate
// without a scope is allowed because the totals question itself is the call
// shape we want to support — the dataset is already bounded by `fact_kind`
// and the active-generation predicate at index lookup time.
type ContainerImageIdentityAggregateFilter struct {
	Digest             string
	ImageRef           string
	SourceRepositoryID string
	RepositoryID       string
	Outcome            string
	// AllowedSourceRepositoryIDs carries the scoped-token grant set (the union
	// of granted repository and ingestion-scope ids). Empty means unrestricted;
	// when populated the aggregate counts and inventory buckets cover only
	// identities whose source_repository_ids overlap the granted set, so
	// uncorrelated images never inflate a scoped caller's totals.
	AllowedSourceRepositoryIDs []string
}

// ContainerImageIdentityAggregateCount is the cheap-summary totals envelope
// used by the count handler. ByOutcome and ByIdentityStrength are
// pre-aggregated rollups so callers can answer "images per outcome" and
// "images per identity strength" without a second round trip.
type ContainerImageIdentityAggregateCount struct {
	TotalIdentities    int
	ByOutcome          map[string]int
	ByIdentityStrength map[string]int
}

// ContainerImageIdentityInventoryRow is one grouped bucket returned by the
// inventory aggregate.
type ContainerImageIdentityInventoryRow struct {
	Dimension ContainerImageIdentityInventoryDimension `json:"dimension"`
	Value     string                                   `json:"value"`
	Count     int                                      `json:"count"`
}
