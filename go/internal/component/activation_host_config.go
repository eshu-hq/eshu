// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package component

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// ActivationHostClaimMetadata is the safe, non-secret claim identity a hosted
// activation may ask the coordinator and worker to use for SDK claims.
type ActivationHostClaimMetadata struct {
	SourceSystem string                   `json:"source_system,omitempty" yaml:"sourceSystem"`
	Scope        ActivationHostClaimScope `json:"scope,omitempty" yaml:"scope"`
}

// ActivationHostClaimScope identifies the public source scope for one hosted
// activation claim.
type ActivationHostClaimScope struct {
	ID   string `json:"id,omitempty" yaml:"id"`
	Kind string `json:"kind,omitempty" yaml:"kind"`
}

type activationHostConfigFile struct {
	Host ActivationHostClaimMetadata `json:"host" yaml:"host"`
}

// LoadActivationHostClaimMetadata reads the optional host claim metadata from
// an activation config file without exposing the config path in returned
// errors. The file may contain other adapter-specific sections; this reader
// ignores them.
func LoadActivationHostClaimMetadata(path string) (ActivationHostClaimMetadata, bool, error) {
	if strings.TrimSpace(path) == "" {
		return ActivationHostClaimMetadata{}, false, nil
	}
	raw, err := os.ReadFile(strings.TrimSpace(path))
	if err != nil {
		return ActivationHostClaimMetadata{}, false, fmt.Errorf(
			"read activation config host metadata: %w",
			sanitizedFileReadError(err),
		)
	}
	var config activationHostConfigFile
	if err := yaml.Unmarshal(raw, &config); err != nil {
		return ActivationHostClaimMetadata{}, false, fmt.Errorf("decode activation config host metadata: %w", err)
	}
	host := config.Host.Normalized()
	if host.Empty() {
		return ActivationHostClaimMetadata{}, false, nil
	}
	if err := host.Validate(); err != nil {
		return ActivationHostClaimMetadata{}, false, err
	}
	return host, true, nil
}

// Normalized returns a copy with whitespace trimmed from all string fields.
func (m ActivationHostClaimMetadata) Normalized() ActivationHostClaimMetadata {
	return ActivationHostClaimMetadata{
		SourceSystem: strings.TrimSpace(m.SourceSystem),
		Scope: ActivationHostClaimScope{
			ID:   strings.TrimSpace(m.Scope.ID),
			Kind: strings.TrimSpace(m.Scope.Kind),
		},
	}
}

// Empty reports whether the activation config omitted host claim metadata.
func (m ActivationHostClaimMetadata) Empty() bool {
	normalized := m.Normalized()
	return normalized.SourceSystem == "" &&
		normalized.Scope.ID == "" &&
		normalized.Scope.Kind == ""
}

// Validate checks that host claim metadata is complete when present.
func (m ActivationHostClaimMetadata) Validate() error {
	normalized := m.Normalized()
	if normalized.SourceSystem == "" {
		return fmt.Errorf("activation config host.sourceSystem is required when host metadata is present")
	}
	if normalized.Scope.ID == "" {
		return fmt.Errorf("activation config host.scope.id is required when host metadata is present")
	}
	if normalized.Scope.Kind == "" {
		return fmt.Errorf("activation config host.scope.kind is required when host metadata is present")
	}
	return nil
}

func sanitizedFileReadError(err error) error {
	switch {
	case os.IsNotExist(err):
		return errors.New("file does not exist")
	case os.IsPermission(err):
		return errors.New("permission denied")
	default:
		return errors.New("read failed")
	}
}
