// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package freshness

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/testutil/failure"
)

// TestFreshnessRoutesAnswerFixedServerFailures is the #7674 regression for the
// three freshness reads. Each answered 500 with the store error text, skipped
// the reader-fence 503, and answered a client cancel as a server fault. Not
// parallel: it swaps freshnessHandlerTracer.
func TestFreshnessRoutesAnswerFixedServerFailures(t *testing.T) {
	failure.Run(t, failure.Suite{HandlerTracer: &freshnessHandlerTracer, Routes: []failure.Route{
		{
			Name: "changed since", Path: "/api/v0/freshness/changed-since?scope_id=scope-1&since_generation_id=gen-1",
			Message: changedSinceFailedMessage,
			Handler: func(err error) failure.Mounter {
				return &Handler{ChangedSince: &recordingChangedSinceReader{err: err}}
			},
		},
		{
			Name: "generation lifecycle", Path: "/api/v0/freshness/generations?scope_id=scope-1",
			Message: generationLifecycleFailedMessage,
			Handler: func(err error) failure.Mounter {
				return &Handler{Generations: &recordingGenerationLifecycleReader{err: err}}
			},
		},
		{
			Name: "service changed since", Path: "/api/v0/freshness/services/changed-since?service_id=svc-1&since_generation_id=gen-1",
			Message: serviceChangedSinceFailedMessage,
			Handler: func(err error) failure.Mounter {
				return &Handler{ServiceChangedSince: &recordingServiceChangedSinceReader{err: err}}
			},
		},
	}})
}
