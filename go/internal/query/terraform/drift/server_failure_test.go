// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package drift

import (
	"context"
	"net/http"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/testutil/failure"
)

// TestTerraformConfigStateDriftAnswersFixedServerFailures is the #7674
// regression for both reads of POST
// /api/v0/terraform/config-state-drift/findings. Each answered 500 with the
// store error text, skipped the reader-fence 503, and answered a client cancel
// as a server fault. Not parallel: it swaps driftHandlerTracer.
func TestTerraformConfigStateDriftAnswersFixedServerFailures(t *testing.T) {
	const (
		path = "/api/v0/terraform/config-state-drift/findings"
		body = `{"scope_id":"state_snapshot:s3:hash-1"}`
	)
	failure.Run(t, failure.Suite{
		HandlerTracer: &driftHandlerTracer,
		Routes: []failure.Route{
			{
				Name: "drift count", Method: http.MethodPost, Path: path, Body: body,
				Message: driftFindingsCountFailedMessage,
				Handler: func(err error) failure.Mounter { return &Handler{Store: failingDriftStore{countErr: err}} },
			},
			{
				Name: "drift list", Method: http.MethodPost, Path: path, Body: body,
				Message: driftFindingsListFailedMessage,
				Handler: func(err error) failure.Mounter { return &Handler{Store: failingDriftStore{listErr: err}} },
			},
		},
	})
}

// failingDriftStore fails the finding count or list read with the configured
// error and answers the other step with nothing.
type failingDriftStore struct {
	countErr error
	listErr  error
}

func (s failingDriftStore) ListActiveFindings(context.Context, FindingFilter) ([]FindingRow, error) {
	return nil, s.listErr
}

func (s failingDriftStore) CountActiveFindings(context.Context, FindingFilter) (int, error) {
	return 0, s.countErr
}
