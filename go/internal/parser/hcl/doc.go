// Package hcl extracts Terraform and Terragrunt parser payloads for the parent
// parser engine.
//
// Parse reads one HCL file, preserves the Terraform and Terragrunt bucket
// contract, and returns deterministic rows sorted by name. Terraform parsing
// covers authored configuration blocks, modern import/refactor/check blocks,
// and provider lock files without mixing lockfile providers into provider
// configuration rows. The package owns HCL syntax parsing and local Terragrunt
// helper-expression extraction, while registry dispatch and parse timing remain
// in the parent parser package.
package hcl
