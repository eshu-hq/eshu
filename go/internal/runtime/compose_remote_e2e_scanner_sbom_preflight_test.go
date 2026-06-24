// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import (
	"fmt"
	"strings"
	"testing"
)

func TestRemoteE2EComposeDefinesScannerWorkerSBOMPreflight(t *testing.T) {
	t.Parallel()

	doc := readComposeDocument(t, "docker-compose.remote-e2e.yaml")
	preflight := requireComposeService(t, doc, "remote-e2e-scanner-sbom-preflight")
	if preflight.Image != "alpine:3.21" {
		t.Fatalf("scanner SBOM preflight image = %q, want alpine:3.21", preflight.Image)
	}
	assertComposeEnv(
		t,
		preflight,
		"ESHU_SCANNER_WORKER_SBOM_HOST_ROOT",
		"${ESHU_SCANNER_WORKER_SBOM_HOST_ROOT:-./tests/fixtures/ecosystems}",
	)
	assertComposeEnv(t, preflight, "ESHU_SCANNER_WORKER_SBOM_MOUNTED_ROOT", "/scanner-fixtures")
	assertComposeVolumeContains(
		t,
		preflight,
		"${ESHU_SCANNER_WORKER_SBOM_HOST_ROOT:-./tests/fixtures/ecosystems}:/scanner-fixtures:ro",
	)
	assertComposeVolumeContains(
		t,
		preflight,
		"./scripts/remote-e2e-scanner-sbom-preflight.sh:/usr/local/bin/remote-e2e-scanner-sbom-preflight.sh:ro",
	)
	assertComposeScriptContains(t, preflight, "remote-e2e-scanner-sbom-preflight.sh")

	coordinator := requireComposeService(t, doc, "workflow-coordinator")
	requireComposeDependencyCondition(
		t,
		coordinator,
		"remote-e2e-scanner-sbom-preflight",
		"service_completed_successfully",
	)
}

func TestRemoteE2EScannerWorkerSBOMPreflightScriptContract(t *testing.T) {
	t.Parallel()

	content := readRepositoryFile(t, "../../..", "scripts/remote-e2e-scanner-sbom-preflight.sh")
	for _, want := range []string{
		"ESHU_SCANNER_WORKER_SBOM_MOUNTED_ROOT",
		"ESHU_SCANNER_WORKER_SBOM_HOST_ROOT",
		"supported_manifest_candidates",
		"scanner SBOM preflight passed",
		"scanner SBOM root has no supported manifests",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("scanner SBOM preflight script missing %q", want)
		}
	}
}

func requireComposeDependencyCondition(t *testing.T, service composeService, key, wantCondition string) {
	t.Helper()

	dependencies, ok := service.DependsOn.(map[string]any)
	if !ok {
		t.Fatalf("compose depends_on = %#v, want map", service.DependsOn)
	}
	dependency, ok := dependencies[key].(map[string]any)
	if !ok {
		t.Fatalf("compose dependency %s = %#v, want map", key, dependencies[key])
	}
	if got := fmt.Sprint(dependency["condition"]); got != wantCondition {
		t.Fatalf("compose dependency %s condition = %q, want %q", key, got, wantCondition)
	}
}
