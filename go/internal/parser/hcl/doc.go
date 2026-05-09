// Package hcl extracts Terraform and Terragrunt parser payloads for the parent
// parser engine.
//
// Parse reads one HCL file, preserves the existing Terraform and Terragrunt
// bucket contract, and returns deterministic rows sorted by name. The package
// owns HCL syntax parsing and local Terragrunt helper-expression extraction,
// while registry dispatch and parse timing remain in the parent parser package.
package hcl
