// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factschema

import "testing"

// The fixtures below mirror the exact row shapes
// go/internal/parser/hcl/parser.go's producers emit (parseTerraformBlocks'
// "module" case, parseTerragruntModuleSources, parseTerragruntDependencies,
// parseTerragruntConfig), cited field-by-field against that source rather
// than an imagined shape. go/internal/relationships' real-parser-driven
// tests (terraform_evidence_test.go, terragrunt_helper_evidence_test.go)
// are the authoritative equivalence proof that runs the actual HCL parser;
// these package-level tests exercise the decode function itself, including
// the malformed-input paths a real parser can never produce by
// construction.

// TestDecodeParsedFileDataTerraformModules_TypedRows proves the
// terraform_modules inner key decodes into typed []TerraformModule rows
// exposing Name and Source -- the two fields
// discoverStructuredTerraformEvidence reads -- while every module-block-only
// field (version, deployment_name, ...) survives in Attributes.
func TestDecodeParsedFileDataTerraformModules_TypedRows(t *testing.T) {
	t.Parallel()

	pfd := map[string]any{
		"terraform_modules": []any{
			map[string]any{
				"name":            "vpc",
				"line_number":     float64(3),
				"source":          "terraform-aws-modules/vpc/aws",
				"version":         "5.0.0",
				"deployment_name": "",
				"repo_name":       "",
				"create_deploy":   "",
				"cluster_name":    "",
				"zone_id":         "",
				"path":            "main.tf",
				"lang":            "hcl",
			},
		},
	}

	modules, err := DecodeParsedFileDataTerraformModules(pfd)
	if err != nil {
		t.Fatalf("DecodeParsedFileDataTerraformModules() error = %v, want nil", err)
	}
	if len(modules) != 1 {
		t.Fatalf("len(modules) = %d, want 1", len(modules))
	}
	module := modules[0]
	if module.Name != "vpc" || module.Source != "terraform-aws-modules/vpc/aws" {
		t.Fatalf("Name/Source = %q/%q", module.Name, module.Source)
	}
	if module.Attributes == nil {
		t.Fatal("Attributes = nil, want the non-read producer fields captured")
	}
	if got, ok := module.Attributes["version"].(string); !ok || got != "5.0.0" {
		t.Fatalf("Attributes[version] = %#v, want string \"5.0.0\"", module.Attributes["version"])
	}
	for _, named := range []string{"name", "source"} {
		if _, leaked := module.Attributes[named]; leaked {
			t.Fatalf("named field %q leaked into Attributes; it must be a typed field", named)
		}
	}
}

// TestDecodeParsedFileDataTerraformModules_Absent proves an absent
// terraform_modules key decodes to a nil slice with no error.
func TestDecodeParsedFileDataTerraformModules_Absent(t *testing.T) {
	t.Parallel()

	modules, err := DecodeParsedFileDataTerraformModules(map[string]any{"lang": "hcl"})
	if err != nil {
		t.Fatalf("DecodeParsedFileDataTerraformModules() error = %v, want nil", err)
	}
	if modules != nil {
		t.Fatalf("modules = %#v, want nil for an absent terraform_modules key", modules)
	}
}

// TestDecodeParsedFileDataTerraformModules_WrongTopLevelShape proves a
// present-but-not-any-recognized-slice-shape terraform_modules value
// surfaces a wrapped error rather than silently decoding to an empty slice.
func TestDecodeParsedFileDataTerraformModules_WrongTopLevelShape(t *testing.T) {
	t.Parallel()

	_, err := DecodeParsedFileDataTerraformModules(map[string]any{
		"terraform_modules": "not-a-slice",
	})
	if err == nil {
		t.Fatal("DecodeParsedFileDataTerraformModules() error = nil, want error for a non-slice terraform_modules value")
	}
}

// TestDecodeParsedFileDataTerraformModules_MalformedElementSkipped proves a
// non-object element inside an otherwise well-formed terraform_modules slice
// is SKIPPED, not an aborting error, so one malformed module row never drops
// every other well-formed module in the same .tf file -- the same
// per-element tolerance discoverStructuredTerraformEvidence's pre-typing
// raw-map read had (item, ok := raw.(map[string]any); if !ok { continue }).
func TestDecodeParsedFileDataTerraformModules_MalformedElementSkipped(t *testing.T) {
	t.Parallel()

	modules, err := DecodeParsedFileDataTerraformModules(map[string]any{
		"terraform_modules": []any{
			"not-an-object",
			map[string]any{"name": "vpc", "source": "terraform-aws-modules/vpc/aws"},
		},
	})
	if err != nil {
		t.Fatalf("DecodeParsedFileDataTerraformModules() error = %v, want nil (malformed element skipped)", err)
	}
	if len(modules) != 1 || modules[0].Name != "vpc" {
		t.Fatalf("modules = %#v, want one row for the well-formed element", modules)
	}
}

