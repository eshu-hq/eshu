// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import (
	"fmt"
	"strings"
	"testing"
)

func assertComposeVolumeContains(t *testing.T, service composeService, want string) {
	t.Helper()

	for _, volume := range service.Volumes {
		if fmt.Sprint(volume) == want {
			return
		}
	}
	t.Fatalf("compose volume %q missing from %#v", want, service.Volumes)
}

func assertComposeScriptContains(t *testing.T, service composeService, want string) {
	t.Helper()

	body := fmt.Sprintf("%#v %#v", service.Entrypoint, service.Command)
	if !strings.Contains(body, want) {
		t.Fatalf("compose script missing %q in %s", want, body)
	}
}

func remoteE2EWorkerPprofServices() []string {
	return []string{
		"bootstrap-index",
		"ingester",
		"resolution-engine",
		"workflow-coordinator",
		"collector-terraform-state",
		"collector-oci-registry",
		"collector-package-registry",
		"collector-sbom-attestation",
		"collector-security-alerts",
		"collector-vulnerability-intelligence",
		"collector-aws-cloud",
		"projector",
		"collector-confluence",
		"scanner-worker",
	}
}

func assertComposePortContains(t *testing.T, service composeService, want string) {
	t.Helper()

	for _, port := range service.Ports {
		if fmt.Sprint(port) == want {
			return
		}
	}
	t.Fatalf("compose port %q missing from %#v", want, service.Ports)
}

func assertComposePortMissing(t *testing.T, service composeService, forbidden string) {
	t.Helper()

	for _, port := range service.Ports {
		if strings.Contains(fmt.Sprint(port), forbidden) {
			t.Fatalf("compose port %q unexpectedly present in %#v", forbidden, service.Ports)
		}
	}
}
