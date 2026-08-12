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
//  1. the 40-non-test-.go-file cap,
//  2. the sibling-subpackage naming rule,
//  3. the digest-pinned grandfather ledger (grandfather, nil if none
//     applies -- e.g. in tests exercising an ungrandfathered directory),
//     and
//  4. the //nolint:dirgate escape hatch, checked per finding against its
//     own reported file.
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

	capViolates, capNote := evaluateCapViolation(count, digest, entry, grandfathered)
	namingCovered := grandfathered && count <= entry.FileCount

	var out []finding

	if capViolates {
		rep := representativeFile(files)
		if _, justified := nolintJustification(filepath.Join(dir, rep), gateName); !justified {
			out = append(out, finding{File: rep, Message: capMessage(key, count, capNote)})
		}
	}

	if !namingCovered {
		for _, v := range namingViols {
			if _, justified := nolintJustification(filepath.Join(dir, v.File), gateName); justified {
				continue
			}
			out = append(out, finding{File: v.File, Message: namingMessage(v)})
		}
	}

	return out, nil
}

// evaluateCapViolation applies the size cap and, when the directory is
// grandfathered, the pinned-envelope rule: shrinking below the pinned
// count is always fine, holding exactly at the pinned count requires the
// digest to still match (otherwise the file set was swapped, not just
// trimmed), and exceeding the pinned count fails regardless of digest --
// growth un-grandfathers the directory outright.
func evaluateCapViolation(count int, digest string, entry grandfatherEntry, grandfathered bool) (violates bool, note string) {
	if count <= maxDirFiles {
		return false, ""
	}
	if !grandfathered {
		return true, ""
	}
	switch {
	case count < entry.FileCount:
		return false, ""
	case count == entry.FileCount && digest == entry.Digest:
		return false, ""
	case count == entry.FileCount:
		return true, fmt.Sprintf(
			"file set changed at its pinned count of %d (one file was swapped for another); the grandfathered digest no longer matches",
			entry.FileCount)
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
