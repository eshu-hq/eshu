// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import "testing"

// TestComposeForwardsGraphWriteBoundsToEveryGraphWriter pins that every
// canonical graph writer receives the write timeout and the in-flight write
// ceiling on both backends. A writer that is never handed
// ESHU_CANONICAL_WRITE_TIMEOUT reads empty, so on Neo4j its transactions stay
// unbounded even when the operator sets the variable for the stack.
func TestComposeForwardsGraphWriteBoundsToEveryGraphWriter(t *testing.T) {
	t.Parallel()

	inFlight := map[string]string{
		"bootstrap-index":   "${ESHU_GRAPH_WRITE_MAX_IN_FLIGHT:-8}",
		"ingester":          "${ESHU_GRAPH_WRITE_MAX_IN_FLIGHT:-8}",
		"resolution-engine": "${ESHU_GRAPH_WRITE_MAX_IN_FLIGHT:-8}",
		"projector":         "${ESHU_GRAPH_WRITE_MAX_IN_FLIGHT:-2}",
	}
	for _, fileName := range []string{"docker-compose.yaml", "docker-compose.neo4j.yml"} {
		doc := readComposeDocument(t, fileName)
		for serviceName, wantInFlight := range inFlight {
			service := requireComposeService(t, doc, serviceName)
			assertComposeEnv(t, service, "ESHU_CANONICAL_WRITE_TIMEOUT", "${ESHU_CANONICAL_WRITE_TIMEOUT:-}")
			assertComposeEnv(t, service, "ESHU_GRAPH_WRITE_MAX_IN_FLIGHT", wantInFlight)
		}
	}
}
