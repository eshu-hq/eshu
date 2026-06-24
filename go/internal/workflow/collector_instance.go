// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workflow

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
)

const componentInstanceConfigSchema = "eshu.component.instance.v1"

const (
	componentCollectorSDKProtocolV1Alpha1 = "collector-sdk/v1alpha1"
	componentRuntimeAdapterOCI            = "oci"
	componentRuntimeAdapterProcess        = "process"
)

type componentActivationConfiguration struct {
	SchemaVersion    string `json:"schema_version"`
	ComponentID      string `json:"component_id"`
	ComponentVersion string `json:"component_version"`
	ManifestDigest   string `json:"manifest_digest"`
	ConfigHandle     string `json:"config_handle"`
	Runtime          struct {
		SDKProtocol string `json:"sdk_protocol"`
		Adapter     string `json:"adapter"`
	} `json:"runtime"`
}

// DesiredCollectorInstance is the declarative source-of-truth shape reconciled
// into durable collector_instances rows.
type DesiredCollectorInstance struct {
	InstanceID    string
	CollectorKind scope.CollectorKind
	Mode          CollectorMode
	Enabled       bool
	Bootstrap     bool
	ClaimsEnabled bool
	DisplayName   string
	Configuration string
}

// Validate checks that the desired collector instance is well formed.
func (d DesiredCollectorInstance) Validate() error {
	if err := validateIdentifier("instance_id", d.InstanceID); err != nil {
		return err
	}
	if err := validateIdentifier("collector_kind", string(d.CollectorKind)); err != nil {
		return err
	}
	if err := d.Mode.Validate(); err != nil {
		return err
	}
	if err := validateJSONDocument("configuration", d.Configuration); err != nil {
		return err
	}
	if err := validateDesiredCollectorConfiguration(d.CollectorKind, d.Enabled, d.Configuration); err != nil {
		return err
	}
	return nil
}

