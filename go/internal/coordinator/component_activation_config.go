// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/component"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

const componentInstanceConfigSchema = "eshu.component.instance.v1"

type componentInstanceConfig struct {
	SchemaVersion    string                                 `json:"schema_version"`
	ComponentID      string                                 `json:"component_id"`
	ComponentVersion string                                 `json:"component_version"`
	Publisher        string                                 `json:"publisher"`
	ManifestDigest   string                                 `json:"manifest_digest"`
	ConfigHandle     string                                 `json:"config_handle"`
	Host             *component.ActivationHostClaimMetadata `json:"host,omitempty"`
	Runtime          componentInstanceRuntimeConfig         `json:"runtime"`
}

type componentInstanceRuntimeConfig struct {
	SDKProtocol string `json:"sdk_protocol"`
	Adapter     string `json:"adapter"`
}

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
	config := componentInstanceConfig{
		SchemaVersion:    componentInstanceConfigSchema,
		ComponentID:      manifest.Metadata.ID,
		ComponentVersion: manifest.Metadata.Version,
		Publisher:        manifest.Metadata.Publisher,
		ManifestDigest:   entry.ManifestDigest,
		ConfigHandle:     componentConfigHandle(manifest.Metadata.ID, manifest.Metadata.Version, activation),
		Runtime: componentInstanceRuntimeConfig{
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

func parseComponentInstanceConfig(raw string) (componentInstanceConfig, bool, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return componentInstanceConfig{}, false, nil
	}
	var config componentInstanceConfig
	if err := json.Unmarshal([]byte(trimmed), &config); err != nil {
		return componentInstanceConfig{}, false, fmt.Errorf("decode component activation configuration: %w", err)
	}
	if strings.TrimSpace(config.SchemaVersion) == "" && strings.TrimSpace(config.ComponentID) == "" {
		return componentInstanceConfig{}, false, nil
	}
	if strings.TrimSpace(config.ComponentID) == "" &&
		strings.TrimSpace(config.SchemaVersion) != componentInstanceConfigSchema {
		return componentInstanceConfig{}, false, nil
	}
	if strings.TrimSpace(config.SchemaVersion) != componentInstanceConfigSchema {
		return componentInstanceConfig{}, false, fmt.Errorf(
			"component activation configuration schema_version must be %q",
			componentInstanceConfigSchema,
		)
	}
	if strings.TrimSpace(config.ComponentID) == "" {
		return componentInstanceConfig{}, false, fmt.Errorf("component activation configuration component_id is required")
	}
	if strings.TrimSpace(config.ComponentVersion) == "" {
		return componentInstanceConfig{}, false, fmt.Errorf("component activation configuration component_version is required")
	}
	if strings.TrimSpace(config.ManifestDigest) == "" {
		return componentInstanceConfig{}, false, fmt.Errorf("component activation configuration manifest_digest is required")
	}
	if strings.TrimSpace(config.ConfigHandle) == "" {
		return componentInstanceConfig{}, false, fmt.Errorf("component activation configuration config_handle is required")
	}
	if config.Host != nil {
		host := config.Host.Normalized()
		if host.Empty() {
			config.Host = nil
		} else {
			if err := host.Validate(); err != nil {
				return componentInstanceConfig{}, false, err
			}
			config.Host = &host
		}
	}
	if strings.TrimSpace(config.Runtime.SDKProtocol) == "" {
		return componentInstanceConfig{}, false, fmt.Errorf("component activation configuration runtime.sdk_protocol is required")
	}
	if strings.TrimSpace(config.Runtime.SDKProtocol) != component.CollectorSDKProtocolV1Alpha1 {
		return componentInstanceConfig{}, false, fmt.Errorf(
			"component activation configuration runtime.sdk_protocol %q is unsupported",
			config.Runtime.SDKProtocol,
		)
	}
	if strings.TrimSpace(config.Runtime.Adapter) == "" {
		return componentInstanceConfig{}, false, fmt.Errorf("component activation configuration runtime.adapter is required")
	}
	switch strings.TrimSpace(config.Runtime.Adapter) {
	case component.RuntimeAdapterOCI, component.RuntimeAdapterProcess:
	default:
		return componentInstanceConfig{}, false, fmt.Errorf(
			"component activation configuration runtime.adapter %q is unsupported",
			config.Runtime.Adapter,
		)
	}
	return config, true, nil
}
