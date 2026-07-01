// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import "testing"

func TestComposeUsesDocumentedLargeRepoConcurrencyDefault(t *testing.T) {
	t.Parallel()

	for _, fileName := range []string{"docker-compose.yaml", "docker-compose.neo4j.yml"} {
		doc := readComposeDocument(t, fileName)
		for _, serviceName := range []string{"bootstrap-index", "ingester"} {
			service := requireComposeService(t, doc, serviceName)
			assertComposeEnv(t, service, "ESHU_LARGE_REPO_MAX_CONCURRENT", "${ESHU_LARGE_REPO_MAX_CONCURRENT:-2}")
		}
	}
}
