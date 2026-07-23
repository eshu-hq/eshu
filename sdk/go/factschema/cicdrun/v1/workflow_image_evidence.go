// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

// WorkflowImageEvidence is the schema-version-1 typed payload for the
// "ci.workflow_image_evidence" fact kind: one static workflow command
// evidence row the git collector extracts from a checked-in GitHub Actions
// workflow file (go/internal/collector/git_workflow_image_facts.go), distinct
// from the ci_cd_run collector's provider-run facts (Run, Artifact,
// EnvironmentObservation, TriggerEdge, Step in this package).
//
// RepositoryID is the only required field: the collector's
// workflowImageEvidenceFactEnvelope always sets it from the scanned
// repository, and it is the reducer's own join key
// (attachWorkflowImagesToRuns,
// go/internal/reducer/ci_cd_run_correlation_workflow_image.go:8-24) that
// attaches workflow image evidence to every run sharing the same
// RepositoryID. A fact whose repository_id is absent could never join to any
// run, so a decode-time guarantee here replaces the pre-typing empty-string
// join-key collapse (which would silently attach to zero runs, since no run
// fact carries an empty RepositoryID by construction).
type WorkflowImageEvidence struct {
	// RepositoryID is the reducer-facing repository locator this evidence
	// was extracted for. Required — the reducer's sole join key attaching
	// workflow image evidence to a run.
	RepositoryID string `json:"repository_id"`

	// WorkflowPath is the workflow file's repository-relative path.
	// Optional: always emitted by the collector, but not a reducer join
	// key.
	WorkflowPath *string `json:"workflow_path,omitempty"`

	// CommitSHA is the git commit the workflow file was extracted at (the
	// scanned repository snapshot's commit). Optional and additive (#5424):
	// the reducer's attachWorkflowImagesToRuns prefers a run whose own
	// CommitSHA matches this value over the commit-blind repository-wide
	// fan-out, so a workflow file declared on one branch does not lend a
	// false-confident image correlation to a run built from another branch.
	// A workflow-image fact from a collector that does not stamp the commit
	// keeps the prior repository-wide fallback behavior.
	CommitSHA *string `json:"commit_sha,omitempty"`

	// CommandKind classifies the workflow command the evidence was extracted
	// from (for example a `uses:` reusable-workflow reference or a `run:`
	// shell command). Optional.
	CommandKind *string `json:"command_kind,omitempty"`

	// EvidenceClass classifies the confidence of the extracted image
	// reference: "workflow_image_ref" (exactly one resolvable image),
	// "workflow_image_unresolved" (only templated/variable refs), or
	// "workflow_image_ambiguous" (multiple candidate refs). Optional: the
	// reducer's classifyCICDWorkflowImageEvidence only attempts an
	// image-identity join when this equals "workflow_image_ref"
	// (go/internal/reducer/ci_cd_run_correlation_workflow_image.go:31-38,
	// ci_cd_run_correlation.go:290), so any other value (or an absent one)
	// is a valid "not a resolvable single ref" observation, not malformed
	// input.
	EvidenceClass *string `json:"evidence_class,omitempty"`

	// JobName is the workflow job name the evidence was extracted from.
	// Optional.
	JobName *string `json:"job_name,omitempty"`

	// StepName is the workflow step name the evidence was extracted from.
	// Optional.
	StepName *string `json:"step_name,omitempty"`

	// ImageRef is the single resolvable image reference, set when
	// EvidenceClass=="workflow_image_ref". Optional: the reducer's join path
	// only reads this when EvidenceClass matches, mirroring the collector's
	// own contract that ImageRef is populated exactly when the evidence
	// class is resolvable (go/internal/workflowimage.Evidence.ImageRef).
	ImageRef *string `json:"image_ref,omitempty"`

	// ImageRefs lists every candidate image reference when the evidence is
	// ambiguous (EvidenceClass=="workflow_image_ambiguous"). Optional: not
	// read by the reducer's typed decode path today (an ambiguous workflow
	// image fact is not currently joined to a run), modeled for contract
	// completeness.
	ImageRefs []string `json:"image_refs,omitempty"`

	// Reason records why the collector could not resolve a single image
	// reference, when EvidenceClass is unresolved or ambiguous. Optional.
	Reason *string `json:"reason,omitempty"`
}
