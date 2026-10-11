// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package artifacts

const (
	// HydrationFloor is the number of config-artifact candidates every
	// repository of a live load always hydrates (#7951). It was the hard cap of
	// the load before #7951, so a load with any allowance at or above the floor
	// returns a superset of the capped rows. The query package's anchor-only
	// loaders (compose, workflow, controller and runtime artifacts) still use it
	// as their cap until they move to the chunked reader.
	HydrationFloor = 50

	// AllowanceWholeWindow is the allowance that selects every candidate in
	// the list window. A content-write derive passes it: a derive is per
	// repository and must store the complete rows, so a stored answer has no
	// request budget to disclose.
	AllowanceWholeWindow = -1

	// ListLimit is the file-list window of the config-artifact load: the first
	// ListLimit files of a repository by relative_path. It equals
	// querycontract.RepositorySemanticEntityLimit, which the repository story
	// uses for the anchor list; a test in the query package pins the two
	// together. LoadConfigPaths reads ListLimit+1 rows so a cut list is known
	// (ReadStats.ListTruncated), never silent.
	ListLimit = 5000
)

// SelectHydrationIndexes returns, in list order, the positions of the first
// allowance files that satisfy shouldHydrate and have no content yet (content
// returns the empty string), and the number of such files in the whole list:
// the candidate pool. A file that already carries content is not in the pool.
// A negative allowance selects the whole pool; LoadConfigPaths accepts only
// AllowanceWholeWindow as a negative value.
//
// It is the single definition of the hydration set: the anchor repository's
// full pass and the needed-content pass both start from it, so the selection
// and the candidate order cannot differ between them. It is generic so the
// query package applies it to its own file type without a copy.
func SelectHydrationIndexes[T any](
	files []T,
	shouldHydrate func(T) bool,
	content func(T) string,
	allowance int,
) (indexes []int, candidates int) {
	indexes = make([]int, 0, min(max(allowance, 0), len(files)))
	for i, file := range files {
		if !shouldHydrate(file) || content(file) != "" {
			continue
		}
		candidates++
		if allowance >= 0 && len(indexes) >= allowance {
			continue
		}
		indexes = append(indexes, i)
	}
	return indexes, candidates
}

// fileContent returns the stored content of a file, for SelectHydrationIndexes.
func fileContent(file File) string { return file.Content }
