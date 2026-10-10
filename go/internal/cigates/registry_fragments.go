// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type gateFragmentFile struct {
	Version string     `yaml:"version"`
	Gates   []gateFile `yaml:"gates"`
}

// FragmentPaths returns the validated fragment paths in registry order.
// A flat registry returns nil. The returned slice does not alias Registry.
func (r *Registry) FragmentPaths() []string {
	return append([]string(nil), r.gateFragments...)
}

// decodeRegistryFile validates the root YAML and expands its ordered fragments.
func decodeRegistryFile(path string) (registryFile, error) {
	raw, err := os.ReadFile(path) // #nosec G304 -- path is the operator-configured gate registry under specs/.
	if err != nil {
		return registryFile{}, fmt.Errorf("read ci-gates registry %s: %w", path, err)
	}
	var parsed registryFile
	decoder := yaml.NewDecoder(strings.NewReader(string(raw)))
	decoder.KnownFields(true)
	if err := decoder.Decode(&parsed); err != nil {
		return registryFile{}, fmt.Errorf("parse ci-gates registry %s: %w", path, err)
	}
	var extra interface{}
	if err := decoder.Decode(&extra); err != io.EOF {
		return registryFile{}, fmt.Errorf("ci-gates registry %s: extra document or invalid YAML: %v", path, err)
	}
	if parsed.Version != "v1" {
		return registryFile{}, fmt.Errorf("ci-gates registry %s: unsupported version %q", path, parsed.Version)
	}
	if parsed.Gates == nil && parsed.GateFragments == nil {
		return registryFile{}, fmt.Errorf("ci-gates registry %s: missing gates or gate_fragments", path)
	}
	if err := loadGateFragments(path, &parsed); err != nil {
		return registryFile{}, err
	}
	return parsed, nil
}

// loadGateFragments expands an ordered, root-relative gate list before Load
// validates the complete registry. Flat custom registries remain supported.
func loadGateFragments(path string, parsed *registryFile) error {
	if len(parsed.GateFragments) == 0 {
		if parsed.GateFragments != nil {
			return fmt.Errorf("ci-gates registry %s: empty gate_fragments", path)
		}
		return nil
	}
	if parsed.Gates != nil {
		return fmt.Errorf("ci-gates registry %s: gates and gate_fragments cannot be mixed", path)
	}
	root, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil {
		return fmt.Errorf("ci-gates registry %s: resolve root: %w", path, err)
	}
	allowed := filepath.Join(root, "ci-gates.d")
	seen := make(map[string]bool, len(parsed.GateFragments))
	for _, ref := range parsed.GateFragments {
		clean := filepath.Clean(ref)
		if ref == "" || strings.ContainsAny(ref, "\r\n") || filepath.IsAbs(ref) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || clean != ref || !strings.HasPrefix(clean, "ci-gates.d"+string(filepath.Separator)) {
			return fmt.Errorf("ci-gates registry %s: invalid gate fragment %q", path, ref)
		}
		if seen[clean] {
			return fmt.Errorf("ci-gates registry %s: duplicate gate fragment %q", path, ref)
		}
		seen[clean] = true
		resolved, err := filepath.EvalSymlinks(filepath.Join(root, clean))
		if err != nil {
			return fmt.Errorf("ci-gates registry %s: resolve fragment %q: %w", path, ref, err)
		}
		rel, err := filepath.Rel(allowed, resolved)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == "." {
			return fmt.Errorf("ci-gates registry %s: fragment %q escapes ci-gates.d", path, ref)
		}
		raw, err := os.ReadFile(resolved) // #nosec G304 -- resolved path is confined to the registry fragment subtree.
		if err != nil {
			return fmt.Errorf("ci-gates registry %s: read fragment %q: %w", path, ref, err)
		}
		var fragment gateFragmentFile
		decoder := yaml.NewDecoder(strings.NewReader(string(raw)))
		decoder.KnownFields(true)
		if err := decoder.Decode(&fragment); err != nil {
			return fmt.Errorf("ci-gates registry %s: parse fragment %q: %w", path, ref, err)
		}
		var extra interface{}
		if err := decoder.Decode(&extra); err != io.EOF {
			return fmt.Errorf("ci-gates registry %s: fragment %q has extra document or invalid YAML: %v", path, ref, err)
		}
		if fragment.Version != parsed.Version || fragment.Version != "v1" || len(fragment.Gates) == 0 {
			return fmt.Errorf("ci-gates registry %s: fragment %q has invalid version or empty gates", path, ref)
		}
		parsed.Gates = append(parsed.Gates, fragment.Gates...)
	}
	return nil
}
