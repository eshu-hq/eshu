// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codeowners

import "testing"

// TestCodeownersOwnershipAnswersFixedServerFailures is the #7674 regression
// for both reads of GET /api/v0/codeowners/ownership. Each answered 500 with
// the graph error text and answered a client cancel as a server fault. Not
// parallel: it swaps queryHandlerTracer.
func TestCodeownersOwnershipAnswersFixedServerFailures(t *testing.T) {
	const path = "/api/v0/codeowners/ownership?repository_id=repository:r_payments&limit=10"
	runServerFailureRoutes(t, []serverFailureRoute{
		{
			name: "ownership rows", path: path, message: codeownersOwnershipReadFailedMessage,
			handler: func(err error) serverFailureMounter {
				return &Handler{Neo4j: &recordingCodeownersGraphReader{runErr: err}}
			},
		},
		{
			name: "effective owner", path: path, message: codeownersEffectiveOwnerFailedMessage,
			handler: func(err error) serverFailureMounter {
				return &Handler{Neo4j: &recordingCodeownersGraphReader{singleErr: err}}
			},
		},
	})
}
