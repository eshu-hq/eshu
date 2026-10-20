// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

// snapshotDeclaresEmptyScope reports whether a repository snapshot is
// genuinely fileless, so streamFacts can declare `empty_scope` on the
// generation's repository fact (issue #8657, marker contract #8668).
//
// The gate is strict: it declares only when discovery found zero files
// (FileCount), no file content was parsed or carried on any carrier
// (FileData, ContentFiles, ContentEntities, ContentFileMetas,
// DocumentationFileMetas, WorkflowImageFileMetas), no paths were deleted
// or delta-scoped, and the snapshot is not a delta. A scope whose files
// were discovered but dropped before emission must never declare:
// downstream the build carries zero file facts either way, and only the
// absent declaration keeps the writer-side retract check loud for that
// shape. Documentation-only and other non-file content does not declare:
// those scopes carry no File facts but did observe files, so the probe
// at the writer (not the declaration) decides them against prior nodes.
func snapshotDeclaresEmptyScope(snapshot *RepositorySnapshot) bool {
	if snapshot == nil {
		return false
	}
	if snapshot.Delta {
		return false
	}
	if snapshot.FileCount != 0 {
		return false
	}
	if len(snapshot.FileData) != 0 {
		return false
	}
	if len(snapshot.ContentFiles) != 0 {
		return false
	}
	if len(snapshot.ContentEntities) != 0 {
		return false
	}
	if len(snapshot.ContentFileMetas) != 0 {
		return false
	}
	if len(snapshot.DocumentationFileMetas) != 0 {
		return false
	}
	if len(snapshot.WorkflowImageFileMetas) != 0 {
		return false
	}
	if len(snapshot.DeltaRelativePaths) != 0 {
		return false
	}
	if len(snapshot.DeletedRelativePaths) != 0 {
		return false
	}
	return true
}
