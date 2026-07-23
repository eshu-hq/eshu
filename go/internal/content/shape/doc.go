// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package shape converts parser-shaped file payloads into the
// content.Materialization rows persisted by the Postgres content writer.
//
// Materialize walks the parser entity buckets in a fixed order, derives
// canonical content-entity identifiers via
// content.CanonicalEntityIDWithMetadata — which routes in-scope manifest
// dependency Variables (npm, composer, cargo, gradle, maven, nuget, pypi, go,
// rubygems, pub, hex; non-lockfile) to the section-keyed, line-independent
// content.CanonicalDependencyEntityID and falls back to the legacy line-keyed
// content.CanonicalEntityID for everything else — builds
// per-entity source_cache snippets from the file body or parser source, records
// line and language metadata, and applies bounded byte limits to oversized
// low-signal labels (currently Variable). The bucket table includes Terraform
// and Terragrunt infrastructure entities, including refactor/import/check and
// lockfile-provider evidence. Output ordering is deterministic so storage diffs
// stay stable across runs.
package shape
