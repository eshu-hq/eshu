// Package shape converts parser-shaped file payloads into the
// content.Materialization rows persisted by the Postgres content writer.
//
// Materialize walks the parser entity buckets in a fixed order, derives
// canonical content-entity identifiers via content.CanonicalEntityID, builds
// per-entity source_cache snippets from the file body or parser source, records
// line and language metadata, and applies bounded byte limits to oversized
// low-signal labels (currently Variable). The bucket table includes Terraform
// and Terragrunt infrastructure entities, including refactor/import/check and
// lockfile-provider evidence. Output ordering is deterministic so storage diffs
// stay stable across runs.
package shape
