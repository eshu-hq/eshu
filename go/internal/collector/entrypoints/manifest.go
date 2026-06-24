// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entrypoints

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// ManifestSet is the checked-in manifest file for generated collector entrypoints.
type ManifestSet struct {
	SchemaVersion int        `json:"schema_version" yaml:"schema_version"`
	Collectors    []Manifest `json:"collectors" yaml:"collectors"`
}

// LoadManifestFile reads and validates a collector entrypoint manifest file.
func LoadManifestFile(path string) ([]Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read collector entrypoint manifest: %w", err)
	}
	var set ManifestSet
	if err := yaml.Unmarshal(data, &set); err != nil {
		return nil, fmt.Errorf("decode collector entrypoint manifest: %w", err)
	}
	if set.SchemaVersion != 1 {
		return nil, fmt.Errorf("collector entrypoint manifest schema_version = %d, want 1", set.SchemaVersion)
	}
	if len(set.Collectors) == 0 {
		return nil, fmt.Errorf("collector entrypoint manifest must declare at least one collector")
	}
	seenCommandDirs := map[string]struct{}{}
	seenRuntimeNames := map[string]struct{}{}
	for i := range set.Collectors {
		if set.Collectors[i].SchemaVersion == 0 {
			set.Collectors[i].SchemaVersion = set.SchemaVersion
		}
		if err := validateManifest(set.Collectors[i]); err != nil {
			return nil, fmt.Errorf("collectors[%d]: %w", i, err)
		}
		if err := noteUnique(seenCommandDirs, set.Collectors[i].CommandDir, "command_dir"); err != nil {
			return nil, fmt.Errorf("collectors[%d]: %w", i, err)
		}
		if err := noteUnique(seenRuntimeNames, set.Collectors[i].RuntimeName, "runtime_name"); err != nil {
			return nil, fmt.Errorf("collectors[%d]: %w", i, err)
		}
	}
	return set.Collectors, nil
}

func noteUnique(seen map[string]struct{}, raw string, field string) error {
	value := strings.TrimSpace(raw)
	if _, ok := seen[value]; ok {
		return fmt.Errorf("duplicate %s %q", field, value)
	}
	seen[value] = struct{}{}
	return nil
}
