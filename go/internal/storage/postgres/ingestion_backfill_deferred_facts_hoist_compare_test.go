// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/relationships"
)

func evidenceSetsEqual(a, b []relationships.EvidenceFact) bool {
	if len(a) != len(b) {
		return false
	}
	key := func(f relationships.EvidenceFact) string {
		return string(f.EvidenceKind) + "|" + f.SourceRepoID + "|" + f.TargetRepoID + "|" + fmt.Sprint(f.Details["path"]) + "|" + fmt.Sprint(f.Details["matched_value"])
	}
	seenA := make(map[string]int, len(a))
	for _, f := range a {
		seenA[key(f)]++
	}
	for _, f := range b {
		k := key(f)
		if seenA[k] == 0 {
			return false
		}
		seenA[k]--
	}
	for _, count := range seenA {
		if count != 0 {
			return false
		}
	}
	return true
}
