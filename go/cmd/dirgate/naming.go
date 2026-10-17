// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"path"
	"strings"
)

// collision is a file whose name belongs in a sibling subpackage.
type collision struct {
	dir, file, sub string
}

// collisionsOf lists every collision in a census: a qualifying file whose stem
// equals a sibling subpackage name or starts with that name and an underscore.
// The list is sorted by directory, then file.
func collisionsOf(s snapshot) []collision {
	var out []collision
	for _, dir := range sortedKeys(s.qualifying) {
		subs := s.subpackages[dir]
		if len(subs) == 0 {
			continue
		}
		for _, file := range sortedKeys(s.qualifying[dir]) {
			stem := strings.TrimSuffix(file, ".go")
			for _, sub := range subs {
				if stem == sub || strings.HasPrefix(stem, sub+"_") {
					out = append(out, collision{dir: dir, file: file, sub: sub})
					break
				}
			}
		}
	}
	return out
}

// namingFindings applies the sibling-subpackage rule absolutely. A collision is
// RED unless the file carries `//nolint:dirgate // <reason>` or has a row in the
// naming baseline. It returns the set of colliding file paths for stale-marker
// detection.
func namingFindings(in evalInput) (map[string]bool, []finding) {
	violators := map[string]bool{}
	var out []finding
	for _, c := range collisionsOf(in.head) {
		full := path.Join(goRoot, c.dir, c.file)
		violators[full] = true
		if in.only != nil && !in.only[c.dir] {
			continue
		}
		if m, ok := in.markers[full]; ok && m.hasReason {
			continue
		}
		if _, ok := in.baseline.naming[c.dir][c.file]; ok {
			continue
		}
		out = append(out, finding{sev: sevRed, kind: kindNaming, dir: c.dir, file: full, msg: fmt.Sprintf(
			"RED %s: the name matches sibling package %q and is not in the naming baseline. "+
				"Move it into %s/, or add `//nolint:dirgate // <reason>` to its package line.", full, c.sub, c.sub)})
	}
	return violators, out
}
