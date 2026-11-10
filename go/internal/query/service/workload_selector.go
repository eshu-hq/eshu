// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package service

// WorkloadSelector is the exported selector for callers outside the
// service family that need the service-story dossier directly (for example
// the service intelligence report composer) rather than via the HTTP
// handler. It moved here from the query root (service_story_seam.go) with
// the service family (Issue #6060, lane B B4); the staying EntityHandler
// seam keeps spelling it through the root alias. Pinned by
// internal/serviceintelhttp via that alias.
type WorkloadSelector struct {
	// ServiceName is the canonical service name. Required.
	ServiceName string
	// ServiceID narrows resolution to a specific service identifier.
	ServiceID string
	// Repository narrows resolution to a specific repository.
	Repository string
	// Environment narrows resolution to a specific environment.
	Environment string
	// DownstreamReadLimit names the indirect-evidence row bound directly
	// (#10477). Zero keeps the legacy MaxDepth-derived default; a positive
	// value replaces it via querycontract.BoundedDownstreamReadLimit and is
	// echoed on the workload context so the result limits and the
	// investigation coverage report the effective bound. It does not take
	// part in workload resolution.
	DownstreamReadLimit int
}
