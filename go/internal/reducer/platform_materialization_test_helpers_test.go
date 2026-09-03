// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import "context"

// These doubles serve the reducer-root tests that wire the deployment_mapping
// handler through the default domain catalog (defaults, materialization
// sub-duration, semantic-entity and repo-dependency replay coverage). The
// handler itself moved to internal/reducer/platformfam (#6061) and carries its
// own copies for its own tests; a test double cannot be shared across packages
// without exporting it, which would put test-only types on the family's API.

// recordingPlatformMaterializationWriter records the canonical write requests
// the deployment_mapping handler issues and returns a canned result.
type recordingPlatformMaterializationWriter struct {
	requests []PlatformMaterializationWrite
	result   PlatformMaterializationWriteResult
	err      error
}

func (w *recordingPlatformMaterializationWriter) WritePlatformMaterialization(
	_ context.Context,
	request PlatformMaterializationWrite,
) (PlatformMaterializationWriteResult, error) {
	w.requests = append(w.requests, request)
	return w.result, w.err
}

// recordingWorkloadMaterializationReplayer records the workload-materialization
// replays the deployment_mapping handler requests after cross-repo resolution
// writes canonical edges.
type recordingWorkloadMaterializationReplayer struct {
	calls []workloadMaterializationReplayCall
	err   error
}

// workloadMaterializationReplayCall is one recorded replay request.
type workloadMaterializationReplayCall struct {
	scopeID      string
	generationID string
	entityKey    string
}

func (r *recordingWorkloadMaterializationReplayer) ReplayWorkloadMaterialization(
	_ context.Context,
	scopeID string,
	generationID string,
	entityKey string,
) (bool, error) {
	r.calls = append(r.calls, workloadMaterializationReplayCall{
		scopeID:      scopeID,
		generationID: generationID,
		entityKey:    entityKey,
	})
	return true, r.err
}
