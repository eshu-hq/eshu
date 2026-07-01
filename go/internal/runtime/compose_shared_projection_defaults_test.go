// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import "testing"

func TestComposeUsesDocumentedSharedProjectionDefaults(t *testing.T) {
	t.Parallel()

	for _, fileName := range []string{"docker-compose.yaml", "docker-compose.neo4j.yml"} {
		doc := readComposeDocument(t, fileName)
		service := requireComposeService(t, doc, "resolution-engine")

		assertComposeEnv(t, service, "ESHU_SHARED_PROJECTION_WORKERS", "${ESHU_SHARED_PROJECTION_WORKERS:-4}")
		assertComposeEnv(t, service, "ESHU_SHARED_PROJECTION_PARTITION_COUNT", "${ESHU_SHARED_PROJECTION_PARTITION_COUNT:-8}")
	}
}
