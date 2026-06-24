// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import (
	"fmt"
	"slices"
	"testing"
)

func TestDefaultComposeWiresComponentRegistryToWorkflowCoordinator(t *testing.T) {
	t.Parallel()

	doc := readComposeDocument(t, "docker-compose.yaml")
	service := requireComposeService(t, doc, "workflow-coordinator")
	assertComposeNamedVolume(t, service, "eshu_data", "/data")
	assertComposeEnv(t, service, "ESHU_COMPONENT_HOME", "/data/.eshu/components")
	assertComposeEnv(t, service, "ESHU_COMPONENT_TRUST_MODE", "${ESHU_COMPONENT_TRUST_MODE:-disabled}")
	assertComposeEnv(t, service, "ESHU_COMPONENT_ALLOW_IDS", "${ESHU_COMPONENT_ALLOW_IDS:-}")
	assertComposeEnv(t, service, "ESHU_COMPONENT_ALLOW_PUBLISHERS", "${ESHU_COMPONENT_ALLOW_PUBLISHERS:-}")
	assertComposeEnv(t, service, "ESHU_COMPONENT_REVOKE_IDS", "${ESHU_COMPONENT_REVOKE_IDS:-}")
	assertComposeEnv(t, service, "ESHU_COMPONENT_REVOKE_PUBLISHERS", "${ESHU_COMPONENT_REVOKE_PUBLISHERS:-}")
	assertComposeEnv(t, service, "ESHU_COMPONENT_CORE_VERSION", "${ESHU_COMPONENT_CORE_VERSION:-}")
}

func TestDefaultComposeWiresComponentExtensionCollector(t *testing.T) {
	t.Parallel()

	doc := readComposeDocument(t, "docker-compose.yaml")
	service := requireComposeService(t, doc, "component-extension-collector")
	if !slices.Contains(service.Profiles, "component-extension-collector") {
		t.Fatalf("profiles = %v, want component-extension-collector", service.Profiles)
	}
	if got, want := fmt.Sprint(service.Command), "[/usr/local/bin/eshu-collector-component-extension]"; got != want {
		t.Fatalf("command = %s, want %s", got, want)
	}
	assertComposeNamedVolume(t, service, "eshu_data", "/data")
	assertComposeEnv(t, service, "ESHU_COMPONENT_HOME", "/data/.eshu/components")
	assertComposeEnv(t, service, "ESHU_COMPONENT_TRUST_MODE", "${ESHU_COMPONENT_TRUST_MODE:-disabled}")
	assertComposeEnv(t, service, "ESHU_COMPONENT_ALLOW_IDS", "${ESHU_COMPONENT_ALLOW_IDS:-}")
	assertComposeEnv(t, service, "ESHU_COMPONENT_ALLOW_PUBLISHERS", "${ESHU_COMPONENT_ALLOW_PUBLISHERS:-}")
	assertComposeEnv(t, service, "ESHU_COMPONENT_COLLECTOR_INSTANCE_ID", "${ESHU_COMPONENT_COLLECTOR_INSTANCE_ID:-}")
	assertComposeEnv(t, service, "ESHU_COMPONENT_COLLECTOR_SCOPE_KIND", "${ESHU_COMPONENT_COLLECTOR_SCOPE_KIND:-component}")
}

func assertComposeNamedVolume(t *testing.T, service composeService, source string, target string) {
	t.Helper()

	for _, volume := range service.Volumes {
		fields, ok := volume.(map[string]any)
		if !ok {
			continue
		}
		if fmt.Sprint(fields["source"]) == source && fmt.Sprint(fields["target"]) == target {
			return
		}
	}
	t.Fatalf("compose service missing volume source=%q target=%q in %#v", source, target, service.Volumes)
}
