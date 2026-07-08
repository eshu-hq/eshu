// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

// Run is the schema-version-1 typed payload for the "ci.run" fact kind: one
// provider CI/CD run execution (go/internal/collector/cicdrun.runEnvelope).
//
// Provider and RunID are the required identity fields: the collector's
// sharedPayload always sets Provider, and runEnvelope always sets RunID from
// the provider's own run identifier
// (go/internal/collector/cicdrun/github_actions_fixture.go:190-223). Together
// with RunAttempt they form the reducer's own join key
// (cicdRunKey/go/internal/reducer/ci_cd_run_correlation.go:428-435) that keys
// every other ci_cd_run kind's evidence back to its owning run; a fact
// missing either segment could never join to its run, so a decode-time
// guarantee here replaces the pre-typing empty-string join-key collapse.
// RunAttempt stays OPTIONAL because the collector already defaults an absent
// attempt to "1" (go/internal/collector/cicdrun/envelope.go:110-115,
// defaultCICDRunAttempt mirrors it on the reducer's read side) — the decode
// seam must not require a key the collector's own contract treats as
// defaultable.
type Run struct {
	// Provider identifies the CI/CD provider that reported this run (for
	// example "github_actions"). Required — half of the reducer's run join
	// key.
	Provider string `json:"provider"`

	// RunID is the provider's own run identifier. Required — half of the
	// reducer's run join key.
	RunID string `json:"run_id"`

	// RunAttempt is the provider's run-attempt number as a string. Optional:
	// the collector always writes it, but an absent value defaults to "1" on
	// both the collector's emit side and the reducer's read side
	// (defaultCICDRunAttempt), so it is not a decode-time requirement.
	RunAttempt *string `json:"run_attempt,omitempty"`

	// RunNumber is the provider's run-number as a string. Optional.
	RunNumber *string `json:"run_number,omitempty"`

	// WorkflowName is the run's workflow display name. Optional.
	WorkflowName *string `json:"workflow_name,omitempty"`

	// Event is the triggering event (for example "push", "pull_request").
	// Optional.
	Event *string `json:"event,omitempty"`

	// Status is the provider's run status (for example "completed").
	// Optional.
	Status *string `json:"status,omitempty"`

	// Result is the provider's run conclusion (for example "success").
	// Optional: the reducer never treats CI success alone as deployment
	// truth (Golden Rules), so this field is read only for evidence
	// provenance, never a correlation decision input.
	Result *string `json:"result,omitempty"`

	// Branch is the run's head branch. Optional.
	Branch *string `json:"branch,omitempty"`

	// CommitSHA is the run's head commit SHA. Optional: the reducer's
	// classifyCICDRunEvidence marks a run UNRESOLVED when this is absent
	// alongside RepositoryID (go/internal/reducer/ci_cd_run_correlation.go:363-367),
	// a matcher-level completeness gate, not a decode-time requirement — a
	// run fact with no commit anchor is still a valid, if unresolved,
	// observation.
	CommitSHA *string `json:"commit_sha,omitempty"`

	// RepositoryID is the reducer-facing repository locator the collector
	// derives from the provider's repository reference
	// (go/internal/collector/cicdrun/github_actions_fixture.go:367-373).
	// Optional: see CommitSHA for the unresolved-outcome matcher gate this
	// field also participates in.
	RepositoryID *string `json:"repository_id,omitempty"`

	// RepositoryURL is the provider's repository URL. Optional.
	RepositoryURL *string `json:"repository_url,omitempty"`

	// Actor is the run's triggering actor login. Optional.
	Actor *string `json:"actor,omitempty"`

	// StartedAt is the run's start timestamp as an RFC3339 string. Optional.
	StartedAt *string `json:"started_at,omitempty"`

	// UpdatedAt is the run's last-updated timestamp as an RFC3339 string.
	// Optional.
	UpdatedAt *string `json:"updated_at,omitempty"`

	// URL is the run's provider-hosted URL, with credential/query-string
	// evidence stripped by the collector. Optional.
	URL *string `json:"url,omitempty"`

	// CorrelationAnchors lists the non-empty repository/commit/run identity
	// segments the collector precomputed for correlation. Optional: not read
	// by the reducer's typed decode path today (the reducer recomputes its
	// own join key from Provider/RunID/RunAttempt), but modeled because the
	// collector always emits it.
	CorrelationAnchors []string `json:"correlation_anchors,omitempty"`

	// CollectorInstanceID is the collector boundary token. Optional.
	CollectorInstanceID *string `json:"collector_instance_id,omitempty"`
}

