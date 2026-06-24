// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package terraformstate

// DiscoveryConfig controls Terraform state candidate discovery.
type DiscoveryConfig struct {
	Graph                bool
	Seeds                []DiscoverySeed
	LocalRepos           []string
	BackendFilters       []DiscoveryBackendFilter
	LocalStateCandidates LocalStateCandidatePolicy
}

// DiscoveryBackendFilter scopes graph-backed backend discovery without naming
// individual repositories. Empty fields are wildcards; target_scope_id is
// routing metadata applied to matching candidates.
type DiscoveryBackendFilter struct {
	TargetScopeID string
	BackendKind   BackendKind
	Bucket        string
	Key           string
	Region        string
}

// DiscoverySeed is one exact operator-approved state locator.
type DiscoverySeed struct {
	Kind          BackendKind
	TargetScopeID string
	Path          string
	RepoID        string
	Bucket        string
	Key           string
	Region        string
	VersionID     string
	DynamoDBTable string
	// PreviousETag is durable freshness metadata from a previous S3 read. It is
	// intentionally not populated from collector configuration JSON.
	PreviousETag string
}

// DiscoveryCandidate is one exact Terraform state object to inspect later.
type DiscoveryCandidate struct {
	State             StateKey
	Source            DiscoveryCandidateSource
	TargetScopeID     string
	RepoID            string
	RelativePath      string
	Region            string
	DynamoDBTable     string
	PreviousETag      string
	PriorGenerationID string
	StateInVCS        bool
}

// DiscoveryQuery scopes graph-backed Terraform backend fact reads.
type DiscoveryQuery struct {
	RepoIDs                     []string
	BackendFilters              []DiscoveryBackendFilter
	IncludeLocalStateCandidates bool
	ApprovedLocalCandidates     []LocalStateCandidateRef
}
