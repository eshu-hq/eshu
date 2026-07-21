// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

// This file types three more closed-shape parsed_file_data inner keys
// (Contract System v1 §7 incremental migration, issue #5445 slice 1),
// following the exact pattern parsed_file_data.go established for the #4750
// S1 batch: name what a consumer reads, pass every other producer field
// through untyped via Attributes, and leave File.ParsedFileData itself an
// open map[string]any so the wire schema stays byte-identical.
//
// terraform_modules and terragrunt_dependencies have TWO producers each on
// different code paths of go/internal/parser/hcl/parser.go: the "module"
// HCL block handler (parseTerraformBlocks, non-terragrunt .tf files) and the
// terragrunt-only helpers (parseTerragruntModuleSources,
// parseTerragruntDependencies, terragrunt.hcl files only). The two producers
// for terraform_modules write different field sets (the module-block row
// additionally carries version/deployment_name/repo_name/create_deploy/
// cluster_name/zone_id/deploy_entry_point); only the fields both producers
// share, and go/internal/relationships actually reads (Name, Source), are
// named here. terragrunt_configs has a single producer
// (parseTerragruntConfig, terragrunt.hcl only).

// TerraformModule is the typed view of one entry in a parsed_file_data
// "terraform_modules" inner slice: a Terraform `module` block
// (go/internal/parser/hcl/parser.go parseTerraformBlocks) or a Terragrunt
// `terraform { source = ... }` block (parseTerragruntModuleSources). Only
// Name and Source are named -- the two fields
// discoverStructuredTerraformEvidence (go/internal/relationships/
// terraform_evidence.go) reads to resolve a module source reference to a
// target repository. Every other module-block-only field (version,
// deployment_name, repo_name, create_deploy, cluster_name, zone_id,
// deploy_entry_point) survives in Attributes so the accessor drops no
// evidence for a future consumer.
type TerraformModule struct {
	// Name is the module block's label (the HCL `module "name" {}` label) or,
	// for a Terragrunt terraform-source row, the terragrunt.hcl file's base
	// name (parseTerragruntModuleSources synthesizes this so the row has a
	// stable identity even though Terragrunt's terraform block has no label).
	Name string `json:"name,omitempty"`
	// Source is the module's declared source expression: a registry
	// reference, a relative path, a git/https URL, or a Terragrunt
	// HCL-function expression (get_repo_root(), path.module, ...) that
	// normalizeTerraformEvidencePathExpression resolves further.
	Source string `json:"source,omitempty"`
	// Attributes carries every producer field with no named struct field
	// above (line_number, path, lang, version, deployment_name, repo_name,
	// create_deploy, cluster_name, zone_id, deploy_entry_point), preserving
	// each value's JSON-native Go type.
	Attributes map[string]any `json:"-"`
}

// TerragruntDependency is the typed view of one entry in a parsed_file_data
// "terragrunt_dependencies" inner slice: a Terragrunt `dependency "name" {}`
// block (go/internal/parser/hcl/parser.go parseTerragruntDependencies,
// terragrunt.hcl files only). Only Name and ConfigPath are named -- the two
// fields discoverStructuredTerraformEvidence
// (go/internal/relationships/terraform_evidence.go) reads to resolve a
// dependency's config_path to a target repository.
type TerragruntDependency struct {
	// Name is the dependency block's label (the HCL `dependency "name" {}`
	// label).
	Name string `json:"name,omitempty"`
	// ConfigPath is the dependency's declared config_path expression, present
	// only when the block sets one (the producer omits the key entirely
	// otherwise, matching every other optional HCL attribute in this parser).
	ConfigPath string `json:"config_path,omitempty"`
	// Attributes carries every producer field with no named struct field
	// above (line_number, path, lang), preserving each value's JSON-native Go
	// type.
	Attributes map[string]any `json:"-"`
}

// TerragruntConfig is the typed view of one entry in a parsed_file_data
// "terragrunt_configs" inner slice: the single per-file Terragrunt config
// summary row (go/internal/parser/hcl/parser.go parseTerragruntConfig,
// terragrunt.hcl files only -- exactly one row per file). The four
// comma-joined helper-path fields are the ones
// discoverStructuredTerragruntConfigEvidence
// (go/internal/relationships/terragrunt_helper_evidence.go) reads and splits
// with csvValues; each stays a plain comma-joined string here, matching the
// wire shape, rather than pre-splitting into a slice -- the CSV-join/split
// convention is business logic owned by the relationships package, not the
// payload contract.
type TerragruntConfig struct {
	// IncludePaths is the comma-joined set of file paths an `include` block's
	// helper functions (find_in_parent_folders, etc. resolved at parse time)
	// discovered, empty when the file declares no include helper paths.
	IncludePaths string `json:"include_paths,omitempty"`
	// ReadConfigPaths is the comma-joined set of paths a
	// read_terragrunt_config(...) call references, empty when none appear.
	ReadConfigPaths string `json:"read_config_paths,omitempty"`
	// FindInParentFoldersPaths is the comma-joined set of paths a
	// find_in_parent_folders(...) call resolves to, empty when none appear.
	FindInParentFoldersPaths string `json:"find_in_parent_folders_paths,omitempty"`
	// LocalConfigAssetPaths is the comma-joined set of local file(...) /
	// templatefile(...) asset paths the config references, empty when none
	// appear.
	LocalConfigAssetPaths string `json:"local_config_asset_paths,omitempty"`
	// Attributes carries every producer field with no named struct field
	// above (name, line_number, path, lang, terraform_source, includes,
	// locals, inputs), preserving each value's JSON-native Go type.
	Attributes map[string]any `json:"-"`
}
