// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package artifacts

// ExtractorVersion is the version of the output of the config-artifact
// extractors: the stored rows (raw_path, relative_path, evidence_kind,
// name_scoped) that the per-repository function LoadConfigPaths yields for
// a repository's files under the name-free build. A content-write derive
// stores it on each repository's state row, and a stored row is trusted only
// by a reader that runs the same version. A row written under another version
// is served from the live path until a derive rewrites it (#7126, ADR 7126
// section "Extractor version").
//
// Bump rule: a change that can alter those rows for any input needs a bump.
// That includes an extractor, a path rule, the clean step, the hydration
// selection, the list window, the dedup or the name-scope table. Bump
// ExtractorVersion by one and, in the same change, append the new pinned row
// (version, corpus digest, output digest) to extractorPins in
// version_guard_test.go. The guard runs the derive's call of LoadConfigPaths
// over the committed corpus in testdata/guard. It fails when the output
// digest changed at an unchanged version, when the version changed but the
// output did not, and when the corpus bytes changed outside the re-snapshot
// procedure in the package README. A change that leaves every row unchanged
// keeps the version. A bump makes every stored row stale, so each repository
// is derived again.
const ExtractorVersion int = 1
