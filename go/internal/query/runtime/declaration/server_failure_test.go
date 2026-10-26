// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package declaration

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/testutil/failure"
)

// TestListAnswersFixedServerFailures is the #7674 contract for the list route:
// a failed graph read answers a fixed per-step body, never the backend error,
// and a client cancel is not a server fault. Not parallel: it swaps
// declarationHandlerTracer.
func TestListAnswersFixedServerFailures(t *testing.T) {
	failure.Run(t, failure.Suite{HandlerTracer: &declarationHandlerTracer, Routes: []failure.Route{
		{
			Name: "page", Method: "POST", Path: "/api/v0/runtime-declarations/query", Body: `{"runtime":"node"}`,
			Message: pageReadFailedMessage,
			Handler: func(err error) failure.Mounter { return &Handler{Neo4j: &fakeGraph{pageErr: err}} },
		},
		{
			Name: "excluded count", Method: "POST", Path: "/api/v0/runtime-declarations/query",
			Body:    `{"runtime":"node","major_below":20}`,
			Message: excludedReadFailedMessage, CancelMessage: excludedReadFailedMessage,
			Handler: func(err error) failure.Mounter { return &Handler{Neo4j: &fakeGraph{excludedErr: err}} },
		},
	}})
}
