// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package discovery

import "sort"

// SizeUnavailable is the sentinel FileWithSize.Size value meaning the on-disk
// size could not be observed during the walk (a symlink whose target could not
// be followed). It is negative so it never collides with a genuine file size,
// including a real zero-byte file (Size == 0), which must keep its own weight
// floor rather than the unavailable default.
const SizeUnavailable int64 = -1

// FileWithSize pairs an absolute file path with the on-disk size observed
// during directory walk, so parse partitioning can weight files by byte
// size without a second os.Stat. A negative Size (SizeUnavailable) is a
// sentinel that means the size was not observed and the caller should apply
// its own default weight; a Size of 0 is a genuine empty file.
type FileWithSize struct {
	Path string
	Size int64
}

// RepoFileSet groups one repo root with its discovered supported files.
//
// RepoRoot and Files entries are absolute paths and Files are sorted for
// stable output.
type RepoFileSet struct {
	RepoRoot string
	Files    []FileWithSize
}

// FilePaths returns just the absolute file paths from the set, in order.
func (fs RepoFileSet) FilePaths() []string {
	return FilePaths(fs.Files)
}

// FilePaths extracts the absolute file paths from a FileWithSize slice, in
// order. It is the single canonical path-extraction helper shared by the
// RepoFileSet method and collector-side callers that hold a bare slice.
func FilePaths(files []FileWithSize) []string {
	paths := make([]string, len(files))
	for i, f := range files {
		paths[i] = f.Path
	}
	return paths
}

func sortFileWithSizeSlice(files []FileWithSize) {
	sort.Slice(files, func(i, j int) bool {
		return files[i].Path < files[j].Path
	})
}
