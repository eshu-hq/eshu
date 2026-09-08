// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

import "context"

// CICDRunCorrelationStore reads reducer-owned CI/CD run correlations.
//
// It lives here (promoted from root package query for #6060 lane B B3)
// because two query families need it without importing root: the staying
// CI/CD handler and the moved repository handler's story CI/CD evidence
// read. Root keeps type aliases so every staying caller compiles unchanged;
// the Postgres implementation stays in root.
type CICDRunCorrelationStore interface {
	ListCICDRunCorrelations(context.Context, CICDRunCorrelationFilter) ([]CICDRunCorrelationRow, error)
}

// CICDRunCorrelationFilter bounds run-correlation reads to a concrete repo,
// commit, run, artifact digest, environment, or scope.
type CICDRunCorrelationFilter struct {
	ScopeID              string
	RepositoryID         string
	CommitSHA            string
	Provider             string
	ProviderRunID        string
	ArtifactDigest       string
	ImageRef             string
	Environment          string
	Outcome              string
	AfterCorrelationID   string
	AllowedRepositoryIDs []string
	AllowedScopeIDs      []string
	Limit                int
}

// HasScope reports whether the filter carries any read anchor: a concrete
// repository, commit, run, artifact digest, image, or environment, or an
// ingestion scope.
//
// It is exported because the staying Postgres implementation and the
// staying CI/CD handler call it across the package boundary.
func (f CICDRunCorrelationFilter) HasScope() bool {
	return f.ScopeID != "" ||
		f.RepositoryID != "" ||
		f.CommitSHA != "" ||
		f.ProviderRunID != "" ||
		f.ArtifactDigest != "" ||
		f.ImageRef != "" ||
		f.Environment != ""
}

// HasProviderRunDisambiguator reports whether the filter carries enough of
// an anchor to disambiguate one provider run. It is exported for the same
// reason as HasScope.
func (f CICDRunCorrelationFilter) HasProviderRunDisambiguator() bool {
	return f.ScopeID != "" ||
		f.RepositoryID != "" ||
		f.CommitSHA != "" ||
		f.ArtifactDigest != "" ||
		f.ImageRef != "" ||
		f.Environment != ""
}

// CICDRunCorrelationRow is one durable CI/CD correlation fact decoded from
// the reducer-owned read model.
type CICDRunCorrelationRow struct {
	CorrelationID       string
	Provider            string
	RunID               string
	RunAttempt          string
	RepositoryID        string
	CommitSHA           string
	Environment         string
	EnvironmentEvidence string
	ArtifactDigest      string
	ImageRef            string
	Outcome             string
	Reason              string
	ProvenanceOnly      bool
	CanonicalWrites     int
	CanonicalTarget     string
	CorrelationKind     string
	EvidenceFactIDs     []string
}

// CICDRunCorrelationResult is one reducer-owned CI/CD run correlation row.
// It lives here (promoted from root package query for #6060 lane B B3)
// because the repository artifact evidence assembly in repositoryartifacts
// decodes run rows without importing root; root keeps a type alias so every
// staying caller compiles unchanged.
type CICDRunCorrelationResult struct {
	CorrelationID string `json:"correlation_id"`
	Provider      string `json:"provider,omitempty"`
	RunID         string `json:"run_id,omitempty"`
	RunAttempt    string `json:"run_attempt,omitempty"`
	RepositoryID  string `json:"repository_id,omitempty"`
	CommitSHA     string `json:"commit_sha,omitempty"`
	Environment   string `json:"environment,omitempty"`
	// EnvironmentEvidence records how the environment was established:
	// "deploy_event" when a ci.deployment_event observed at the run's commit
	// supplied it, "declared" when it came from the CI-declared workflow job
	// gate alone. Empty when the correlation has no environment. Consumers
	// that treat an environment as deployment truth should require
	// "deploy_event" rather than accepting a declared value (#5426).
	EnvironmentEvidence string   `json:"environment_evidence,omitempty"`
	ArtifactDigest      string   `json:"artifact_digest,omitempty"`
	ImageRef            string   `json:"image_ref,omitempty"`
	Outcome             string   `json:"outcome"`
	Reason              string   `json:"reason,omitempty"`
	ProvenanceOnly      bool     `json:"provenance_only"`
	CanonicalWrites     int      `json:"canonical_writes"`
	CanonicalTarget     string   `json:"canonical_target,omitempty"`
	CorrelationKind     string   `json:"correlation_kind,omitempty"`
	EvidenceFactIDs     []string `json:"evidence_fact_ids,omitempty"`
}
