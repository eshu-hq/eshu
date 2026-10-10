// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package freshness

import "testing"

// TestFreshnessRoutesAnswerFixedServerFailures is the #7674 regression for the
// three freshness reads. Each answered 500 with the store error text, skipped
// the reader-fence 503, and answered a client cancel as a server fault. Not
// parallel: it swaps freshnessHandlerTracer.
func TestFreshnessRoutesAnswerFixedServerFailures(t *testing.T) {
	runServerFailureRoutes(t, []serverFailureRoute{
		{
			name: "changed since", path: "/api/v0/freshness/changed-since?scope_id=scope-1&since_generation_id=gen-1",
			message: changedSinceFailedMessage,
			handler: func(err error) serverFailureMounter {
				return &Handler{ChangedSince: &recordingChangedSinceReader{err: err}}
			},
		},
		{
			name: "generation lifecycle", path: "/api/v0/freshness/generations?scope_id=scope-1",
			message: generationLifecycleFailedMessage,
			handler: func(err error) serverFailureMounter {
				return &Handler{Generations: &recordingGenerationLifecycleReader{err: err}}
			},
		},
		{
			name: "service changed since", path: "/api/v0/freshness/services/changed-since?service_id=svc-1&since_generation_id=gen-1",
			message: serviceChangedSinceFailedMessage,
			handler: func(err error) serverFailureMounter {
				return &Handler{ServiceChangedSince: &recordingServiceChangedSinceReader{err: err}}
			},
		},
	})
}
