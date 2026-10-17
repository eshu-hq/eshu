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
// version_guard_test.go. A change that leaves every row unchanged keeps the
// version. A bump makes every stored row stale, so each repository is derived
// again.
//
// The guard (TestExtractorOutputIsPinned) runs the derive's call of
// LoadConfigPaths over the frozen corpus in testdata/guard. It fails when a
// change moves a stored row over that corpus, when the version changed but
// the output over that corpus did not, and when the corpus bytes changed
// without a matching pin row. It does not see, so each still needs a manual
// bump when it changes: inputs the corpus lacks (add one that shows the
// change to the generator's supplement and regenerate in the bump change);
// the storedRows mapping in storage/postgres/config/artifacts, which the
// guard copies (TestStoredRowsCopiesTheBuildVerbatim there fails on a change
// to it); the content_files statements, whose semantics the guard's store
// re-implements; and artifact_type, which the corpus does not hold. The
// package README, "Extractor version and its guard", has the procedures.
//
// The version is monotonic: never lower it or delete a pin row. A git revert
// of a bump is not a rollback, because the derive's write condition refuses a
// lower extractor version over a state row that a higher-version binary
// wrote. Undo an extractor change with a new bump.
const ExtractorVersion int = 1
