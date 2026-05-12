// Package json parses JSON and JSONC configuration, CloudFormation, and
// data-intelligence documents for the parent parser engine.
//
// Parse reads one file, preserves legacy JSON payload buckets, and returns
// deterministic rows for dependency manifests, TypeScript configs, `.jsonc`
// config files, CloudFormation templates, dbt manifests, and replay fixture
// documents. JSONC normalization strips comments and trailing commas with
// bounded scans before strict JSON decoding. The package depends on shared
// parser helpers and CloudFormation extraction, but it does not import the
// parent parser package; parent-owned dbt SQL lineage is supplied through Config
// and converted at the parent wrapper boundary.
package json
