// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import (
	"strings"
	"testing"
)

func TestRemoteE2EComposeConfiguresCodeCallProjectionConcurrency(t *testing.T) {
	t.Parallel()

	doc := readComposeDocument(t, "docker-compose.remote-e2e.yaml")
	resolutionEngine := requireComposeService(t, doc, "resolution-engine")

	assertComposeEnv(
		t,
		resolutionEngine,
		"ESHU_CODE_CALL_PROJECTION_PARTITION_COUNT",
		"${ESHU_CODE_CALL_PROJECTION_PARTITION_COUNT:-4}",
	)
	assertComposeEnv(
		t,
		resolutionEngine,
		"ESHU_CODE_CALL_PROJECTION_WORKERS",
		"${ESHU_CODE_CALL_PROJECTION_WORKERS:-2}",
	)
	assertComposeEnv(
		t,
		resolutionEngine,
		"ESHU_REPO_DEPENDENCY_PROJECTION_WORKERS",
		"${ESHU_REPO_DEPENDENCY_PROJECTION_WORKERS:-4}",
	)
}

func TestRemoteE2EExampleEnvDocumentsCodeCallProjectionConcurrency(t *testing.T) {
	t.Parallel()

	content := readRepositoryFile(t, "../../..", ".env.remote-e2e.example")
	for _, want := range []string{
		"ESHU_CODE_CALL_PROJECTION_PARTITION_COUNT=4",
		"ESHU_CODE_CALL_PROJECTION_WORKERS=2",
		"ESHU_REPO_DEPENDENCY_PROJECTION_WORKERS=4",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf(".env.remote-e2e.example missing %q", want)
		}
	}
}
