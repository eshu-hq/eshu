// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package dependency

import "testing"

// TestDependenciesAnswerFixedServerFailures is the #7674 regression for
// GET /api/v0/dependencies. It answered 500 with the graph error text and
// answered a client cancel as a server fault. Not parallel: it swaps
// dependencyHandlerTracer.
func TestDependenciesAnswerFixedServerFailures(t *testing.T) {
	runServerFailureRoutes(t, []serverFailureRoute{{
		name: "dependencies", path: "/api/v0/dependencies?direction=forward&limit=10",
		message: dependenciesReadFailedMessage,
		handler: func(err error) serverFailureMounter {
			return &Handler{Neo4j: &recordingDependenciesGraphReader{err: err}}
		},
	}})
}
