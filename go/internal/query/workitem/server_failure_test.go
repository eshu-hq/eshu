// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workitem

import (
	"context"
	"testing"
)

// TestWorkItemEvidenceAnswersFixedServerFailures is the #7674 regression for
// GET /api/v0/work-items/evidence. It answered 500 with the store error text,
// skipped the reader-fence 503, and answered a client cancel as a server
// fault. Not parallel: it swaps workitemHandlerTracer.
func TestWorkItemEvidenceAnswersFixedServerFailures(t *testing.T) {
	runServerFailureRoutes(t, []serverFailureRoute{{
		name: "work-item evidence", path: "/api/v0/work-items/evidence?scope_id=scope-1&limit=10",
		message: workItemEvidenceListFailedMessage,
		handler: func(err error) serverFailureMounter {
			return &Handler{Evidence: erroringWorkItemEvidenceStore{err: err}}
		},
	}})
}

// erroringWorkItemEvidenceStore fails the evidence read with err.
type erroringWorkItemEvidenceStore struct{ err error }

func (s erroringWorkItemEvidenceStore) ListWorkItemEvidence(context.Context, EvidenceFilter) (EvidencePage, error) {
	return EvidencePage{}, s.err
}
