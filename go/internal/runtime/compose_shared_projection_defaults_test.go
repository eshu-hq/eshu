// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import "testing"

func TestComposeUsesDocumentedSharedProjectionDefaults(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		fileName              string
		repoDependencyWorkers string
	}{
		{fileName: "docker-compose.yaml", repoDependencyWorkers: "${ESHU_REPO_DEPENDENCY_PROJECTION_WORKERS:-4}"},
		{fileName: "docker-compose.neo4j.yml", repoDependencyWorkers: "${ESHU_REPO_DEPENDENCY_PROJECTION_WORKERS:-1}"},
	} {
		t.Run(tc.fileName, func(t *testing.T) {
			t.Parallel()

			doc := readComposeDocument(t, tc.fileName)
			service := requireComposeService(t, doc, "resolution-engine")

			assertComposeEnv(t, service, "ESHU_SHARED_PROJECTION_WORKERS", "${ESHU_SHARED_PROJECTION_WORKERS:-4}")
			assertComposeEnv(t, service, "ESHU_SHARED_PROJECTION_PARTITION_COUNT", "${ESHU_SHARED_PROJECTION_PARTITION_COUNT:-8}")
			assertComposeEnv(t, service, "ESHU_REPO_DEPENDENCY_PROJECTION_WORKERS", tc.repoDependencyWorkers)
		})
	}
}
