// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package activation

import (
	"bytes"
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
// (component_activation_config.go); component_extension_service.go checks it;
// the extension child parses it at planning time; and pagerduty_service.go and
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
	// ComponentConfig is the activation's config block as one opaque JSON
	// object, or nil. Root's registry readback fills it only for collector
	// kinds whose coordinator plans host-derived scopes (today
	// vulnerability_intelligence), from the same activation file it reads
	// host from. This package checks only that it is a JSON object; it never
	// reads a key inside it. A kind-scoped planner in the coordinator decodes
	// it; every other consumer ignores it, and the extension planner keeps
	// it out of RequestedScopeSet. The block is non-secret by contract:
	// credentials go in the worker's environment, not the config block.
	ComponentConfig json.RawMessage `json:"component_config,omitempty"`
}

// RuntimeConfig is the collector-SDK runtime binding a component activation
// declares: the SDK protocol version and the adapter (oci or process) the
// coordinator's component runtime uses to host it.
type RuntimeConfig struct {
	SDKProtocol string `json:"sdk_protocol"`
	Adapter     string `json:"adapter"`
}

// IsConfiguration reports whether raw collector-instance configuration is a
// generic component-extension activation document, so the extension host, not
// an in-tree planner, owns the instance. A malformed or incomplete activation
// document counts, because the generic planner reports that error and no
// in-tree planner may claim the instance as well. Blank and in-tree target
// configuration return false. The coordinator scheduler, its startup claim
// validation, and the status reader share this one check so they always agree
// on which instances are extension-hosted.
func IsConfiguration(raw string) bool {
	_, ok, err := ParseConfig(raw)
	return ok || err != nil
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
	componentConfig, err := normalizeComponentConfig(config.ComponentConfig)
	if err != nil {
		return Config{}, false, err
	}
	config.ComponentConfig = componentConfig
	if strings.TrimSpace(config.Runtime.SDKProtocol) == "" {
		return Config{}, false, fmt.Errorf("component activation configuration runtime.sdk_protocol is required")
	}
	switch strings.TrimSpace(config.Runtime.SDKProtocol) {
	case component.CollectorSDKProtocolV1Alpha1, component.CollectorSDKProtocolV1Alpha2:
	default:
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

// normalizeComponentConfig accepts an absent or null component_config and any
// JSON object, and rejects every other JSON value. It does not look inside the
// object: the keys belong to the component's kind-scoped planner.
func normalizeComponentConfig(raw json.RawMessage) (json.RawMessage, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, nil
	}
	if trimmed[0] != '{' {
		return nil, fmt.Errorf("component activation configuration component_config must be a JSON object")
	}
	return trimmed, nil
}
