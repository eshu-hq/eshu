// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package capabilitycatalog

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// CollectorClaimMarker is the HTML-comment marker that binds a collector
// readiness claim in docs to a collector and a readiness lane. It is invisible
// in rendered MkDocs output:
//
//	<!-- collector-state: name=<collector> lane=<readiness_lane> -->
//
// The collector readiness guard checks each marker against the generated surface
// inventory so a docs page cannot claim a collector is implemented (or any other
// lane) when the inventory says otherwise, and cannot claim implemented without
// linked promotion proof.
const CollectorClaimMarker = "collector-state"

var collectorClaimRE = regexp.MustCompile(`<!--\s*collector-state:\s*(.*?)\s*-->`)

// CollectorClaim is a collector readiness claim parsed from a docs page marker.
type CollectorClaim struct {
	// Path is the doc path relative to the scanned docs directory.
	Path string
	// Line is the 1-based line number of the marker.
	Line int
	// Collector is the claimed collector name (a scope collector kind).
	Collector string
	// Lane is the claimed readiness lane.
	Lane ReadinessLane
	// Malformed marks a marker present but missing its name or lane.
	Malformed bool
	// Marker holds the raw marker body for a malformed marker's finding.
	Marker string
}

// CollectorFinding is a contradiction between a docs collector readiness claim
// and the generated surface inventory.
type CollectorFinding struct {
	// Path is the doc path relative to the scanned docs directory.
	Path string `json:"path"`
	// Line is the 1-based line number of the marker.
	Line int `json:"line"`
	// Collector is the claimed collector name.
	Collector string `json:"collector"`
	// Claimed is the readiness lane the doc asserts.
	Claimed ReadinessLane `json:"claimed"`
	// Expected is the surface inventory lane, when the collector is known.
	Expected ReadinessLane `json:"expected,omitempty"`
	// Reason explains the contradiction.
	Reason string `json:"reason"`
}

// ParseCollectorClaims scans every .md file under docsDir for collector-state
// markers and returns the parsed claims sorted by path then line. Markers inside
// fenced code blocks are skipped as illustrative examples. A marker missing its
// name or lane is returned with Malformed set so it surfaces as a finding rather
// than being dropped.
func ParseCollectorClaims(docsDir string) ([]CollectorClaim, error) {
	var claims []CollectorClaim
	err := filepath.WalkDir(docsDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || filepath.Ext(path) != ".md" {
			return nil
		}
		rel, err := filepath.Rel(docsDir, path)
		if err != nil {
			return err
		}
		fileClaims, err := parseFileCollectorClaims(path, rel)
		if err != nil {
			return err
		}
		claims = append(claims, fileClaims...)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scan collector claims: %w", err)
	}
	sort.Slice(claims, func(i, j int) bool {
		if claims[i].Path != claims[j].Path {
			return claims[i].Path < claims[j].Path
		}
		return claims[i].Line < claims[j].Line
	})
	return claims, nil
}

func parseFileCollectorClaims(path, rel string) ([]CollectorClaim, error) {
	file, err := os.Open(path) // #nosec G304 -- path is program-constructed from WalkDir over the operator-configured docsDir, not from external input
	if err != nil {
		return nil, fmt.Errorf("open doc %s: %w", path, err)
	}
	defer func() { _ = file.Close() }()

	var claims []CollectorClaim
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	line := 0
	inFence := false
	for scanner.Scan() {
		line++
		text := scanner.Text()
		if trimmed := strings.TrimSpace(text); strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		for _, match := range collectorClaimRE.FindAllStringSubmatch(text, -1) {
			claim := parseCollectorClaimFields(match[1])
			claim.Path = rel
			claim.Line = line
			claims = append(claims, claim)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read doc %s: %w", path, err)
	}
	return claims, nil
}

func parseCollectorClaimFields(body string) CollectorClaim {
	var claim CollectorClaim
	for _, field := range strings.Fields(body) {
		key, value, found := strings.Cut(field, "=")
		if !found {
			continue
		}
		switch key {
		case "name":
			claim.Collector = value
		case "lane":
			claim.Lane = ReadinessLane(strings.TrimSpace(value))
		}
	}
	if claim.Collector == "" || claim.Lane == "" {
		claim.Malformed = true
		claim.Marker = strings.TrimSpace(body)
	}
	return claim
}

// CheckCollectorReadiness compares parsed collector claims against the generated
// surface inventory and returns a finding for every claim that names an unknown
// collector, uses an invalid lane, contradicts the inventory lane, or claims the
// implemented lane without linked promotion proof. It is the collector
// promotion-proof CI gate: a doc cannot claim a collector is production-ready
// unless the inventory agrees and proof exists.
func CheckCollectorReadiness(inv SurfaceInventory, claims []CollectorClaim) []CollectorFinding {
	byName := make(map[string]SurfaceRecord)
	for _, rec := range inv.Surfaces {
		if rec.Category == SurfaceCollector {
			byName[rec.Name] = rec
		}
	}

	var findings []CollectorFinding
	for _, claim := range claims {
		if claim.Malformed {
			findings = append(findings, CollectorFinding{
				Path: claim.Path, Line: claim.Line, Collector: claim.Marker,
				Reason: "malformed collector-state marker: missing name or lane",
			})
			continue
		}
		rec, ok := byName[claim.Collector]
		if !ok {
			findings = append(findings, CollectorFinding{
				Path: claim.Path, Line: claim.Line, Collector: claim.Collector,
				Claimed: claim.Lane, Reason: "collector is not in the surface inventory",
			})
			continue
		}
		if !claim.Lane.Valid() {
			findings = append(findings, CollectorFinding{
				Path: claim.Path, Line: claim.Line, Collector: claim.Collector,
				Claimed: claim.Lane, Expected: rec.Readiness, Reason: "claimed lane is not a valid readiness lane",
			})
			continue
		}
		if claim.Lane != rec.Readiness {
			findings = append(findings, CollectorFinding{
				Path: claim.Path, Line: claim.Line, Collector: claim.Collector,
				Claimed: claim.Lane, Expected: rec.Readiness, Reason: "doc claim contradicts surface inventory readiness lane",
			})
			continue
		}
		if claim.Lane.RequiresPromotionProof() && rec.Proof == "" {
			findings = append(findings, CollectorFinding{
				Path: claim.Path, Line: claim.Line, Collector: claim.Collector,
				Claimed: claim.Lane, Expected: rec.Readiness,
				Reason: "doc claims collector is implemented but the surface inventory links no promotion proof",
			})
		}
	}
	return findings
}
