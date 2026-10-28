// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package component

import (
	"encoding/json"
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

type activationRuntimeFile struct {
	Host ActivationHostClaimMetadata `yaml:"host"`
	// Config is kept as a raw node so a file whose config block the caller
	// never needs decodes exactly as it did before the block was read.
	Config yaml.Node `yaml:"config"`
}

// ActivationRuntimeFile is what the coordinator reads from one activation
// config file in a single read: the optional host claim metadata and the
// activation's config block. The worker reads the same file and hands that
// config block to the component verbatim; this type never rewrites it.
type ActivationRuntimeFile struct {
	// Host is the normalized host claim metadata. It is valid when HasHost is
	// true.
	Host ActivationHostClaimMetadata
	// HasHost reports whether the file declared host claim metadata.
	HasHost bool
	config  yaml.Node
}

// LoadActivationRuntimeFile reads one activation config file once and returns
// its host claim metadata and its config block, without exposing the config
// path in returned errors. Other adapter-specific sections are ignored.
func LoadActivationRuntimeFile(path string) (ActivationRuntimeFile, error) {
	if strings.TrimSpace(path) == "" {
		return ActivationRuntimeFile{}, nil
	}
	raw, err := os.ReadFile(strings.TrimSpace(path))
	if err != nil {
		return ActivationRuntimeFile{}, fmt.Errorf(
			"read activation config host metadata: %w",
			sanitizedFileReadError(err),
		)
	}
	var config activationRuntimeFile
	if err := yaml.Unmarshal(raw, &config); err != nil {
		return ActivationRuntimeFile{}, fmt.Errorf("decode activation config host metadata: %w", err)
	}
	file := ActivationRuntimeFile{config: config.Config}
	host := config.Host.Normalized()
	if host.Empty() {
		return file, nil
	}
	if err := host.Validate(); err != nil {
		return ActivationRuntimeFile{}, err
	}
	file.Host = host
	file.HasHost = true
	return file, nil
}

// ConfigJSON returns the activation's config block re-encoded as one JSON
// object, decoded the way the extension worker decodes it (a YAML mapping into
// map[string]any). It returns nil when the file has no config block or the
// block is null, and an error when the block is not a mapping or cannot be
// encoded as JSON. The coordinator carries the result as an opaque value; this
// package does not interpret any key inside it.
func (f ActivationRuntimeFile) ConfigJSON() (json.RawMessage, error) {
	node := f.config
	if node.Kind == 0 || (node.Kind == yaml.ScalarNode && node.Tag == "!!null") {
		return nil, nil
	}
	if node.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("activation config config block must be a mapping")
	}
	var block map[string]any
	if err := node.Decode(&block); err != nil {
		return nil, fmt.Errorf("decode activation config config block: %w", err)
	}
	encoded, err := json.Marshal(block)
	if err != nil {
		return nil, fmt.Errorf("encode activation config config block: %w", err)
	}
	return encoded, nil
}

// LoadActivationHostClaimMetadata reads the optional host claim metadata from
// an activation config file without exposing the config path in returned
// errors. The file may contain other adapter-specific sections; this reader
// ignores them.
func LoadActivationHostClaimMetadata(path string) (ActivationHostClaimMetadata, bool, error) {
	file, err := LoadActivationRuntimeFile(path)
	if err != nil {
		return ActivationHostClaimMetadata{}, false, err
	}
	return file.Host, file.HasHost, nil
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
