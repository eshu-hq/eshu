// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package hcl extracts Terraform and Terragrunt parser payloads for the parent
// parser engine.
//
// Parse reads one HCL file, preserves the Terraform and Terragrunt bucket
// contract, and returns deterministic rows sorted by name. Terraform parsing
// covers authored configuration blocks, declared PagerDuty module/tfvars
// evidence, declared Grafana folder/dashboard/datasource/rule-group metadata,
// modern import/refactor/check blocks, and provider lock files without mixing
// lockfile providers into provider configuration rows. The package owns HCL syntax
// parsing, local Terragrunt helper-expression
// extraction, and per-resource attribute extraction for drift comparison.
// Registry dispatch and parse timing remain in the parent parser package.
//
// Resource attribute extraction uses cty-value evaluation via
// hclsyntax.Expression.Value so that heredoc strings and escaped-quote strings
// produce the same canonical values the state-side flattener stores, rather
// than raw source bytes that would never match.
//
// Terragrunt parsing also extracts remote_state blocks into the
// terragrunt_remote_states bucket and follows local include chains
// (find_in_parent_folders, read_terragrunt_config, literal include paths) up
// to a bounded depth so a child terragrunt.hcl that inherits its remote_state
// from a parent file is recorded with the parent's backend evidence. Each
// row carries source_path for parser provenance, kept distinct from the
// local backend's path attribute so the two values cannot collide. Walker
// failures (depth exceeded, cycle detected, unsafe-file rejection from the
// include chain's regular-file and size guards) surface as rows in the
// terragrunt_include_warnings bucket so downstream consumers see them.
//
// The terraform_backends bucket (terraform_backend.go) solves the identical
// path/path-attribute collision the opposite way round: its row["path"] is
// the parser provenance field (set first, for every backend block regardless
// of kind) and the local backend's own "path" attribute is stored under
// row["state_path"] instead, because row["path"] here was already the
// provenance field before backends carried a path attribute of their own
// (issue #5594). "state_path", not "local_path", because the durable
// `repository` fact already has an established "local_path" payload field
// meaning the repo checkout root; reusing that name for the backend's path
// attribute would recreate the exact collision this split exists to avoid.
// The two buckets are independent and intentionally keep their own
// established convention; do not "align" one to the other without checking
// every existing consumer of the field each convention actually uses for
// provenance.
package hcl
