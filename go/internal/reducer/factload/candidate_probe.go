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
// candidate-precondition probe matches a file fact holding ANY of these keys.
// It is deliberately a structural superset check, not a signal-equality
// check: a bare Jenkinsfile path with no parsed payload still yields a
// candidate through the path convention alone, so that shape probes false
// and the caller loads normally (missed saving, never a wrong deferral).
// When extraction learns a new parsed_file_data signal, add its key here;
// a missing key only ever misses a saving, while a listed key extraction no
// longer reads only risks deferring an empty scope during a resolution
// storm (still truth-safe: the queue retries it to success).
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

// WorkloadCandidateProbe is an optional fast path for loaders that can tell
// cheaply whether one scope generation may hold workload candidates, before
// anyone pays for the bulk fact load. Loaders that do not implement it keep
// the existing load-then-gate behavior.
//
// Contract: ScopeMayHoldWorkloadCandidates reports false only when the
// generation cannot yield candidates (no qualifying repository identity
// joined to a signal-bearing file fact, mirroring the structural
// preconditions of candidate extraction). False lets the caller take the
// vacuous-gate success path without consulting readiness lookups; true, an
// error, or an unimplemented probe all mean the caller loads and gates
// normally. A false answer for a generation that would yield candidates
// only misses a saving; a true answer for an empty generation only risks a
// cheap storm-window deferral that the queue retries to the same success.
type WorkloadCandidateProbe interface {
	ScopeMayHoldWorkloadCandidates(ctx context.Context, scopeID, generationID string) (bool, error)
}
