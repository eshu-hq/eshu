// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"path/filepath"
)

// grandfatherEntry pins one directory's known-offender state at the
// commit it was grandfathered, mirroring
// go/internal/mcp/read_surface_grandfather.go and
// go/internal/queryplan/grandfathered_non_hot.go's digest-pinned-ledger
// mechanism. See scripts/lib/dirgate-grandfather.tsv (the source of
// truth) and grandfather.go (generated from it) for the full ledger and
// the exact semantics FileCount and Digest are checked with.
type grandfatherEntry struct {
	// FileCount is the qualifying-file count pinned at landing. A
	// directory in the ledger passes at this count or below; it fails the
	// moment its live count exceeds it.
	FileCount int
	// Digest is qualifyingDigest of the pinned file set. It only matters
	// when the live count still equals FileCount, where it catches a
	// same-count swap (one file removed, a different one added) that pure
	// counting would miss.
	Digest string
	// NamingExempt pins the exact basenames of files that already violated
	// the sibling-subpackage naming rule when this directory was
	// grandfathered. A file's exemption depends only on its own name being
	// listed here -- NEVER on the directory's aggregate file count, unlike
	// FileCount/Digest above. This is deliberate: gating the naming check
	// on the aggregate count (the pre-fix behavior) suppressed it for the
	// WHOLE directory for as long as the live count stayed at or below the
	// pin, which silently hid brand-new naming violations, and un-hid
	// pre-existing ones the moment any OTHER file pushed the count over the
	// pin. See namingExemptSet and evaluateDirectory's naming section.
	NamingExempt []string
}

// finding is one reportable dirgate diagnostic. File is the qualifying
// file's basename the finding should be reported against (the
// representative file for a cap finding, the offending file itself for a
// naming finding); run() resolves it to a token.Pos.
type finding struct {
	File    string
	Message string
}

// evaluateDirectory decides the findings dirgate should report for the
// package directory dir (whose grandfather-ledger key is key), combining:
//
//  1. the 40-non-test-.go-file cap, gated by the digest-pinned
//     FileCount/Digest envelope (evaluateCapViolation) when grandfathered,
//  2. the sibling-subpackage naming rule, gated PER FILE by the
//     grandfather entry's NamingExempt list (namingExemptSet) when
//     grandfathered -- never by the directory's aggregate file count, and
//  3. the //nolint:dirgate escape hatch, checked per finding against its
//     own reported file.
//
// The cap check and the naming check are independent: a directory can be
// over its cap while every naming violation stays pinned-exempt, or under
// its cap while a brand-new naming violation is reported. grandfather is
// nil in tests exercising an ungrandfathered directory.
//
// It is deliberately independent of analysis.Pass / the AST so it can be
// tested directly against a real temp directory; run() is the only
// caller that needs a *analysis.Pass, purely to turn File back into a
// token.Pos.
func evaluateDirectory(key, dir string, grandfather map[string]grandfatherEntry) ([]finding, error) {
	files, err := qualifyingFiles(dir)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, nil
	}

	subpkgs, err := namingSubpackages(dir)
	if err != nil {
		return nil, err
	}
	namingViols := detectNamingViolations(files, subpkgs)

	count := len(files)
	digest := qualifyingDigest(files)
	entry, grandfathered := grandfather[key]

	capViolates, capNote := evaluateCapViolation(key, count, digest, entry, grandfathered)
	namingExempt := namingExemptSet(grandfathered, entry)

	var out []finding

	if capViolates {
		rep := representativeFile(files)
		// A cap nolint goes on the directory's representative file and would
		// suppress the cap for EVERY file in that directory, indefinitely. On a
		// grandfathered directory that is catastrophic: one marker on
		// internal/query's doc.go un-gates 880 files for good, and the "split it
		// into a subpackage" exit does not even compile for query, reducer,
		// projector or mcp until the acyclic boundary lands (see the Part 3
		// prerequisite in docs/internal/design/package-restructure.md). So the
		// escape hatch is refused here: a grandfathered directory's only exit is
		// a reviewed pin bump, which shows up as a one-line TSV diff a reviewer
		// can see and argue with.
		if grandfathered {
			out = append(out, finding{File: rep, Message: capMessage(key, count, capNote)})
		} else if _, justified := nolintJustification(filepath.Join(dir, rep), gateName); !justified {
			out = append(out, finding{File: rep, Message: capMessage(key, count, capNote)})
		}
	}

	for _, v := range namingViols {
		if namingExempt[v.File] {
			continue
		}
		if _, justified := nolintJustification(filepath.Join(dir, v.File), gateName); justified {
			continue
		}
		out = append(out, finding{File: v.File, Message: namingMessage(v)})
	}

	return out, nil
}