// TestDecodeParsedFileDataTerragruntDependencies_TypedRows proves the
// terragrunt_dependencies inner key decodes into typed
// []TerragruntDependency rows exposing Name and ConfigPath, matching
// go/internal/parser/hcl/parser.go's parseTerragruntDependencies row shape.
func TestDecodeParsedFileDataTerragruntDependencies_TypedRows(t *testing.T) {
	t.Parallel()

	pfd := map[string]any{
		"terragrunt_dependencies": []any{
			map[string]any{
				"name":        "vpc",
				"line_number": float64(1),
				"path":        "terragrunt.hcl",
				"lang":        "hcl",
				"config_path": "../vpc",
			},
		},
	}

	dependencies, err := DecodeParsedFileDataTerragruntDependencies(pfd)
	if err != nil {
		t.Fatalf("DecodeParsedFileDataTerragruntDependencies() error = %v, want nil", err)
	}
	if len(dependencies) != 1 {
		t.Fatalf("len(dependencies) = %d, want 1", len(dependencies))
	}
	dependency := dependencies[0]
	if dependency.Name != "vpc" || dependency.ConfigPath != "../vpc" {
		t.Fatalf("Name/ConfigPath = %q/%q", dependency.Name, dependency.ConfigPath)
	}
}

// TestDecodeParsedFileDataTerragruntDependencies_AbsentConfigPath proves a
// dependency block with no config_path attribute -- which
// parseTerragruntDependencies omits from the row entirely -- decodes to an
// empty ConfigPath with no error, matching the reducer's prior
// payloadString(dependency, "config_path") read of a missing key.
func TestDecodeParsedFileDataTerragruntDependencies_AbsentConfigPath(t *testing.T) {
	t.Parallel()

	dependencies, err := DecodeParsedFileDataTerragruntDependencies(map[string]any{
		"terragrunt_dependencies": []any{
			map[string]any{"name": "vpc", "line_number": float64(1), "path": "terragrunt.hcl", "lang": "hcl"},
		},
	})
	if err != nil {
		t.Fatalf("DecodeParsedFileDataTerragruntDependencies() error = %v, want nil", err)
	}
	if len(dependencies) != 1 || dependencies[0].ConfigPath != "" {
		t.Fatalf("dependencies = %#v, want one row with empty ConfigPath", dependencies)
	}
}

// TestDecodeParsedFileDataTerragruntConfigs_TypedRows proves the
// terragrunt_configs inner key decodes into typed []TerragruntConfig rows
// exposing the four comma-joined helper-path fields
// discoverStructuredTerragruntConfigEvidence reads, matching
// go/internal/parser/hcl/parser.go's parseTerragruntConfig row shape.
func TestDecodeParsedFileDataTerragruntConfigs_TypedRows(t *testing.T) {
	t.Parallel()

	pfd := map[string]any{
		"terragrunt_configs": []any{
			map[string]any{
				"name":                         "terragrunt",
				"line_number":                  float64(1),
				"path":                         "terragrunt.hcl",
				"lang":                         "hcl",
				"terraform_source":             "",
				"includes":                     "",
				"locals":                       "",
				"inputs":                       "",
				"include_paths":                "../../_envcommon/root.hcl",
				"read_config_paths":            "../../_envcommon/vpc.hcl",
				"find_in_parent_folders_paths": "../../terragrunt.hcl",
				"local_config_asset_paths":     "./templates/values.yaml.tpl",
			},
		},
	}

	configs, err := DecodeParsedFileDataTerragruntConfigs(pfd)
	if err != nil {
		t.Fatalf("DecodeParsedFileDataTerragruntConfigs() error = %v, want nil", err)
	}
	if len(configs) != 1 {
		t.Fatalf("len(configs) = %d, want 1", len(configs))
	}
	config := configs[0]
	if config.IncludePaths != "../../_envcommon/root.hcl" {
		t.Fatalf("IncludePaths = %q", config.IncludePaths)
	}
	if config.ReadConfigPaths != "../../_envcommon/vpc.hcl" {
		t.Fatalf("ReadConfigPaths = %q", config.ReadConfigPaths)
	}
	if config.FindInParentFoldersPaths != "../../terragrunt.hcl" {
		t.Fatalf("FindInParentFoldersPaths = %q", config.FindInParentFoldersPaths)
	}
	if config.LocalConfigAssetPaths != "./templates/values.yaml.tpl" {
		t.Fatalf("LocalConfigAssetPaths = %q", config.LocalConfigAssetPaths)
	}
}

// TestDecodeParsedFileDataTerragruntConfigs_Absent proves an absent
// terragrunt_configs key decodes to a nil slice with no error.
func TestDecodeParsedFileDataTerragruntConfigs_Absent(t *testing.T) {
	t.Parallel()

	configs, err := DecodeParsedFileDataTerragruntConfigs(map[string]any{"lang": "hcl"})
	if err != nil {
		t.Fatalf("DecodeParsedFileDataTerragruntConfigs() error = %v, want nil", err)
	}
	if configs != nil {
		t.Fatalf("configs = %#v, want nil for an absent terragrunt_configs key", configs)
	}
}
