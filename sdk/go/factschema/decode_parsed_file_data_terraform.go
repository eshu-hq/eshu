// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factschema

import (
	"fmt"

	codegraphv1 "github.com/eshu-hq/eshu/sdk/go/factschema/codegraph/v1"
)

// This file holds the typed accessors for three more closed-shape
// parsed_file_data inner keys (issue #5445 slice 1): terraform_modules,
// terragrunt_dependencies, and terragrunt_configs. It follows the exact
// pattern decode_parsed_file_data.go established for the #4750 S1 batch --
// see that file's package-level doc comment for the tolerant-decode
// contract (absent key -> empty/false with no error, present-but-wrong-shape
// -> a wrapped error, malformed element -> a wrapped error).

// DecodeParsedFileDataTerraformModules decodes the "terraform_modules" inner
// slice of a parsed_file_data map into a typed []codegraphv1.TerraformModule.
// An absent key decodes to a nil slice with no error, matching
// discoverStructuredTerraformEvidence's prior raw-map tolerant read
// (go/internal/relationships/terraform_evidence.go).
func DecodeParsedFileDataTerraformModules(parsedFileData map[string]any) ([]codegraphv1.TerraformModule, error) {
	raw, present := parsedFileData["terraform_modules"]
	if !present || raw == nil {
		return nil, nil
	}
	elems, ok := asObjectSlice(raw)
	if !ok {
		return nil, fmt.Errorf("factschema: terraform_modules: want slice of JSON objects, got %T", raw)
	}
	modules := make([]codegraphv1.TerraformModule, 0, len(elems))
	for i, elem := range elems {
		var module codegraphv1.TerraformModule
		if err := decodeMapInto(elem, &module); err != nil {
			return nil, fmt.Errorf("factschema: terraform_modules[%d]: %w", i, err)
		}
		modules = append(modules, module)
	}
	return modules, nil
}

// DecodeParsedFileDataTerragruntDependencies decodes the
// "terragrunt_dependencies" inner slice of a parsed_file_data map into a
// typed []codegraphv1.TerragruntDependency. An absent key decodes to a nil
// slice with no error, matching discoverStructuredTerraformEvidence's prior
// raw-map tolerant read (go/internal/relationships/terraform_evidence.go).
func DecodeParsedFileDataTerragruntDependencies(parsedFileData map[string]any) ([]codegraphv1.TerragruntDependency, error) {
	raw, present := parsedFileData["terragrunt_dependencies"]
	if !present || raw == nil {
		return nil, nil
	}
	elems, ok := asObjectSlice(raw)
	if !ok {
		return nil, fmt.Errorf("factschema: terragrunt_dependencies: want slice of JSON objects, got %T", raw)
	}
	dependencies := make([]codegraphv1.TerragruntDependency, 0, len(elems))
	for i, elem := range elems {
		var dependency codegraphv1.TerragruntDependency
		if err := decodeMapInto(elem, &dependency); err != nil {
			return nil, fmt.Errorf("factschema: terragrunt_dependencies[%d]: %w", i, err)
		}
		dependencies = append(dependencies, dependency)
	}
	return dependencies, nil
}

// DecodeParsedFileDataTerragruntConfigs decodes the "terragrunt_configs"
// inner slice of a parsed_file_data map into a typed
// []codegraphv1.TerragruntConfig. An absent key decodes to a nil slice with
// no error, matching discoverStructuredTerragruntConfigEvidence's prior
// raw-map tolerant read
// (go/internal/relationships/terragrunt_helper_evidence.go). The producer
// (parseTerragruntConfig) emits at most one row per terragrunt.hcl file, but
// the bucket stays a slice on the wire like every other parsed_file_data
// inner key.
func DecodeParsedFileDataTerragruntConfigs(parsedFileData map[string]any) ([]codegraphv1.TerragruntConfig, error) {
	raw, present := parsedFileData["terragrunt_configs"]
	if !present || raw == nil {
		return nil, nil
	}
	elems, ok := asObjectSlice(raw)
	if !ok {
		return nil, fmt.Errorf("factschema: terragrunt_configs: want slice of JSON objects, got %T", raw)
	}
	configs := make([]codegraphv1.TerragruntConfig, 0, len(elems))
	for i, elem := range elems {
		var config codegraphv1.TerragruntConfig
		if err := decodeMapInto(elem, &config); err != nil {
			return nil, fmt.Errorf("factschema: terragrunt_configs[%d]: %w", i, err)
		}
		configs = append(configs, config)
	}
	return configs, nil
}
