// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package discovery

import "sort"

// FileWithSize pairs an absolute file path with the on-disk size observed
// during directory walk, so parse partitioning can weight files by byte
// size without a second os.Stat. A zero Size is a sentinel that means
// the size was not available (Info() failed) and the caller should apply
// its own default weight.
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
	paths := make([]string, len(fs.Files))
	for i, f := range fs.Files {
		paths[i] = f.Path
	}
	return paths
}

func sortFileWithSizeSlice(files []FileWithSize) {
	sort.Slice(files, func(i, j int) bool {
		return files[i].Path < files[j].Path
	})
}
