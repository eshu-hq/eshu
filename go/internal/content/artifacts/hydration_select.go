// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package artifacts

// HydrationLimit bounds the files one hydration pass reads: the first
// HydrationLimit candidates in list order that carry no content yet.
const HydrationLimit = 50

// ListLimit is the file-list window of the config-artifact load: the first
// ListLimit files of a repository by relative_path. It equals
// querycontract.RepositorySemanticEntityLimit, which the repository story uses
// for the anchor list; a test in the query package pins the two together.
const ListLimit = 5000

// SelectHydrationIndexes returns the positions of the first HydrationLimit
// files that satisfy shouldHydrate and have no content yet (content returns
// the empty string), in list order. A file that already carries content does
// not use a slot. It is the single definition of the hydration set: the anchor
// repository's full pass and the needed-content pass both start from it, so the
// cap and the candidate order cannot differ between them. It is generic so the
// query package applies it to its own file type without a copy.
func SelectHydrationIndexes[T any](files []T, shouldHydrate func(T) bool, content func(T) string) []int {
	indexes := make([]int, 0, HydrationLimit)
	for i, file := range files {
		if len(indexes) >= HydrationLimit {
			break
		}
		if !shouldHydrate(file) || content(file) != "" {
			continue
		}
		indexes = append(indexes, i)
	}
	return indexes
}

// fileContent returns the stored content of a file, for SelectHydrationIndexes.
func fileContent(file File) string { return file.Content }
