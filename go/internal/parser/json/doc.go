// Package json parses JSON and JSONC configuration, CloudFormation, and
// data-intelligence documents for the parent parser engine.
//
// Parse reads one file, preserves legacy JSON payload buckets, and returns
// deterministic rows for dependency manifests, npm and Composer lockfile
// versions, TypeScript configs, `.jsonc` config files, CloudFormation
// templates, dbt manifests, and replay fixture documents. JSONC normalization strips comments
// and trailing commas with bounded scans before strict JSON decoding. The
// package depends on shared parser helpers and CloudFormation extraction, but
// it does not import the parent parser package; parent-owned dbt SQL lineage is
// supplied through Config and converted at the parent wrapper boundary.
//
// DependencyCoverage publishes the per-ecosystem repository dependency
// parser coverage matrix that the supply-chain impact reducer relies on.
// Each entry records whether a manifest or lockfile is parsed into
// content_entity dependency facts (Covered) or is still a Gap; gap entries
// preserve the safety rule that missing dependency evidence is neither safe
// nor affected. Guard tests in dependency_coverage_test.go keep the matrix
// honest by exercising Parse against fixtures for every covered file and
// asserting that gap files emit no dependency rows.
package json
