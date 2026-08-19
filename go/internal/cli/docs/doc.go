// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package docs verifies documentation claims against Eshu truth sources for
// `eshu docs verify`: it inventories Markdown files, resolves the CLI command,
// HTTP endpoint, environment variable, local path, container image, and
// Terraform address claims they make, and optionally persists the findings as
// facts.
//
// Verify is the entry point. It returns the verification result and a
// PersistenceSummary; it does not choose an output format or an exit code.
// The package reads no cobra flags, reads no environment variable, and never
// calls os.Exit. go/cmd/eshu's docs.go is the thin cobra wrapper: it parses
// flags, resolves the "auto" image truth mode (which needs both flags and the
// environment), builds the API client, walks the live cobra tree for the
// command surface, opens Postgres from the environment, and maps a result onto
// the CLI's exit-code contract.
//
// # Filesystem surface
//
// This package reads from the filesystem and never writes to it. There is no
// os.Create, os.WriteFile, os.Mkdir, os.Remove, os.Rename, or os.OpenFile in
// its non-test code, and neither doctruth nor eshulocal.ResolveWorkspaceRoot
// writes on the paths called here. The only persistent writes this package
// performs go to Postgres, through Persistence.CommitScopeGeneration.
//
// The reads are:
//
//   - InventoryDocuments stats VerifyOptions.Path, and for a directory walks it
//     with filepath.WalkDir, skipping .git, node_modules, and vendor, opening
//     each .md/.mdx/.markdown file. Content is bounded by
//     VerifyOptions.MaxDocumentBytes; the rest of each file is still streamed
//     through the hash so the revision id covers the whole file.
//   - EnvironmentTruth stats the scan path, globs environment-*.md, and reads
//     each environment reference page it finds. Candidates are, for every
//     ancestor directory of the scan path, reference/, docs/public/reference/,
//     and docs/docs/reference/, plus those last two relative to the working
//     directory and its parent.
//   - LocalPathResolver stats one candidate per documented path, after
//     safeJoinLocalPath confirms the candidate stays inside the workspace root,
//     and calls filepath.EvalSymlinks on the claiming document's directory.
//   - LocalContainerImageResolver walks the workspace root for Dockerfiles and
//     .yaml/.yml/.json/.toml manifests.
//   - TerraformAddressResolver walks the workspace root for .tf and .tf.json
//     files.
//   - TruthRoot delegates to eshulocal.ResolveWorkspaceRoot, which stats
//     ancestor directories looking for a .eshu.yaml then a .git marker.
//
// The last three read the resolved workspace root, not VerifyOptions.Path:
// verifying a single README still scans that README's whole workspace for
// manifests and Terraform files. Both walks are bounded at 2000 files and
// 512 KiB per file, and skip .git, .worktrees, node_modules, vendor, dist,
// build, and site (the Terraform walk also skips .terraform).
//
// # Bounded scans report unsupported, never contradicted
//
// Every truth scan here can come back incomplete: a file limit was reached, a
// file was oversized or unreadable, or HCL would not parse. An incomplete scan
// makes an unmatched claim report unsupported, which the verifier records as
// missing evidence. It never reports contradicted. A bounded scan that has not
// seen everything is not evidence that a documented image or address is
// absent, and treating it as such would turn a correct document into a
// verification failure.
package docs
