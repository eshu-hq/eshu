// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

// selectionBackpressureSupportsMode reports whether the schedule leg may defer
// repositories in the given source mode (#10882). A deferral is only safe when
// the mode's sync keeps durable per-repository state, so a repository the leg
// skips is found again on a later cycle: explicit, githubOrg and gitlabGroup
// compare each repository's remote head with its local checkout. Filesystem mode
// compares one whole-root fingerprint and stamps it after copying only the
// repositories it was handed, so a deferred repository would never be selected
// again until the corpus changes (copy mode also deletes its managed copy).
// Filesystem mode keeps the admission and pre-write pacing legs; only this leg is
// undefined there, and startup refuses the pair (WireSelectionBackpressure).
func selectionBackpressureSupportsMode(sourceMode string) bool {
	switch sourceMode {
	case "explicit", "githubOrg", "gitlabGroup":
		return true
	default:
		return false
	}
}
