// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import (
	"strings"
	"testing"
)

// reconcileInitContainers returns every rendered reconcile-pagerduty-a init
// container: the activation worker's and the workflow coordinator's.
func reconcileInitContainers(t *testing.T, manifests []helmManifest) []map[string]any {
	t.Helper()

	var found []map[string]any
	for _, manifest := range manifests {
		if manifest["kind"] != "Deployment" {
			continue
		}
		for _, init := range helmMapSlice(helmPodSpec(t, manifest)["initContainers"]) {
			if init["name"] == "reconcile-pagerduty-a" {
				found = append(found, init)
			}
		}
	}
	if len(found) < 2 {
		t.Fatalf("reconcile-pagerduty-a init containers = %d, want the worker's and the coordinator's", len(found))
	}
	return found
}

// TestHelmComponentActivationReconcilePassesCoreVersion proves the reconcile
// init containers, which run `eshu component install`, receive
// componentExtensionCollector.coreVersion as ESHU_COMPONENT_CORE_VERSION. On a
// main-branch image the build version is a commit SHA, so without it every
// install fails its compatibleCore check (#9090).
func TestHelmComponentActivationReconcilePassesCoreVersion(t *testing.T) {
	t.Parallel()

	t.Run("set", func(t *testing.T) {
		t.Parallel()

		header := strings.Replace(componentActivationsHeader,
			"  allowPublishers: example\n", "  allowPublishers: example\n  coreVersion: \"0.1.0\"\n", 1)
		if header == componentActivationsHeader {
			t.Fatal("component activations header changed; update the core version test")
		}
		manifests := renderComponentActivations(t, header+"  activations:\n"+pagerDutyActivationValues)
		for _, init := range reconcileInitContainers(t, manifests) {
			env := helmEnvByName(init)
			entry, ok := env["ESHU_COMPONENT_CORE_VERSION"]
			if !ok {
				t.Fatalf("reconcile init container has no ESHU_COMPONENT_CORE_VERSION; env = %v", env)
			}
			if got := entry["value"]; got != "0.1.0" {
				t.Fatalf("ESHU_COMPONENT_CORE_VERSION = %v, want 0.1.0", got)
			}
		}
		// The runtimes that load the installed component check the same range,
		// so the worker and the coordinator must carry the same version.
		worker := requireHelmManifest(t, manifests, "Deployment", "eshu-cx-pagerduty-a")
		assertHelmLiteralEnv(t, helmEnvByName(requireHelmContainer(t, worker, "component-extension-collector")), "ESHU_COMPONENT_CORE_VERSION", "0.1.0")
		coordinator := requireHelmManifest(t, manifests, "Deployment", "eshu-workflow-coordinator")
		assertHelmLiteralEnv(t, helmEnvByName(requireHelmContainer(t, coordinator, "workflow-coordinator")), "ESHU_COMPONENT_CORE_VERSION", "0.1.0")
	})

	t.Run("unset", func(t *testing.T) {
		t.Parallel()

		manifests := renderComponentActivations(t, componentActivationsHeader+"  activations:\n"+pagerDutyActivationValues)
		for _, init := range reconcileInitContainers(t, manifests) {
			if _, ok := helmEnvByName(init)["ESHU_COMPONENT_CORE_VERSION"]; ok {
				t.Fatal("ESHU_COMPONENT_CORE_VERSION rendered with no coreVersion set")
			}
		}
	})
}
