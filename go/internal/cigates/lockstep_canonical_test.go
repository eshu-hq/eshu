// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"os"
	"path/filepath"
	"strings"
)

// dsExactRel accepts an existing path only when every component has the
// spelling recorded in the checkout. Stat alone accepts case aliases on some hosts.
func dsExactRel(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return ""
	}
	if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return filepath.ToSlash(rel)
	}
	dir := root
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return ""
		}
		exact := false
		for _, entry := range entries {
			if entry.Name() == part {
				exact = true
				break
			}
		}
		if !exact {
			return ""
		}
		dir = filepath.Join(dir, part)
	}
	return filepath.ToSlash(rel)
}
