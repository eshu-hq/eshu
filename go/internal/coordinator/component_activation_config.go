// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/component"
	"github.com/eshu-hq/eshu/go/internal/coordinator/componentactivation"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func componentCollectorInstancesFromEnv(getenv func(string) string) ([]workflow.DesiredCollectorInstance, error) {
	home := strings.TrimSpace(getenv("ESHU_COMPONENT_HOME"))
	if home == "" {
		return nil, nil
	}
	registry := component.NewRegistry(home)
	readback, err := registry.Readback(componentPolicyFromEnv(getenv))
	if err != nil {
		return nil, fmt.Errorf("read component registry: %w", err)
	}
	instances := make([]workflow.DesiredCollectorInstance, 0)
	for _, entry := range readback {
		if entry.Error != nil || entry.Verification == nil || !entry.Verification.Allowed {
			continue
		}
		manifest, err := component.LoadManifest(entry.ManifestPath)
		if err != nil {
			return nil, fmt.Errorf("load component manifest %q: %w", entry.ID, err)
		}
		componentInstances, err := desiredInstancesForComponent(entry, manifest)
		if err != nil {
			return nil, err
		}
		instances = append(instances, componentInstances...)
	}
	return instances, nil
}

func componentPolicyFromEnv(getenv func(string) string) component.Policy {
	mode := strings.TrimSpace(getenv("ESHU_COMPONENT_TRUST_MODE"))
	if mode == "" {
		mode = component.TrustModeDisabled
	}
	return component.ConfigureProvenanceFromEnv(component.Policy{
		Mode:              mode,
		AllowedIDs:        envStringList(getenv("ESHU_COMPONENT_ALLOW_IDS")),
		AllowedPublishers: envStringList(getenv("ESHU_COMPONENT_ALLOW_PUBLISHERS")),
		RevokedIDs:        envStringList(getenv("ESHU_COMPONENT_REVOKE_IDS")),
		RevokedPublishers: envStringList(getenv("ESHU_COMPONENT_REVOKE_PUBLISHERS")),
		CoreVersion:       strings.TrimSpace(getenv("ESHU_COMPONENT_CORE_VERSION")),
	}, getenv)
}

func desiredInstancesForComponent(
	entry component.RegistryReadbackComponent,
	manifest component.Manifest,
) ([]workflow.DesiredCollectorInstance, error) {
	if len(entry.Activations) == 0 {
		return nil, nil
	}
	if len(manifest.Spec.CollectorKinds) != 1 {
		return nil, fmt.Errorf(
			"component %q hosted activation requires exactly one collector kind, got %d",
			manifest.Metadata.ID,
			len(manifest.Spec.CollectorKinds),
		)
	}
	collectorKind := scope.CollectorKind(strings.TrimSpace(manifest.Spec.CollectorKinds[0]))
	instances := make([]workflow.DesiredCollectorInstance, 0, len(entry.Activations))
	for _, activation := range entry.Activations {
		if !activation.ClaimsEnabled {
			continue
		}
		config, err := componentActivationRuntimeConfig(entry, manifest, activation)
		if err != nil {
			return nil, err
		}
		instances = append(instances, workflow.DesiredCollectorInstance{
			InstanceID:    strings.TrimSpace(activation.InstanceID),
			CollectorKind: collectorKind,
			Mode:          workflow.CollectorMode(strings.TrimSpace(activation.Mode)),
			Enabled:       true,
			ClaimsEnabled: true,
			DisplayName:   strings.TrimSpace(manifest.Metadata.Name),
			Configuration: config,
		})
	}
	return instances, nil
}

func componentActivationRuntimeConfig(
	entry component.RegistryReadbackComponent,
	manifest component.Manifest,
	activation component.Activation,
) (string, error) {
	host, ok, err := component.LoadActivationHostClaimMetadata(activation.ConfigPath)
	if err != nil {
		return "", fmt.Errorf(
			"load component activation host metadata for %q: %w",
			strings.TrimSpace(activation.InstanceID),
			err,
		)
	}
	config := componentactivation.Config{
		SchemaVersion:    componentactivation.ConfigSchema,
		ComponentID:      manifest.Metadata.ID,
		ComponentVersion: manifest.Metadata.Version,
		Publisher:        manifest.Metadata.Publisher,
		ManifestDigest:   entry.ManifestDigest,
		ConfigHandle:     componentConfigHandle(manifest.Metadata.ID, manifest.Metadata.Version, activation),
		Runtime: componentactivation.RuntimeConfig{
			SDKProtocol: manifest.Spec.Runtime.SDKProtocol,
			Adapter:     manifest.Spec.Runtime.Adapter,
		},
	}
	if ok {
		config.Host = &host
	}
	raw, err := json.Marshal(config)
	if err != nil {
		return "", fmt.Errorf("encode component activation configuration: %w", err)
	}
	return string(raw), nil
}

func componentConfigHandle(componentID string, version string, activation component.Activation) string {
	return component.ActivationConfigHandle(componentID, version, activation)
}

func envStringList(raw string) []string {
	fields := strings.Split(raw, ",")
	values := make([]string, 0, len(fields))
	for _, field := range fields {
		trimmed := strings.TrimSpace(field)
		if trimmed != "" {
			values = append(values, trimmed)
		}
	}
	return values
}

func mergeCollectorInstances(
	static []workflow.DesiredCollectorInstance,
	components []workflow.DesiredCollectorInstance,
) ([]workflow.DesiredCollectorInstance, error) {
	if len(components) == 0 {
		return static, nil
	}
	merged := make([]workflow.DesiredCollectorInstance, 0, len(static)+len(components))
	seen := make(map[string]struct{}, len(static)+len(components))
	for _, instance := range append(static, components...) {
		key := strings.TrimSpace(instance.InstanceID)
		if _, ok := seen[key]; ok {
			return nil, fmt.Errorf("duplicate collector instance %q", key)
		}
		seen[key] = struct{}{}
		merged = append(merged, instance)
	}
	return merged, nil
}