// CollectorInstance is the durable row shape for one reconciled collector
// runtime instance.
type CollectorInstance struct {
	InstanceID     string
	CollectorKind  scope.CollectorKind
	Mode           CollectorMode
	Enabled        bool
	Bootstrap      bool
	ClaimsEnabled  bool
	DisplayName    string
	Configuration  string
	LastObservedAt time.Time
	DeactivatedAt  time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// Validate checks that the stored collector instance has durable identity.
func (i CollectorInstance) Validate() error {
	if err := validateIdentifier("instance_id", i.InstanceID); err != nil {
		return err
	}
	if err := validateIdentifier("collector_kind", string(i.CollectorKind)); err != nil {
		return err
	}
	if err := i.Mode.Validate(); err != nil {
		return err
	}
	if err := validateJSONDocument("configuration", i.Configuration); err != nil {
		return err
	}
	if err := validateDurableCollectorConfiguration(i.CollectorKind, i.Enabled, i.Configuration); err != nil {
		return err
	}
	if err := validateTime("last_observed_at", i.LastObservedAt); err != nil {
		return err
	}
	if err := validateTime("created_at", i.CreatedAt); err != nil {
		return err
	}
	if err := validateTime("updated_at", i.UpdatedAt); err != nil {
		return err
	}
	if i.UpdatedAt.Before(i.CreatedAt) {
		return fmt.Errorf("updated_at must not be before created_at")
	}
	if i.LastObservedAt.Before(i.CreatedAt) {
		return fmt.Errorf("last_observed_at must not be before created_at")
	}
	if !i.DeactivatedAt.IsZero() && i.DeactivatedAt.Before(i.CreatedAt) {
		return fmt.Errorf("deactivated_at must not be before created_at")
	}
	return nil
}

func validateDesiredCollectorConfiguration(kind scope.CollectorKind, enabled bool, raw string) error {
	if kind == scope.CollectorTerraformState {
		return ValidateTerraformStateCollectorConfiguration(raw)
	}
	return validateDurableCollectorConfiguration(kind, enabled, raw)
}

func validateDurableCollectorConfiguration(kind scope.CollectorKind, enabled bool, raw string) error {
	if ok, err := validateComponentActivationConfiguration(raw); ok || err != nil {
		return err
	}
	if kind == scope.CollectorOCIRegistry {
		return ValidateOCIRegistryCollectorConfiguration(raw)
	}
	if kind == scope.CollectorPackageRegistry {
		return ValidatePackageRegistryCollectorConfiguration(raw)
	}
	if kind == scope.CollectorVulnerabilityIntelligence {
		return ValidateVulnerabilityIntelligenceCollectorConfiguration(raw)
	}
	if kind == scope.CollectorSBOMAttestation {
		return ValidateSBOMAttestationCollectorConfiguration(raw)
	}
	if kind == scope.CollectorSecurityAlert {
		return ValidateSecurityAlertCollectorConfiguration(raw)
	}
	if kind == scope.CollectorCICDRun {
		return ValidateCICDRunCollectorConfiguration(raw)
	}
	if kind == scope.CollectorVaultLive {
		if !enabled {
			return nil
		}
		return ValidateVaultLiveCollectorConfiguration(raw)
	}
	if !enabled {
		return nil
	}
	if kind == scope.CollectorPagerDuty {
		return ValidatePagerDutyCollectorConfiguration(raw)
	}
	if kind == scope.CollectorJira {
		return ValidateJiraCollectorConfiguration(raw)
	}
	return nil
}

func validateComponentActivationConfiguration(raw string) (bool, error) {
	var config componentActivationConfiguration
	if err := json.Unmarshal([]byte(normalizeJSONDocument(raw)), &config); err != nil {
		return false, nil
	}
	if strings.TrimSpace(config.SchemaVersion) == "" {
		return false, nil
	}
	if strings.TrimSpace(config.SchemaVersion) != componentInstanceConfigSchema {
		return false, nil
	}
	if strings.TrimSpace(config.ComponentID) == "" {
		return true, fmt.Errorf("component activation configuration component_id is required")
	}
	if strings.TrimSpace(config.ComponentVersion) == "" {
		return true, fmt.Errorf("component activation configuration component_version is required")
	}
	if strings.TrimSpace(config.ManifestDigest) == "" {
		return true, fmt.Errorf("component activation configuration manifest_digest is required")
	}
	if strings.TrimSpace(config.ConfigHandle) == "" {
		return true, fmt.Errorf("component activation configuration config_handle is required")
	}
	if strings.TrimSpace(config.Runtime.SDKProtocol) == "" {
		return true, fmt.Errorf("component activation configuration runtime.sdk_protocol is required")
	}
	if strings.TrimSpace(config.Runtime.SDKProtocol) != componentCollectorSDKProtocolV1Alpha1 {
		return true, fmt.Errorf(
			"component activation configuration runtime.sdk_protocol %q is unsupported",
			config.Runtime.SDKProtocol,
		)
	}
	if strings.TrimSpace(config.Runtime.Adapter) == "" {
		return true, fmt.Errorf("component activation configuration runtime.adapter is required")
	}
	switch strings.TrimSpace(config.Runtime.Adapter) {
	case componentRuntimeAdapterOCI, componentRuntimeAdapterProcess:
	default:
		return true, fmt.Errorf(
			"component activation configuration runtime.adapter %q is unsupported",
			config.Runtime.Adapter,
		)
	}
	return true, nil
}

// Materialize binds one desired collector instance to the supplied observation
// timestamp so it can be persisted durably.
func (d DesiredCollectorInstance) Materialize(observedAt time.Time) CollectorInstance {
	now := observedAt.UTC()
	return CollectorInstance{
		InstanceID:     strings.TrimSpace(d.InstanceID),
		CollectorKind:  d.CollectorKind,
		Mode:           d.Mode,
		Enabled:        d.Enabled,
		Bootstrap:      d.Bootstrap,
		ClaimsEnabled:  d.ClaimsEnabled,
		DisplayName:    strings.TrimSpace(d.DisplayName),
		Configuration:  normalizeJSONDocument(d.Configuration),
		LastObservedAt: now,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
}

func normalizeJSONDocument(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "{}"
	}
	return trimmed
}

func validateJSONDocument(field, raw string) error {
	normalized := normalizeJSONDocument(raw)
	if !json.Valid([]byte(normalized)) {
		return fmt.Errorf("%s must be valid JSON", field)
	}
	return nil
}
