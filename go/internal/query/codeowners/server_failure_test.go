// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codeowners

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/testutil/failure"
)

// TestCodeownersOwnershipAnswersFixedServerFailures is the #7674 regression
// for both reads of GET /api/v0/codeowners/ownership. Each answered 500 with
// the graph error text and answered a client cancel as a server fault. Not
// parallel: it swaps queryHandlerTracer.
func TestCodeownersOwnershipAnswersFixedServerFailures(t *testing.T) {
	const path = "/api/v0/codeowners/ownership?repository_id=repository:r_payments&limit=10"
	failure.Run(t, failure.Suite{HandlerTracer: &queryHandlerTracer, Routes: []failure.Route{
		{
			Name: "ownership rows", Path: path, Message: codeownersOwnershipReadFailedMessage,
			Handler: func(err error) failure.Mounter {
				return &Handler{Neo4j: &recordingCodeownersGraphReader{runErr: err}}
			},
		},
		{
			Name: "effective owner", Path: path, Message: codeownersEffectiveOwnerFailedMessage,
			Handler: func(err error) failure.Mounter {
				return &Handler{Neo4j: &recordingCodeownersGraphReader{singleErr: err}}
			},
		},
	}})
}
