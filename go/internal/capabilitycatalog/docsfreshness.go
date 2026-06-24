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
	"strconv"
	"strings"
)

// DocClaimMarker is the HTML-comment marker that binds a docs capability-state
// claim to a stable capability id. It is invisible in rendered MkDocs output:
//
//	<!-- capability-state: id=<capability_id> state=<state> [issue=<n>] -->
//
// The docs freshness guard checks each marker's state against the catalog so a
// docs page cannot silently contradict code-owned capability truth.
const DocClaimMarker = "capability-state"

var docClaimRE = regexp.MustCompile(`<!--\s*capability-state:\s*(.*?)\s*-->`)

// DocClaim is a capability-state claim parsed from a docs page marker.
type DocClaim struct {
	// Path is the doc path relative to the scanned docs directory.
	Path string
	// Line is the 1-based line number of the marker.
	Line int
	// Capability is the claimed capability id.
	Capability string
	// State is the claimed maturity.
	State Maturity
	// Issue is an optional tracking issue number.
	Issue int
	// Malformed marks a capability-state marker that is present but missing its
	// id or state. Malformed markers are surfaced as findings rather than
	// dropped, so a typo cannot bypass the freshness gate. Marker holds the raw
	// marker body for the finding detail.
	Malformed bool
	// Marker is the raw marker body, populated for malformed markers.
	Marker string
}

// DocFinding is a contradiction between a docs claim and the catalog.
type DocFinding struct {
	// Path is the doc path relative to the scanned docs directory.
	Path string `json:"path"`
	// Line is the 1-based line number of the marker.
	Line int `json:"line"`
	// Capability is the claimed capability id.
	Capability string `json:"capability"`
	// Claimed is the maturity the doc asserts.
	Claimed Maturity `json:"claimed"`
	// Expected is the catalog maturity, when the capability is known.
	Expected Maturity `json:"expected,omitempty"`
	// Reason explains the contradiction.
	Reason string `json:"reason"`
}

// allMaturities is the closed set of valid maturity states for doc claims.
var allMaturities = map[Maturity]struct{}{
	MaturityGeneralAvailability: {},
	MaturityExperimental:        {},
	MaturityPreview:             {},
	MaturityGated:               {},
	MaturityDegraded:            {},
	MaturityNotImplemented:      {},
}

// normalizeMaturity accepts the "ga" alias for general_availability and returns
// the canonical maturity value.
func normalizeMaturity(state string) Maturity {
	if strings.TrimSpace(state) == "ga" {
		return MaturityGeneralAvailability
	}
	return Maturity(strings.TrimSpace(state))
}

// ParseDocClaims scans every .md file under docsDir for capability-state markers
// and returns the parsed claims sorted by path then line for deterministic
// output. A marker missing id or state is returned with Malformed set so the
// freshness check surfaces it as a finding instead of dropping it. Markers
// inside fenced code blocks are skipped as illustrative examples.
func ParseDocClaims(docsDir string) ([]DocClaim, error) {
	var claims []DocClaim
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
		fileClaims, err := parseFileClaims(path, rel)
		if err != nil {
			return err
		}
		claims = append(claims, fileClaims...)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scan docs claims: %w", err)
	}
	sort.Slice(claims, func(i, j int) bool {
		if claims[i].Path != claims[j].Path {
			return claims[i].Path < claims[j].Path
		}
		return claims[i].Line < claims[j].Line
	})
	return claims, nil
}

func parseFileClaims(path, rel string) ([]DocClaim, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open doc %s: %w", path, err)
	}
	defer func() { _ = file.Close() }()

	var claims []DocClaim
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	line := 0
	inFence := false
	for scanner.Scan() {
		line++
		text := scanner.Text()
		// Skip fenced code blocks so illustrative markers in ``` or ~~~ examples
		// are not enforced as live claims.
		if trimmed := strings.TrimSpace(text); strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		matches := docClaimRE.FindAllStringSubmatch(text, -1)
		for _, match := range matches {
			claim := parseClaimFields(match[1])
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

// parseClaimFields parses the "id=... state=... issue=..." body of a marker. A
// marker missing its id or state is returned with Malformed set so the freshness
// check can surface it as a finding instead of silently dropping it.
func parseClaimFields(body string) DocClaim {
	var claim DocClaim
	for _, field := range strings.Fields(body) {
		key, value, found := strings.Cut(field, "=")
		if !found {
			continue
		}
		switch key {
		case "id":
			claim.Capability = value
		case "state":
			claim.State = normalizeMaturity(value)
		case "issue":
			if n, err := strconv.Atoi(value); err == nil {
				claim.Issue = n
			}
		}
	}
	if claim.Capability == "" || claim.State == "" {
		claim.Malformed = true
		claim.Marker = strings.TrimSpace(body)
	}
	return claim
}

// CheckDocFreshness compares parsed claims against the catalog and returns a
// finding for every claim that names an unknown capability, uses an invalid
// state, or contradicts the catalog maturity.
func CheckDocFreshness(catalog Catalog, claims []DocClaim) []DocFinding {
	byID := make(map[string]Entry, len(catalog.Entries))
	for _, entry := range catalog.Entries {
		byID[entry.Capability] = entry
	}

	var findings []DocFinding
	for _, claim := range claims {
		if claim.Malformed {
			findings = append(findings, DocFinding{
				Path: claim.Path, Line: claim.Line, Capability: claim.Marker,
				Reason: "malformed capability-state marker: missing id or state",
			})
			continue
		}
		entry, ok := byID[claim.Capability]
		if !ok {
			findings = append(findings, DocFinding{
				Path: claim.Path, Line: claim.Line, Capability: claim.Capability,
				Claimed: claim.State, Reason: "capability is not in the catalog",
			})
			continue
		}
		if _, valid := allMaturities[claim.State]; !valid {
			findings = append(findings, DocFinding{
				Path: claim.Path, Line: claim.Line, Capability: claim.Capability,
				Claimed: claim.State, Expected: entry.Maturity, Reason: "claimed state is not a valid maturity",
			})
			continue
		}
		if claim.State != entry.Maturity {
			findings = append(findings, DocFinding{
				Path: claim.Path, Line: claim.Line, Capability: claim.Capability,
				Claimed: claim.State, Expected: entry.Maturity, Reason: "doc claim contradicts catalog maturity",
			})
		}
	}
	return findings
}
