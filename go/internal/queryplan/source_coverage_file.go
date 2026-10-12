// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package queryplan

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type sourceCoverageFragment struct {
	Version        int              `yaml:"version"`
	SourceCoverage []SourceCoverage `yaml:"source_coverage"`
}

// loadSourceCoverageFiles resolves only explicit, ordered, local coverage shards.
// Flat manifests continue through the existing decoder unchanged.
func loadSourceCoverageFiles(path string, data []byte, manifest *Manifest) error {
	var document yaml.Node
	if err := yaml.Unmarshal(data, &document); err != nil {
		return err
	}
	if err := rejectDuplicateYAMLKeys(&document); err != nil {
		return fmt.Errorf("manifest: %w", err)
	}
	if len(document.Content) != 1 || document.Content[0].Kind != yaml.MappingNode {
		return fmt.Errorf("manifest must be a YAML mapping")
	}
	root := document.Content[0]
	if !hasYAMLKey(root, "source_coverage_files") {
		if manifest.GrandfatheredNonHotBaseline != "" && len(manifest.SourceCoverage) == 0 {
			return fmt.Errorf("frozen source coverage requires inline rows or source_coverage_files")
		}
		return nil
	}
	if hasYAMLKey(root, "source_coverage") {
		return fmt.Errorf("source_coverage and source_coverage_files cannot both be set")
	}
	if err := decodeStrictYAML(data, manifest); err != nil {
		return fmt.Errorf("manifest: %w", err)
	}
	if len(manifest.SourceCoverageFiles) == 0 {
		return fmt.Errorf("source_coverage_files must contain at least one file")
	}
	base, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil {
		return fmt.Errorf("resolve manifest directory: %w", err)
	}
	seenFiles := make(map[string]struct{}, len(manifest.SourceCoverageFiles))
	seenCalls := make(map[string]struct{})
	for _, ref := range manifest.SourceCoverageFiles {
		if !validCoverageReference(ref) {
			return fmt.Errorf("invalid source_coverage_files reference %q", ref)
		}
		if _, duplicate := seenFiles[ref]; duplicate {
			return fmt.Errorf("duplicate source_coverage_files reference %q", ref)
		}
		seenFiles[ref] = struct{}{}
		resolved, err := filepath.EvalSymlinks(filepath.Join(base, ref))
		if err != nil {
			return fmt.Errorf("resolve source coverage file %q: %w", ref, err)
		}
		rel, err := filepath.Rel(base, resolved)
		if err != nil || !strings.HasPrefix(filepath.ToSlash(rel), "source-coverage/") {
			return fmt.Errorf("source coverage file %q escapes source-coverage directory", ref)
		}
		fragmentData, err := os.ReadFile(resolved) // #nosec G304 -- explicit manifest-relative path, canonicalized and confined above
		if err != nil {
			return fmt.Errorf("read source coverage file %q: %w", ref, err)
		}
		var fragment sourceCoverageFragment
		if err := decodeStrictYAML(fragmentData, &fragment); err != nil {
			return fmt.Errorf("source coverage file %q: %w", ref, err)
		}
		if fragment.Version != 1 || len(fragment.SourceCoverage) == 0 {
			return fmt.Errorf("source coverage file %q requires version 1 and nonempty source_coverage", ref)
		}
		for _, source := range fragment.SourceCoverage {
			if strings.TrimSpace(source.File) == "" || len(source.Calls) == 0 {
				return fmt.Errorf("source coverage file %q has empty file or calls", ref)
			}
			for _, call := range source.Calls {
				if strings.TrimSpace(call.Symbol) == "" || call.Count <= 0 {
					return fmt.Errorf("source coverage file %q has invalid call identity or count", ref)
				}
				key := source.File + ":" + call.Symbol
				if _, duplicate := seenCalls[key]; duplicate {
					return fmt.Errorf("duplicate source coverage call %s", key)
				}
				seenCalls[key] = struct{}{}
			}
		}
		manifest.SourceCoverage = append(manifest.SourceCoverage, fragment.SourceCoverage...)
	}
	return nil
}

func validCoverageReference(ref string) bool {
	if ref == "" || filepath.IsAbs(ref) || strings.ContainsAny(ref, "\\:") ||
		filepath.Clean(ref) != ref || filepath.Ext(ref) != ".yaml" ||
		!strings.HasPrefix(ref, "source-coverage/") {
		return false
	}
	for _, part := range strings.Split(ref, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}

func hasYAMLKey(node *yaml.Node, key string) bool {
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return true
		}
	}
	return false
}

func rejectDuplicateYAMLKeys(node *yaml.Node) error {
	if node.Kind == yaml.MappingNode {
		seen := make(map[string]struct{}, len(node.Content)/2)
		for i := 0; i+1 < len(node.Content); i += 2 {
			key := node.Content[i].Value
			if _, duplicate := seen[key]; duplicate {
				return fmt.Errorf("duplicate YAML key %q", key)
			}
			seen[key] = struct{}{}
		}
	}
	for _, child := range node.Content {
		if err := rejectDuplicateYAMLKeys(child); err != nil {
			return err
		}
	}
	return nil
}

func decodeStrictYAML(data []byte, value any) error {
	var document yaml.Node
	if err := yaml.Unmarshal(data, &document); err != nil {
		return err
	}
	if err := rejectDuplicateYAMLKeys(&document); err != nil {
		return err
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(value); err != nil {
		return err
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("multiple YAML documents are not allowed")
		}
		return err
	}
	return nil
}
