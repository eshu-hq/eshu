// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factload

import (
	"context"
)

// WorkloadCandidateSignalKeys names the parsed_file_data keys that can feed
// workload-candidate signals in reducer.ExtractWorkloadCandidates
// (k8s_resources, ArgoCD buckets, chart_type, dockerfile_stages,
// pipeline_calls, artifact_type, compose keys, CloudFormation keys). The
// candidate-precondition probe first requires a file fact holding ANY of
// these keys, then tests the value the way extraction reads it (see
// WorkloadCandidateArrayBucketKeys, WorkloadCandidatePresenceKeys and
// WorkloadCandidateTokenKeys). Parsers write every key on every file of a
// kind, as JSON null or an empty list when the file holds no such evidence
// (#9986: QA showed k8s_resources stored as null), so key existence alone
// says nothing about the file.
//
// When extraction learns a new parsed_file_data signal, add its key here and
// to exactly one value class below; the differential live test in
// go/internal/storage/postgres fails when the probe answers false for a
// generation extraction turns into a candidate.
var WorkloadCandidateSignalKeys = []string{
	"k8s_resources",
	"argocd_applications",
	"argocd_applicationsets",
	"chart_type",
	"dockerfile_stages",
	"pipeline_calls",
	"artifact_type",
	"docker_compose_services",
	"cloudformation_resources",
	"cloudformation_parameters",
	"cloudformation_outputs",
	"cloudformation_cross_stack_imports",
	"cloudformation_cross_stack_exports",
}

// WorkloadCandidateArrayBucketKeys are the signal keys extraction reads as
// a list and counts only when it is non-empty (len(list) > 0 in
// reducer/candidate_loader.go). A JSON null, an empty list, or any
// non-list value contributes nothing.
var WorkloadCandidateArrayBucketKeys = []string{
	"k8s_resources",
	"argocd_applications",
	"argocd_applicationsets",
	"dockerfile_stages",
	"pipeline_calls",
	"docker_compose_services",
	"cloudformation_resources",
	"cloudformation_parameters",
	"cloudformation_outputs",
	"cloudformation_cross_stack_imports",
	"cloudformation_cross_stack_exports",
}

// WorkloadCandidatePresenceKeys are the signal keys extraction counts when
// the value renders to a non-blank string other than the nil marker
// (chart_type: any present, non-null, non-blank value marks a Helm chart).
var WorkloadCandidatePresenceKeys = []string{"chart_type"}

// WorkloadCandidateTokenKeys are the signal keys extraction counts only when
// the trimmed string value equals a known artifact type (artifact_type:
// docker_compose or github_actions_workflow).
var WorkloadCandidateTokenKeys = []string{"artifact_type"}

// WorkloadCandidateProbe is an optional fast path for loaders that can tell
// cheaply whether one scope generation may hold workload candidates, before
// anyone pays for the bulk fact load. Loaders that do not implement it keep
// the existing load-then-gate behavior.
//
// Contract: ScopeMayHoldWorkloadCandidates reports false only when the
// generation cannot yield candidates (no qualifying repository identity
// joined to a file fact whose parsed payload carries a signal value
// extraction reads, or the Jenkinsfile path convention). The caller reads
// the answer one way: true runs the cheap readiness gates BEFORE the bulk
// fact load and may defer there (the #9700 shed); false, an error, or an
// unimplemented probe all load and gate normally, so a candidate-free
// generation keeps its vacuous success. A false answer for a generation
// that would yield candidates only misses a saving (the post-load gates
// still run). A true answer for a candidate-free generation can defer it,
// non-counting, until its own relationship generation activates or its
// producer reaches a terminal state; it can never count, because the pre-gate
// returns nil on a counting verdict and the post-load gate decides on real
// candidates.
type WorkloadCandidateProbe interface {
	ScopeMayHoldWorkloadCandidates(ctx context.Context, scopeID, generationID string) (bool, error)
}
