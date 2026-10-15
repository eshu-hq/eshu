// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package dependency

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/testutil/failure"
)

// TestDependenciesAnswerFixedServerFailures is the #7674 regression for
// GET /api/v0/dependencies. It answered 500 with the graph error text and
// answered a client cancel as a server fault. Not parallel: it swaps
// dependencyHandlerTracer.
func TestDependenciesAnswerFixedServerFailures(t *testing.T) {
	failure.Run(t, failure.Suite{HandlerTracer: &dependencyHandlerTracer, Routes: []failure.Route{{
		Name: "dependencies", Path: "/api/v0/dependencies?direction=forward&limit=10",
		Message: dependenciesReadFailedMessage,
		Handler: func(err error) failure.Mounter {
			return &Handler{Neo4j: &recordingDependenciesGraphReader{err: err}}
		},
	}}})
}
