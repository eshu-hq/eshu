// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package artifacts derives the config-path rows of one repository from its
// stored files, and defines the content_files statements that read them. It is
// the neutral home both the live query reader and a content-write derive in
// storage/postgres import, so the stored answer for a repository cannot differ
// from the live answer for the same content_files state (#7126).
//
// The per-repository function is LoadConfigPaths. It lists a repository's
// first ListLimit files by relative_path with a one-row sentinel that reports
// a cut list (or takes the caller's list), selects the first allowance
// config-artifact candidates (HydrationFloor or more for a live load, every
// candidate for AllowanceWholeWindow), reads only the content
// an extractor consumes (kustomize, Docker Compose, Terraform and Terragrunt
// HCL, Ansible playbooks), asks the store which other candidates are non-blank
// in chunks of BlankCheckChunkSize paths, and runs BuildConfigPaths over the
// result. ReadStats reports the candidate pool, the candidates the allowance
// took and a cut list, so the caller can disclose every bound. BuildConfigPaths("", files) is the
// name-free build a stored read model keeps; the read-time name step is
// StripRepositoryName on the rows whose evidence kind is name-scoped
// (NameScoped), then source_repo. CleanRelativePath is the clean step and must
// not be applied a second time to a stored path.
//
// Row order is total. CompareConfigPaths orders on path, source_repo,
// relative_path and evidence_kind, MergeConfigPaths keeps one dedup map across
// the rows of every repository, and SortConfigPaths adds a stable tie-breaker.
//
// The four content_files statements (ListStatement, GetFileStatement,
// GetFilesStatement, NonBlankPathsStatement) each have one SQL constant and one
// row mapper (FileScanTargets, RelativePathScanTargets). File is the package's own file type.
// Reader is the port the per-repository function reads through. The package
// has no database/sql, telemetry or query-contract dependency: a caller owns
// the connection, the spans and the metrics.
//
// ExtractorVersion versions the output of the extractors. A stored row is
// trusted only at the version that wrote it, and a change that can alter any
// stored row bumps the version together with the digest that
// TestExtractorOutputIsPinned pins over the committed guard corpus, and with
// the hydration window (ListLimit, AllowanceWholeWindow) the pin row records.
// The digests see only changes that move a row over that corpus, and the
// version only moves forward.
//
// Extraction is pure. The extractors read no clock, environment or network,
// drop remote references without reading them, and resolve kustomize
// references only inside the same repository's file index.
package artifacts
