// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package auditpreflight

import (
	"sort"
	"strings"
)

// GapClass classifies how a competitor finding relates to current Eshu state.
type GapClass string

const (
	// GapMissing means Eshu has no implementation of the capability.
	GapMissing GapClass = "missing"
	// GapFoundationExists means the foundation is implemented but not fully surfaced.
	GapFoundationExists GapClass = "foundation exists"
	// GapUIMissing means the capability exists but a UI surface is missing.
	GapUIMissing GapClass = "ui missing"
	// GapDocsStale means docs lag the implemented capability.
	GapDocsStale GapClass = "docs stale"
	// GapProofMissing means the capability lacks proof or evidence.
	GapProofMissing GapClass = "proof missing"
	// GapQualityGap means the capability exists but is below quality bar.
	GapQualityGap GapClass = "quality gap"
	// GapAlreadyTracked means an existing issue already covers the gap.
	GapAlreadyTracked GapClass = "already tracked"
)

// GapClasses is the closed set of valid gap classifications.
var GapClasses = []GapClass{
	GapMissing, GapFoundationExists, GapUIMissing, GapDocsStale,
	GapProofMissing, GapQualityGap, GapAlreadyTracked,
}

// OwnerSurface names the Eshu surface that owns a competitive gap.
type OwnerSurface string

// OwnerSurfaces is the closed set of valid owner surfaces.
var OwnerSurfaces = []OwnerSurface{
	"api", "mcp", "console", "docs", "collector", "parser",
	"reducer", "correlation", "search", "runtime", "governance",
}

// Field is one required preflight section: a stable key and the heading it is
// rendered under in the issue body.
type Field struct {
	Key     string
	Heading string
}

// RequiredFields are the preflight sections every competitive-audit issue must
// fill before it becomes work.
var RequiredFields = []Field{
	{"competitor_source", "Competitor source and local path"},
	{"code_evidence", "Eshu code evidence"},
	{"docs_evidence", "Eshu docs evidence"},
	{"proof_evidence", "Eshu test or proof evidence"},
	{"duplicate_search", "Existing issue duplicate search"},
	{"gap_class", "Gap class"},
	{"owner_surface", "Owner surface"},
	{"verification_plan", "Verification plan"},
}

// FindingKind classifies a preflight validation problem.
type FindingKind string

const (
	// FindingMissingField means a required section heading is absent.
	FindingMissingField FindingKind = "missing_field"
	// FindingEmptyField means a required section is present but empty.
	FindingEmptyField FindingKind = "empty_field"
	// FindingInvalidGapClass means the gap class is not in the taxonomy.
	FindingInvalidGapClass FindingKind = "invalid_gap_class"
	// FindingInvalidOwnerSurface means the owner surface is not in the taxonomy.
	FindingInvalidOwnerSurface FindingKind = "invalid_owner_surface"
)

// Finding is one preflight validation problem.
type Finding struct {
	Kind   FindingKind `json:"kind"`
	Field  string      `json:"field"`
	Detail string      `json:"detail"`
}

const noResponse = "_No response_"

// ParseIssue splits a GitHub issue body into its "### Heading" sections, keyed by
// the lowercased heading. Section values are trimmed; a "_No response_"
// placeholder is normalized to an empty string.
func ParseIssue(body string) map[string]string {
	sections := map[string]string{}
	var heading string
	var content []string
	flush := func() {
		if heading == "" {
			return
		}
		value := strings.TrimSpace(strings.Join(content, "\n"))
		if value == noResponse {
			value = ""
		}
		sections[strings.ToLower(heading)] = value
	}
	for _, line := range strings.Split(body, "\n") {
		if rest, ok := strings.CutPrefix(line, "### "); ok {
			flush()
			heading = strings.TrimSpace(rest)
			content = nil
			continue
		}
		content = append(content, line)
	}
	flush()
	return sections
}

// Validate checks an issue body against the preflight contract and returns the
// findings. An empty slice means the issue is preflight-complete.
func Validate(body string) []Finding {
	sections := ParseIssue(body)
	var findings []Finding
	for _, field := range RequiredFields {
		value, ok := sections[strings.ToLower(field.Heading)]
		if !ok {
			findings = append(findings, Finding{
				Kind: FindingMissingField, Field: field.Heading,
				Detail: "required preflight section is absent",
			})
			continue
		}
		if value == "" {
			findings = append(findings, Finding{
				Kind: FindingEmptyField, Field: field.Heading,
				Detail: "required preflight section is empty",
			})
			continue
		}
		switch field.Key {
		case "gap_class":
			if !validGapClass(value) {
				findings = append(findings, Finding{
					Kind: FindingInvalidGapClass, Field: field.Heading,
					Detail: "gap class must be one of: " + joinGapClasses(),
				})
			}
		case "owner_surface":
			if !validOwnerSurface(value) {
				findings = append(findings, Finding{
					Kind: FindingInvalidOwnerSurface, Field: field.Heading,
					Detail: "owner surface must be one of: " + joinOwnerSurfaces(),
				})
			}
		}
	}
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].Kind != findings[j].Kind {
			return findings[i].Kind < findings[j].Kind
		}
		return findings[i].Field < findings[j].Field
	})
	return findings
}

// normalize lowercases and collapses internal whitespace for taxonomy matching.
func normalize(value string) string {
	return strings.Join(strings.Fields(strings.ToLower(value)), " ")
}

func validGapClass(value string) bool {
	norm := normalize(value)
	for _, gap := range GapClasses {
		if norm == string(gap) {
			return true
		}
	}
	return false
}

func validOwnerSurface(value string) bool {
	norm := normalize(value)
	for _, surface := range OwnerSurfaces {
		if norm == string(surface) {
			return true
		}
	}
	return false
}

func joinGapClasses() string {
	parts := make([]string, len(GapClasses))
	for i, gap := range GapClasses {
		parts[i] = string(gap)
	}
	return strings.Join(parts, ", ")
}

func joinOwnerSurfaces() string {
	parts := make([]string, len(OwnerSurfaces))
	for i, surface := range OwnerSurfaces {
		parts[i] = string(surface)
	}
	return strings.Join(parts, ", ")
}
