// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package capabilitycatalog

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const productClaimMarkerPrefix = "<!-- product-claim:"

// ProductClaimMarker records one public source-side product-claim marker.
type ProductClaimMarker struct {
	Path      string
	Line      int
	ID        string
	Unguarded bool
	Raw       string
	Malformed bool
}

// ParseProductClaimMarkers scans README.md and docs/public Markdown files for
// product-claim markers.
func ParseProductClaimMarkers(repoRoot, docsDir string) ([]ProductClaimMarker, error) {
	repoRootAbs, err := filepath.Abs(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve product claim marker repo root %s: %w", repoRoot, err)
	}
	docsDirAbs, err := filepath.Abs(docsDir)
	if err != nil {
		return nil, fmt.Errorf("resolve product claim marker docs dir %s: %w", docsDir, err)
	}

	var files []string
	readmePath := filepath.Join(repoRootAbs, "README.md")
	if _, err := os.Stat(readmePath); err == nil {
		files = append(files, readmePath)
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("stat product claim marker file %s: %w", readmePath, err)
	}
	if err := filepath.WalkDir(docsDirAbs, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || filepath.Ext(path) != ".md" {
			return nil
		}
		files = append(files, path)
		return nil
	}); err != nil {
		return nil, fmt.Errorf("scan product claim markers in %s: %w", docsDirAbs, err)
	}

	var markers []ProductClaimMarker
	for _, path := range files {
		parsed, err := parseProductClaimMarkersInFile(repoRootAbs, path)
		if err != nil {
			return nil, err
		}
		markers = append(markers, parsed...)
	}
	return markers, nil
}

func parseProductClaimMarkersInFile(repoRoot, path string) ([]ProductClaimMarker, error) {
	raw, err := os.ReadFile(path) // #nosec G304 -- path comes from the repo scan above.
	if err != nil {
		return nil, fmt.Errorf("read product claim marker file %s: %w", path, err)
	}
	rel, err := filepath.Rel(repoRoot, path)
	if err != nil {
		return nil, fmt.Errorf("relativize product claim marker path %s: %w", path, err)
	}
	rel = filepath.ToSlash(rel)
	lines := strings.Split(string(raw), "\n")
	var markers []ProductClaimMarker
	for index, line := range lines {
		for _, rawMarker := range productClaimMarkerTexts(line) {
			marker := parseProductClaimMarker(rawMarker)
			marker.Path = rel
			marker.Line = index + 1
			markers = append(markers, marker)
		}
	}
	return markers, nil
}

func productClaimMarkerTexts(line string) []string {
	var out []string
	remaining := line
	for {
		start := strings.Index(remaining, productClaimMarkerPrefix)
		if start < 0 {
			return out
		}
		remaining = remaining[start:]
		end := strings.Index(remaining, "-->")
		if end < 0 {
			out = append(out, remaining)
			return out
		}
		end += len("-->")
		out = append(out, remaining[:end])
		remaining = remaining[end:]
	}
}

func parseProductClaimMarker(raw string) ProductClaimMarker {
	marker := ProductClaimMarker{Raw: raw}
	if !strings.HasSuffix(strings.TrimSpace(raw), "-->") {
		marker.Malformed = true
	}
	body := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(raw, productClaimMarkerPrefix), "-->"))
	for _, field := range strings.Fields(body) {
		key, value, ok := strings.Cut(field, "=")
		if !ok {
			marker.Malformed = true
			continue
		}
		switch key {
		case "id":
			marker.ID = value
		case "state":
			switch value {
			case "unguarded":
				marker.Unguarded = true
			case "guarded":
			default:
				marker.Malformed = true
			}
		default:
			marker.Malformed = true
		}
	}
	if marker.ID == "" {
		marker.Malformed = true
	}
	return marker
}

// CheckProductClaimMarkers verifies that every guarded product-claim marker has
// exactly one ledger row and every ledger row has exactly one guarded marker.
func CheckProductClaimMarkers(ledger ProductClaimLedger, markers []ProductClaimMarker) []ProductClaimFinding {
	ledgerCounts := map[string]int{}
	claimSources := map[string]ProductClaimSource{}
	for _, claim := range ledger.Claims {
		ledgerCounts[claim.ID]++
		claimSources[claim.ID] = claim.Source
	}

	markersByID := map[string][]ProductClaimMarker{}
	var findings []ProductClaimFinding
	for _, marker := range markers {
		if marker.Malformed {
			findings = append(findings, productClaimFinding(ProductClaimFindingMalformed, marker.ID, marker.Path, marker.Line, fmt.Sprintf("malformed product-claim marker: %s", marker.Raw)))
			continue
		}
		if marker.Unguarded {
			continue
		}
		markersByID[marker.ID] = append(markersByID[marker.ID], marker)
		if ledgerCounts[marker.ID] == 0 {
			findings = append(findings, productClaimFinding(ProductClaimFindingMissingMarker, marker.ID, marker.Path, marker.Line, "guarded product-claim marker has no ledger row"))
		}
	}

	for id, count := range ledgerCounts {
		source := claimSources[id]
		markers := markersByID[id]
		switch {
		case count > 1:
			findings = append(findings, productClaimFinding(ProductClaimFindingDuplicateID, id, source.Path, source.Line, "ledger has more than one row for product claim"))
		case len(markers) == 0:
			findings = append(findings, productClaimFinding(ProductClaimFindingMissingMarker, id, source.Path, source.Line, "ledger row has no guarded source marker"))
		case len(markers) > 1:
			findings = append(findings, productClaimFinding(ProductClaimFindingDuplicateID, id, source.Path, source.Line, "product claim has more than one guarded source marker"))
		case markers[0].Path != filepath.ToSlash(source.Path) || markers[0].Line != source.Line:
			findings = append(findings, productClaimFinding(ProductClaimFindingMissingMarker, id, source.Path, source.Line, fmt.Sprintf("guarded marker is at %s:%d, want %s:%d", markers[0].Path, markers[0].Line, source.Path, source.Line)))
		}
	}
	return findings
}
