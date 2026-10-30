// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package violation is a planted repository-catalog consumer for the #9567
// guard's RED case. It names two keys the catalog read projects away, one as a
// payload key literal and one as a decoded field, so the guard must report
// both. It avoids a payload map index so the payload-usage gate, which scans
// testdata, does not see a raw read. It is testdata and is never built into a
// binary.
package violation

// importsMapKey is the payload key a consumer would look up.
const importsMapKey = "imports_map"

// Repository mimics a decoded codegraph repository with a projected-away field.
type Repository struct {
	GitRefs []string
}

// Reads returns two values the catalog projection drops.
func Reads(repo Repository) (string, int) {
	return importsMapKey, len(repo.GitRefs)
}
