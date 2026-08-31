// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package componentactivation

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/component"
)

// ConfigSchema is the schema_version every generic component activation
// configuration must carry. Root's component registry readback
// (component_activation_config.go's componentActivationRuntimeConfig)
// stamps this value when it builds a collector instance's Configuration
// JSON; ParseConfig rejects any other schema_version.
const ConfigSchema = "eshu.component.instance.v1"

// Config is one component instance's parsed generic activation
// configuration. Root's component registry readback constructs one to
// build a collector instance's Configuration JSON
// (component_activation_config.go); the componentextensionplanner child
// parses it back at planning time; and pagerduty_service.go and
// governance_audit.go read it to exclude component-extension instances from
// unrelated scheduling and to identify the component in a denied-egress
// audit event. This package is dependency-neutral — it imports only
// internal/component, never internal/coordinator or any coordinator child
// package — so every one of those consumers can import it without an
// import cycle.
type Config struct {
	SchemaVersion    string                                 `json:"schema_version"`
	ComponentID      string                                 `json:"component_id"`
	ComponentVersion string                                 `json:"component_version"`
	Publisher        string                                 `json:"publisher"`
	ManifestDigest   string                                 `json:"manifest_digest"`
	ConfigHandle     string                                 `json:"config_handle"`
	Host             *component.ActivationHostClaimMetadata `json:"host,omitempty"`
	Runtime          RuntimeConfig                          `json:"runtime"`
}

// RuntimeConfig is the collector-SDK runtime binding a component activation
// declares: the SDK protocol version and the adapter (oci or process) the
// coordinator's component runtime uses to host it.
type RuntimeConfig struct {
	SDKProtocol string `json:"sdk_protocol"`
	Adapter     string `json:"adapter"`
}

// ParseConfig parses and validates raw collector-instance configuration as a
// generic component-extension activation configuration. ok reports whether
// the configuration is a component-extension activation configuration at
// all (a blank or unrelated configuration returns ok=false, err=nil); a
// malformed or incomplete one that IS a component-extension configuration
// returns ok=false with a descriptive err.
func ParseConfig(raw string) (Config, bool, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return Config{}, false, nil
	}
	var config Config
	if err := json.Unmarshal([]byte(trimmed), &config); err != nil {
		return Config{}, false, fmt.Errorf("decode component activation configuration: %w", err)
	}
	if strings.TrimSpace(config.SchemaVersion) == "" && strings.TrimSpace(config.ComponentID) == "" {
		return Config{}, false, nil
	}
	if strings.TrimSpace(config.ComponentID) == "" &&
		strings.TrimSpace(config.SchemaVersion) != ConfigSchema {
		return Config{}, false, nil
	}
	if strings.TrimSpace(config.SchemaVersion) != ConfigSchema {
		return Config{}, false, fmt.Errorf(
			"component activation configuration schema_version must be %q",
			ConfigSchema,
		)
	}
	if strings.TrimSpace(config.ComponentID) == "" {
		return Config{}, false, fmt.Errorf("component activation configuration component_id is required")
	}
	if strings.TrimSpace(config.ComponentVersion) == "" {
		return Config{}, false, fmt.Errorf("component activation configuration component_version is required")
	}
	if strings.TrimSpace(config.ManifestDigest) == "" {
		return Config{}, false, fmt.Errorf("component activation configuration manifest_digest is required")
	}
	if strings.TrimSpace(config.ConfigHandle) == "" {
		return Config{}, false, fmt.Errorf("component activation configuration config_handle is required")
	}
	if config.Host != nil {
		host := config.Host.Normalized()
		if host.Empty() {
			config.Host = nil
		} else {
			if err := host.Validate(); err != nil {
				return Config{}, false, err
			}
			config.Host = &host
		}
	}
	if strings.TrimSpace(config.Runtime.SDKProtocol) == "" {
		return Config{}, false, fmt.Errorf("component activation configuration runtime.sdk_protocol is required")
	}
	if strings.TrimSpace(config.Runtime.SDKProtocol) != component.CollectorSDKProtocolV1Alpha1 {
		return Config{}, false, fmt.Errorf(
			"component activation configuration runtime.sdk_protocol %q is unsupported",
			config.Runtime.SDKProtocol,
		)
	}
	if strings.TrimSpace(config.Runtime.Adapter) == "" {
		return Config{}, false, fmt.Errorf("component activation configuration runtime.adapter is required")
	}
	switch strings.TrimSpace(config.Runtime.Adapter) {
	case component.RuntimeAdapterOCI, component.RuntimeAdapterProcess:
	default:
		return Config{}, false, fmt.Errorf(
			"component activation configuration runtime.adapter %q is unsupported",
			config.Runtime.Adapter,
		)
	}
	return config, true, nil
}