// namingExemptSet builds the lookup set of basenames pinned as legacy
// naming exemptions for a grandfathered directory. It deliberately ignores
// the directory's aggregate file count -- see the doc comment on
// grandfatherEntry.NamingExempt for why gating on the count was the bug.
// Returns nil (a safe, always-false-lookup zero value) when dir is not
// grandfathered or has no pinned exemptions.
func namingExemptSet(grandfathered bool, entry grandfatherEntry) map[string]bool {
	if !grandfathered || len(entry.NamingExempt) == 0 {
		return nil
	}
	set := make(map[string]bool, len(entry.NamingExempt))
	for _, f := range entry.NamingExempt {
		set[f] = true
	}
	return set
}

// evaluateCapViolation applies the size cap and, when the directory is
// grandfathered, the pinned-envelope rule. The ledger is a MONOTONIC
// ratchet, not a one-time snapshot: a grandfathered directory passes only
// while its live state exactly matches the pin (same count, same digest).
//
//   - Holding exactly at the pinned count requires the digest to still
//     match (otherwise the file set was swapped, not just trimmed).
//   - Shrinking below the pinned count is a violation too, NOT a free
//     pass: without a digest check, a directory pinned at 100 could shrink
//     to 50 and then silently regrow to 99 -- one file short of its
//     original pin -- and no check would ever fire, because the live
//     count would stay below entry.FileCount the whole way. Requiring a
//     re-pin on every shrink closes that gap: after the fix lands, growth
//     is always measured against the BEST (lowest) state the directory
//     ever reached, not the count it happened to have when the gate was
//     first written.
//   - Exceeding the pinned count fails regardless of digest -- growth
//     un-grandfathers the directory outright.
//
// A directory whose live count drops to or below maxDirFiles entirely is
// NOT covered by this rule -- it is no longer a cap offender at all, and
// its ledger row should be deleted (see
// dirgate_report_removable_grandfathers's soft NOTE), not re-pinned.
func evaluateCapViolation(key string, count int, digest string, entry grandfatherEntry, grandfathered bool) (violates bool, note string) {
	if count <= maxDirFiles {
		return false, ""
	}
	if !grandfathered {
		return true, ""
	}
	switch {
	case count == entry.FileCount && digest == entry.Digest:
		return false, ""
	case count == entry.FileCount:
		return true, fmt.Sprintf(
			"file set changed at its pinned count of %d (one file was swapped for another); the grandfathered digest no longer matches",
			entry.FileCount)
	case count < entry.FileCount:
		return true, fmt.Sprintf(
			"shrunk from its grandfathered count of %d to %d; re-pin this row before any further growth is measured against it -- "+
				"run `bash scripts/dev/precommit-go.sh dirgate-digest %s`, update this row in scripts/lib/dirgate-grandfather.tsv "+
				"to the printed count and digest, then regenerate tools/golangci-lint-dirgate/grandfather.go with "+
				"`bash scripts/generate-dirgate-grandfather-go.sh`",
			entry.FileCount, count, key)
	default:
		return true, fmt.Sprintf("grew from its grandfathered count of %d to %d", entry.FileCount, count)
	}
}

func capMessage(dirKey string, count int, note string) string {
	msg := fmt.Sprintf("package directory %s has %d non-test .go files, exceeding the %d-file cap", dirKey, count, maxDirFiles)
	if note != "" {
		msg += " (" + note + ")"
	}
	msg += fmt.Sprintf(
		"; split it into a subpackage, or add //nolint:%s // <reason> to this file's package line",
		gateName)
	return msg
}

func namingMessage(v namingViolation) string {
	return fmt.Sprintf(
		"%s should move into the sibling subpackage %q (its name matches that package); move it, or add //nolint:%s // <reason> to its package line",
		v.File, v.Subpackage, gateName)
}
