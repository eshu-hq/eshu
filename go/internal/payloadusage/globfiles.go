// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package payloadusage

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

// decodeSeamFileNamePattern is the filename shape (matched with
// filepath.Match against a file's base name, not a full path) every
// decode-seam resolver in this package looks for: the base file
// (factschema_decode.go) plus any per-family split
// (factschema_decode_<family>.go, see DecodeFiles' doc comment on Paths).
const decodeSeamFileNamePattern = "factschema_decode*.go"

// globFilesRecursive returns every file anywhere under dir (at any depth)
// whose base name matches namePattern (a filepath.Match pattern applied to
// the base name only, e.g. "factschema_decode*.go"), sorted for
// deterministic output. A missing dir is not an error — it returns a nil
// slice, matching filepath.Glob's own behavior for a pattern whose parent
// directory does not exist — so callers that treat "resolved to nothing" as
// their own signal (a fail-closed error for the reducer, a valid empty
// result for the migrating optional surfaces) keep working unchanged.
//
// This replaces the three factschema_decode*.go filepath.Glob call sites
// this package used before #6055. filepath.Glob (like a shell glob) never
// crosses a "/": pattern "dir/factschema_decode*.go" matches only files
// directly inside dir, not dir/subdir/factschema_decode_foo.go. A
// restructure that moves decode-seam files family-by-family (epic #6053)
// can leave SOME factschema_decode*.go files at the top level while
// relocating others into a subdirectory; the top-level glob still matches
// the leftovers, so the result stays non-empty and every existence/fail-closed
// check downstream stays silent about the files that moved out of its reach
// — the exact "gate passes on nothing new" risk #6055 exists to close.
// Walking the whole subtree makes seam resolution correct regardless of how
// deep a moved file ends up.
func globFilesRecursive(dir, namePattern string) ([]string, error) {
	var matches []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if path == dir && os.IsNotExist(walkErr) {
				// A missing root is not this function's concern to report —
				// filepath.Glob returns (nil, nil) for a pattern whose
				// parent directory does not exist, and callers already
				// decide what an empty result means for them (a fail-closed
				// error for the reducer, a valid empty result for the
				// migrating optional surfaces).
				return filepath.SkipAll
			}
			return walkErr
		}
		if d.IsDir() {
			// testdata holds deliberately broken fixtures that must never be
			// read as production source. filepath.Glob never crossed a `/`, so
			// this directory was unreachable before the walk went recursive;
			// skipping it keeps that property and matches parseReducerDir in
			// usage.go and the exclusion every other gate here applies.
			if d.Name() == "testdata" && path != dir {
				return filepath.SkipDir
			}
			return nil
		}
		ok, matchErr := filepath.Match(namePattern, d.Name())
		if matchErr != nil {
			return fmt.Errorf("payloadusage: match %s against %s: %w", namePattern, path, matchErr)
		}
		if ok {
			matches = append(matches, path)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("payloadusage: walk %s: %w", dir, err)
	}
	sort.Strings(matches)
	return matches, nil
}
