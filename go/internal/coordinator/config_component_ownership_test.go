// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func activeConfigWithInstance(instance workflow.DesiredCollectorInstance) Config {
	return Config{
		DeploymentMode:     deploymentModeActive,
		ClaimsEnabled:      true,
		CollectorInstances: []workflow.DesiredCollectorInstance{instance},
	}
}

func desiredSelectorInstance(kind scope.CollectorKind, configuration string) workflow.DesiredCollectorInstance {
	return workflow.DesiredCollectorInstance{
		InstanceID:    "startup-" + string(kind),
		CollectorKind: kind,
		Mode:          workflow.CollectorModeScheduled,
		Enabled:       true,
		ClaimsEnabled: true,
		Configuration: configuration,
	}
}

// TestConfigValidateAcceptsComponentConfiguredGCPInstance is the #8919 startup
// regression: an enabled, claims-enabled gcp-kind instance configured as an
// eshu.component.instance.v1 document belongs to the generic component
// scheduler, so the GCP claim validator must not read it as GCP configuration
// and stop the coordinator at startup.
func TestConfigValidateAcceptsComponentConfiguredGCPInstance(t *testing.T) {
	t.Parallel()

	instance := desiredSelectorInstance(scope.CollectorGCP, testComponentActivationConfig)
	instance.InstanceID = "gcp-comp"
	if err := activeConfigWithInstance(instance).Validate(); err != nil {
		t.Fatalf("Config.Validate() error = %v, want nil for a component-configured gcp instance", err)
	}
}

// TestConfigValidateKeepsGCPClaimValidationForInTreeConfiguration pins the
// other half of the partition: a gcp-kind instance that is not component
// configured still needs live_collection_enabled=true and an enabled scope.
func TestConfigValidateKeepsGCPClaimValidationForInTreeConfiguration(t *testing.T) {
	t.Parallel()

	for name, configuration := range inTreeOwnedConfigurations {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			instance := desiredSelectorInstance(scope.CollectorGCP, configuration)
			err := activeConfigWithInstance(instance).Validate()
			if err == nil || !strings.Contains(err.Error(), "live_collection_enabled") {
				t.Fatalf("Config.Validate() error = %v, want the gcp live_collection_enabled error", err)
			}
		})
	}
}

// TestConfigValidateComponentConfiguredInstanceOfEveryKind is the sweep of
// every startup reader of Configuration that dispatches on collector kind: a
// valid component-configured instance of any kind must pass Config.Validate in
// an active coordinator, so no kind-specific validator reads the component
// document as its own configuration.
func TestConfigValidateComponentConfiguredInstanceOfEveryKind(t *testing.T) {
	t.Parallel()

	for _, kind := range scope.AllCollectorKinds() {
		t.Run(string(kind), func(t *testing.T) {
			t.Parallel()

			instance := desiredSelectorInstance(kind, testComponentActivationConfig)
			if err := activeConfigWithInstance(instance).Validate(); err != nil {
				t.Fatalf("Config.Validate() error = %v, want nil for a component-configured %s instance", err, kind)
			}
		})
	}
}
