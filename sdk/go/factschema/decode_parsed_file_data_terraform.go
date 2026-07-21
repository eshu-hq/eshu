// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factschema

import (
	"fmt"

	codegraphv1 "github.com/eshu-hq/eshu/sdk/go/factschema/codegraph/v1"
)

// This file holds the typed accessors for three more closed-shape
// parsed_file_data inner keys (issue #5445 slice 1): terraform_modules,
// terragrunt_dependencies, and terragrunt_configs. Each uses
// decodeParsedFileDataTolerantSlice (decode_parsed_file_data_tolerant.go),
// NOT the abort-on-first-malformed-element asObjectSlice the #4750 S1 batch
// uses -- see that helper's doc comment for why: these accessors migrate
// real go/internal/relationships call sites whose pre-typing raw-map read
// tolerated one malformed row in an otherwise-good multi-row bucket, and
// this batch preserves that tolerance exactly rather than regressing it.

// DecodeParsedFileDataTerraformModules decodes the "terraform_modules" inner
// slice of a parsed_file_data map into a typed []codegraphv1.TerraformModule.
// An absent key decodes to a nil slice with no error, and a malformed
// element is skipped rather than failing the whole decode, matching
// discoverStructuredTerraformEvidence's prior raw-map tolerant read
// (go/internal/relationships/terraform_evidence.go). An error is returned
// only when the key is present but not any recognized slice shape at all.
func DecodeParsedFileDataTerraformModules(parsedFileData map[string]any) ([]codegraphv1.TerraformModule, error) {
	raw, present := parsedFileData["terraform_modules"]
	if !present || raw == nil {
		return nil, nil
	}
	modules, ok := decodeParsedFileDataTolerantSlice[codegraphv1.TerraformModule](raw)
	if !ok {
		return nil, fmt.Errorf("factschema: terraform_modules: want slice of JSON objects, got %T", raw)
	}
	return modules, nil
}

// DecodeParsedFileDataTerragruntDependencies decodes the
// "terragrunt_dependencies" inner slice of a parsed_file_data map into a
// typed []codegraphv1.TerragruntDependency. An absent key decodes to a nil
// slice with no error, and a malformed element is skipped rather than
// failing the whole decode, matching discoverStructuredTerraformEvidence's
// prior raw-map tolerant read (go/internal/relationships/terraform_evidence.go).
func DecodeParsedFileDataTerragruntDependencies(parsedFileData map[string]any) ([]codegraphv1.TerragruntDependency, error) {
	raw, present := parsedFileData["terragrunt_dependencies"]
	if !present || raw == nil {
		return nil, nil
	}
	dependencies, ok := decodeParsedFileDataTolerantSlice[codegraphv1.TerragruntDependency](raw)
	if !ok {
		return nil, fmt.Errorf("factschema: terragrunt_dependencies: want slice of JSON objects, got %T", raw)
	}
	return dependencies, nil
}

// DecodeParsedFileDataTerragruntConfigs decodes the "terragrunt_configs"
// inner slice of a parsed_file_data map into a typed
// []codegraphv1.TerragruntConfig. An absent key decodes to a nil slice with
// no error, and a malformed element is skipped rather than failing the whole
// decode, matching discoverStructuredTerragruntConfigEvidence's prior
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
	configs, ok := decodeParsedFileDataTolerantSlice[codegraphv1.TerragruntConfig](raw)
	if !ok {
		return nil, fmt.Errorf("factschema: terragrunt_configs: want slice of JSON objects, got %T", raw)
	}
	return configs, nil
}