// Artifact is the schema-version-1 typed payload for the "ci.artifact" fact
// kind: one artifact emitted by a provider run
// (go/internal/collector/cicdrun.artifactEnvelope).
//
// Provider and RunID are required for the same run-join-key reason as Run
// above: artifactEnvelope always sets both via sharedPayload, and the
// reducer's classifyCICDRunEvidence/ciArtifactDigests join every artifact
// fact back to its run through cicdRunKey. A fact missing either segment
// could never join to its owning run.
type Artifact struct {
	// Provider identifies the CI/CD provider that reported this artifact.
	// Required — half of the reducer's run join key.
	Provider string `json:"provider"`

	// RunID is the owning run's provider identifier. Required — half of the
	// reducer's run join key.
	RunID string `json:"run_id"`

	// RunAttempt is the owning run's attempt number as a string. Optional:
	// see Run.RunAttempt for the same default-to-"1" contract.
	RunAttempt *string `json:"run_attempt,omitempty"`

	// ArtifactID is the provider's artifact identifier. Optional.
	ArtifactID *string `json:"artifact_id,omitempty"`

	// ArtifactName is the artifact's declared name. Optional.
	ArtifactName *string `json:"artifact_name,omitempty"`

	// ArtifactType classifies the artifact (for example "container_image").
	// Optional: the reducer's addCICDArtifactImageReference
	// (container_image_identity_evidence.go) and the artifact-digest join
	// path both read this defensively; an absent value simply does not match
	// "container_image" rather than dead-lettering, matching pre-typing
	// behavior where an absent key read as "".
	ArtifactType *string `json:"artifact_type,omitempty"`

	// ArtifactDigest is the artifact's content digest (for example
	// "sha256:..."). Optional: the reducer's classifyCICDRunEvidence only
	// attempts an image-identity join when this is present
	// (go/internal/reducer/ci_cd_run_correlation.go:377-411); an absent
	// digest is a valid "no digest evidence" observation, not malformed
	// input.
	ArtifactDigest *string `json:"artifact_digest,omitempty"`

	// SizeBytes is the artifact's size in bytes. Optional.
	SizeBytes *int64 `json:"size_bytes,omitempty"`

	// Expired reports whether the provider has expired the artifact.
	// Optional.
	Expired *bool `json:"expired,omitempty"`

	// CreatedAt is the artifact's creation timestamp as an RFC3339 string.
	// Optional.
	CreatedAt *string `json:"created_at,omitempty"`

	// ExpiresAt is the artifact's expiration timestamp as an RFC3339 string.
	// Optional.
	ExpiresAt *string `json:"expires_at,omitempty"`

	// DownloadURL is the artifact's provider download URL, with
	// credential/query-string evidence stripped by the collector. Optional.
	DownloadURL *string `json:"download_url,omitempty"`

	// CorrelationAnchors lists the non-empty run/digest identity segments the
	// collector precomputed for correlation. Optional: not read by the
	// reducer's typed decode path today.
	CorrelationAnchors []string `json:"correlation_anchors,omitempty"`

	// CollectorInstanceID is the collector boundary token. Optional.
	CollectorInstanceID *string `json:"collector_instance_id,omitempty"`
}
