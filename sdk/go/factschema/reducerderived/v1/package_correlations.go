// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

// PackageOwnershipCorrelation is the schema-version-1 payload for
// "reducer_package_ownership_correlation". It records a reducer-owned package
// source-hint ownership decision without promoting it to canonical ownership
// truth unless later evidence admits that write.
type PackageOwnershipCorrelation struct {
	PackageID              string   `json:"package_id"`
	ReducerDomain          *string  `json:"reducer_domain,omitempty"`
	IntentID               *string  `json:"intent_id,omitempty"`
	ScopeID                *string  `json:"scope_id,omitempty"`
	GenerationID           *string  `json:"generation_id,omitempty"`
	SourceSystem           *string  `json:"source_system,omitempty"`
	Cause                  *string  `json:"cause,omitempty"`
	RelationshipKind       *string  `json:"relationship_kind,omitempty"`
	VersionID              *string  `json:"version_id,omitempty"`
	HintKind               *string  `json:"hint_kind,omitempty"`
	SourceURL              *string  `json:"source_url,omitempty"`
	RepositoryID           *string  `json:"repository_id,omitempty"`
	RepositoryName         *string  `json:"repository_name,omitempty"`
	CandidateRepositoryIDs []string `json:"candidate_repository_ids,omitempty"`
	Outcome                *string  `json:"outcome,omitempty"`
	Reason                 *string  `json:"reason,omitempty"`
	ProvenanceOnly         *bool    `json:"provenance_only,omitempty"`
	CanonicalWrites        *int     `json:"canonical_writes,omitempty"`
	EvidenceFactIDs        []string `json:"evidence_fact_ids,omitempty"`
	CorrelationKind        *string  `json:"correlation_kind,omitempty"`
	SourceLayers           []string `json:"source_layers,omitempty"`
}

// PackageConsumptionCorrelation is the schema-version-1 payload for
// "reducer_package_consumption_correlation". It records a repository manifest
// dependency admitted against a package-registry identity.
type PackageConsumptionCorrelation struct {
	PackageID                 string   `json:"package_id"`
	ReducerDomain             *string  `json:"reducer_domain,omitempty"`
	IntentID                  *string  `json:"intent_id,omitempty"`
	ScopeID                   *string  `json:"scope_id,omitempty"`
	GenerationID              *string  `json:"generation_id,omitempty"`
	SourceSystem              *string  `json:"source_system,omitempty"`
	Cause                     *string  `json:"cause,omitempty"`
	RelationshipKind          *string  `json:"relationship_kind,omitempty"`
	Ecosystem                 *string  `json:"ecosystem,omitempty"`
	PackageName               *string  `json:"package_name,omitempty"`
	RepositoryID              *string  `json:"repository_id,omitempty"`
	RepositoryName            *string  `json:"repository_name,omitempty"`
	RelativePath              *string  `json:"relative_path,omitempty"`
	ManifestSection           *string  `json:"manifest_section,omitempty"`
	DependencyRange           *string  `json:"dependency_range,omitempty"`
	ObservedVersion           *string  `json:"observed_version,omitempty"`
	ResolvedVersion           *string  `json:"resolved_version,omitempty"`
	RequestedRange            *string  `json:"requested_range,omitempty"`
	InstalledVersion          *string  `json:"installed_version,omitempty"`
	DependencyPath            []string `json:"dependency_path,omitempty"`
	DependencyDepth           *int     `json:"dependency_depth,omitempty"`
	DirectDependency          *bool    `json:"direct_dependency,omitempty"`
	Lockfile                  *bool    `json:"lockfile,omitempty"`
	DependencyScope           *string  `json:"dependency_scope,omitempty"`
	PrivateAssets             *string  `json:"private_assets,omitempty"`
	IncludeAssets             *string  `json:"include_assets,omitempty"`
	ExcludeAssets             *string  `json:"exclude_assets,omitempty"`
	DevelopmentDependency     *bool    `json:"development_dependency,omitempty"`
	TestDependency            *bool    `json:"test_dependency,omitempty"`
	VersionEvidence           *string  `json:"version_evidence,omitempty"`
	UnresolvedMSBuildProperty *string  `json:"unresolved_msbuild_property,omitempty"`
	AmbiguousMSBuildProperty  *string  `json:"ambiguous_msbuild_property,omitempty"`
	PartialEvidence           *bool    `json:"partial_evidence,omitempty"`
	Outcome                   *string  `json:"outcome,omitempty"`
	Reason                    *string  `json:"reason,omitempty"`
	ProvenanceOnly            *bool    `json:"provenance_only,omitempty"`
	CanonicalWrites           *int     `json:"canonical_writes,omitempty"`
	EvidenceFactIDs           []string `json:"evidence_fact_ids,omitempty"`
	CorrelationKind           *string  `json:"correlation_kind,omitempty"`
	SourceLayers              []string `json:"source_layers,omitempty"`
}

// PackagePublicationCorrelation is the schema-version-1 payload for
// "reducer_package_publication_correlation". It records package-version source
// hint evidence joined to repository identity candidates.
type PackagePublicationCorrelation struct {
	PackageID              string   `json:"package_id"`
	ReducerDomain          *string  `json:"reducer_domain,omitempty"`
	IntentID               *string  `json:"intent_id,omitempty"`
	ScopeID                *string  `json:"scope_id,omitempty"`
	GenerationID           *string  `json:"generation_id,omitempty"`
	SourceSystem           *string  `json:"source_system,omitempty"`
	Cause                  *string  `json:"cause,omitempty"`
	RelationshipKind       *string  `json:"relationship_kind,omitempty"`
	VersionID              *string  `json:"version_id,omitempty"`
	Version                *string  `json:"version,omitempty"`
	PublishedAt            *string  `json:"published_at,omitempty"`
	SourceURL              *string  `json:"source_url,omitempty"`
	SourceHintFactID       *string  `json:"source_hint_fact_id,omitempty"`
	SourceHintKind         *string  `json:"source_hint_kind,omitempty"`
	SourceHintVersionID    *string  `json:"source_hint_version_id,omitempty"`
	RepositoryID           *string  `json:"repository_id,omitempty"`
	RepositoryName         *string  `json:"repository_name,omitempty"`
	CandidateRepositoryIDs []string `json:"candidate_repository_ids,omitempty"`
	Outcome                *string  `json:"outcome,omitempty"`
	Reason                 *string  `json:"reason,omitempty"`
	ProvenanceOnly         *bool    `json:"provenance_only,omitempty"`
	CanonicalWrites        *int     `json:"canonical_writes,omitempty"`
	EvidenceFactIDs        []string `json:"evidence_fact_ids,omitempty"`
	CorrelationKind        *string  `json:"correlation_kind,omitempty"`
	SourceLayers           []string `json:"source_layers,omitempty"`
}
