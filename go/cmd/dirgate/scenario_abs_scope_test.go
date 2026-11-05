// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import "testing"

// TestAbsR2fARenameOrSwapKeepsTheOvershootDirectoryInScope: a PR that renames a
// file, or swaps one file for another, inside an overshoot directory leaves the
// file count unchanged (41 -> 41) but changes the file set, so the directory is
// touched and the absolute term applies. It must not be read as "untouched"
// just because the count or the length of the set matches.
func TestAbsR2fARenameOrSwapKeepsTheOvershootDirectoryInScope(t *testing.T) {
	pr := map[string]string{"GITHUB_EVENT_NAME": "pull_request"}
	for _, tc := range []struct {
		name string
		edit func(r *scratchRepo)
	}{
		{"rename one file", func(r *scratchRepo) {
			r.git("mv", "go/internal/foo/b000.go", "go/internal/foo/z000.go")
		}},
		{"swap one file for another", func(r *scratchRepo) {
			r.rm("go/internal/foo/b000.go")
			r.addGoFiles(dirFoo, "y", 0, 1)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := raceRepo(t)
			mergeBranch(r, "b")
			r.git("checkout", "-q", "-b", "reshuffle", "main")
			tc.edit(r)
			r.commit("reshuffle one file")
			r.git("checkout", "-q", "main")
			r.git("merge", "-q", "--no-ff", "-m", "Merge reshuffle", "reshuffle")
			r.runEnv(pr, "ci").mustExit(t, 1, "RED", "internal/foo", "41 -> 41", "needs +1")
		})
	}
}
