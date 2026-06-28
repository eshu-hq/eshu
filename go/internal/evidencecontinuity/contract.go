// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package evidencecontinuity

import (
	"fmt"
	"sort"
	"strings"
)

var requiredNegativeCases = []string{
	"empty_evidence",
	"missing_evidence",
	"stale_evidence",
	"truncated_evidence",
	"inaccessible_evidence",
}

// Contract is the evidence-continuity matrix document.
type Contract struct {
	Version              string              `yaml:"version"`
	RequiredDomains      []string            `yaml:"required_domains"`
	RequiredCapabilities []string            `yaml:"required_capabilities"`
	CapabilityAPIRoutes  map[string][]string `yaml:"capability_api_routes"`
	Rows                 []Row               `yaml:"rows"`
}

// Row declares one evidence-centric public capability continuity proof.
type Row struct {
	ID           string            `yaml:"id"`
	Domain       string            `yaml:"domain"`
	Capability   string            `yaml:"capability"`
	APIRoutes    []string          `yaml:"api_routes"`
	MCPTools     []string          `yaml:"mcp_tools"`
	SourceFact   ProofRef          `yaml:"source_fact"`
	Continuity   ContinuityProofs  `yaml:"continuity"`
	EmptyStates  EmptyStateProofs  `yaml:"empty_states"`
	NegativeCase []NegativeProof   `yaml:"negative_cases"`
	Properties   map[string]string `yaml:"properties"`
}

// ProofRef points to the test, gate, or golden assertion that proves a slice.
type ProofRef struct {
	Ref    string `yaml:"ref"`
	Detail string `yaml:"detail"`
}

// ContinuityProofs records proof across materialization and answer surfaces.
type ContinuityProofs struct {
	Projection ProofRef `yaml:"projection"`
	API        ProofRef `yaml:"api"`
	MCP        ProofRef `yaml:"mcp"`
}

// EmptyStateProofs records explicit unsupported, empty, and no-provider proof.
type EmptyStateProofs struct {
	NoProvider  ProofRef `yaml:"no_provider"`
	NoCollector ProofRef `yaml:"no_collector"`
	Empty       ProofRef `yaml:"empty"`
}

// NegativeProof records one closed evidence-loss negative case.
type NegativeProof struct {
	Case string `yaml:"case"`
	Ref  string `yaml:"ref"`
}

// SurfaceIndex contains the known public surfaces used for validation.
type SurfaceIndex struct {
	Capabilities        map[string]struct{}
	CapabilityAPIRoutes map[string]map[string]struct{}
	CapabilityMCPTools  map[string]map[string]struct{}
	APIRoutes           map[string]struct{}
	MCPTools            map[string]struct{}
}

// FindingKind is the stable verifier finding code.
type FindingKind string

const (
	FindingMissingRequiredDomain FindingKind = "missing_required_domain"
	FindingMissingRequiredCap    FindingKind = "missing_required_capability"
	FindingDuplicateRow          FindingKind = "duplicate_row"
	FindingUnknownCapability     FindingKind = "unknown_capability"
	FindingMissingAPIRoute       FindingKind = "missing_api_route"
	FindingUnknownAPIRoute       FindingKind = "unknown_api_route"
	FindingAPIRouteNotInCap      FindingKind = "api_route_not_in_capability"
	FindingMissingMCPTool        FindingKind = "missing_mcp_tool"
	FindingUnknownMCPTool        FindingKind = "unknown_mcp_tool"
	FindingMCPToolNotInCap       FindingKind = "mcp_tool_not_in_capability"
	FindingMissingSourceFact     FindingKind = "missing_source_fact_proof"
	FindingMissingContinuity     FindingKind = "missing_continuity_stage"
	FindingMissingEmptyState     FindingKind = "missing_empty_state_proof"
	FindingMissingNegative       FindingKind = "missing_negative_proof"
	FindingUnknownNegative       FindingKind = "unknown_negative_proof"
	FindingMissingProperty       FindingKind = "missing_property"
	FindingInvalidProperty       FindingKind = "invalid_property"
	FindingInvalidProofRef       FindingKind = "invalid_proof_ref"
)

// Finding is one validation error in the evidence-continuity contract.
type Finding struct {
	Kind    FindingKind
	RowID   string
	Message string
}

// Validate returns every contract finding. An empty slice means the matrix is
// complete against the supplied capability and surface indexes.
func Validate(contract Contract, surfaces SurfaceIndex) []Finding {
	var findings []Finding
	findings = append(findings, validateRequiredDomains(contract)...)
	findings = append(findings, validateRequiredCapabilities(contract)...)
	findings = append(findings, validateRows(contract.Rows, surfaces, contract.CapabilityAPIRoutes)...)
	sortFindings(findings)
	return findings
}

// FormatFindings renders findings in deterministic order for tests and scripts.
func FormatFindings(findings []Finding) string {
	if len(findings) == 0 {
		return ""
	}
	sortFindings(findings)
	lines := make([]string, 0, len(findings))
	for _, finding := range findings {
		lines = append(lines, fmt.Sprintf("%s %s: %s", finding.Kind, finding.RowID, finding.Message))
	}
	return strings.Join(lines, "\n")
}

func validateRequiredDomains(contract Contract) []Finding {
	seen := map[string]struct{}{}
	for _, row := range contract.Rows {
		seen[strings.TrimSpace(row.Domain)] = struct{}{}
	}
	var findings []Finding
	for _, domain := range contract.RequiredDomains {
		domain = strings.TrimSpace(domain)
		if domain == "" {
			continue
		}
		if _, ok := seen[domain]; !ok {
			findings = append(findings, Finding{
				Kind:    FindingMissingRequiredDomain,
				Message: fmt.Sprintf("required domain %q has no continuity row", domain),
			})
		}
	}
	return findings
}

func validateRequiredCapabilities(contract Contract) []Finding {
	seen := map[string]struct{}{}
	for _, row := range contract.Rows {
		seen[strings.TrimSpace(row.Capability)] = struct{}{}
	}
	var findings []Finding
	for _, capability := range contract.RequiredCapabilities {
		capability = strings.TrimSpace(capability)
		if capability == "" {
			continue
		}
		if _, ok := seen[capability]; !ok {
			findings = append(findings, Finding{
				Kind:    FindingMissingRequiredCap,
				RowID:   capability,
				Message: fmt.Sprintf("required capability %q has no continuity row", capability),
			})
		}
	}
	return findings
}

func validateRows(rows []Row, surfaces SurfaceIndex, capabilityAPIRoutes map[string][]string) []Finding {
	seenRows := map[string]struct{}{}
	surfaces.CapabilityAPIRoutes = mergeCapabilityAPIRoutes(surfaces.CapabilityAPIRoutes, capabilityAPIRoutes)
	var findings []Finding
	for _, row := range rows {
		id := strings.TrimSpace(row.ID)
		if _, ok := seenRows[id]; id != "" && ok {
			findings = append(findings, finding(row, FindingDuplicateRow, "duplicate row id"))
		}
		seenRows[id] = struct{}{}
		findings = append(findings, validateRow(row, surfaces)...)
	}
	return findings
}

func validateRow(row Row, surfaces SurfaceIndex) []Finding {
	var findings []Finding
	if !known(surfaces.Capabilities, row.Capability) {
		findings = append(findings, finding(row, FindingUnknownCapability, "unknown capability "+row.Capability))
	}
	findings = append(findings, validateAPIRoutes(row, surfaces)...)
	findings = append(findings, validateMCPTools(row, surfaces)...)
	if blank(row.SourceFact.Ref) {
		findings = append(findings, finding(row, FindingMissingSourceFact, "source fact proof is required"))
	} else if !proofLike(row.SourceFact.Ref) {
		findings = append(findings, finding(row, FindingInvalidProofRef, "source fact proof must point to a test, script, or golden assertion"))
	}
	findings = append(findings, validateProof(row, row.Continuity.Projection.Ref, FindingMissingContinuity, "projection/read-model proof")...)
	findings = append(findings, validateProof(row, row.Continuity.API.Ref, FindingMissingContinuity, "API answer proof")...)
	findings = append(findings, validateProof(row, row.Continuity.MCP.Ref, FindingMissingContinuity, "MCP answer proof")...)
	findings = append(findings, validateProof(row, row.EmptyStates.NoProvider.Ref, FindingMissingEmptyState, "no-provider proof")...)
	findings = append(findings, validateProof(row, row.EmptyStates.NoCollector.Ref, FindingMissingEmptyState, "no-collector proof")...)
	findings = append(findings, validateProof(row, row.EmptyStates.Empty.Ref, FindingMissingEmptyState, "empty-state proof")...)
	findings = append(findings, validateNegativeCases(row)...)
	findings = append(findings, validateProperties(row)...)
	return findings
}

func validateAPIRoutes(row Row, surfaces SurfaceIndex) []Finding {
	findings := validateKnownList(row, row.APIRoutes, surfaces.APIRoutes, FindingMissingAPIRoute, FindingUnknownAPIRoute, "api route")
	if len(row.APIRoutes) == 0 || surfaces.CapabilityAPIRoutes == nil {
		return findings
	}
	capabilityRoutes, ok := surfaces.CapabilityAPIRoutes[strings.TrimSpace(row.Capability)]
	if !ok {
		return append(findings, finding(row, FindingAPIRouteNotInCap, fmt.Sprintf("capability %q has no declared API routes", row.Capability)))
	}
	for _, route := range row.APIRoutes {
		route = strings.TrimSpace(route)
		if known(surfaces.APIRoutes, route) && !known(capabilityRoutes, route) {
			findings = append(findings, finding(row, FindingAPIRouteNotInCap, fmt.Sprintf("api route %q is not declared for capability %q", route, row.Capability)))
		}
	}
	return findings
}

func validateMCPTools(row Row, surfaces SurfaceIndex) []Finding {
	findings := validateKnownList(row, row.MCPTools, surfaces.MCPTools, FindingMissingMCPTool, FindingUnknownMCPTool, "mcp tool")
	if len(row.MCPTools) == 0 || surfaces.CapabilityMCPTools == nil {
		return findings
	}
	capabilityTools, ok := surfaces.CapabilityMCPTools[strings.TrimSpace(row.Capability)]
	if !ok {
		return findings
	}
	for _, tool := range row.MCPTools {
		tool = strings.TrimSpace(tool)
		if known(surfaces.MCPTools, tool) && !known(capabilityTools, tool) {
			findings = append(findings, finding(row, FindingMCPToolNotInCap, fmt.Sprintf("mcp tool %q is not declared for capability %q", tool, row.Capability)))
		}
	}
	return findings
}

func validateProperties(row Row) []Finding {
	expected := map[string]string{
		"provider_key_independent": "true",
		"deterministic_truth":      "required",
	}
	var findings []Finding
	for key, want := range expected {
		got := strings.TrimSpace(row.Properties[key])
		if got == "" {
			findings = append(findings, finding(row, FindingMissingProperty, key+" property is required"))
			continue
		}
		if got != want {
			findings = append(findings, finding(row, FindingInvalidProperty, fmt.Sprintf("%s must be %q, got %q", key, want, got)))
		}
	}
	return findings
}

func validateKnownList(row Row, values []string, knownValues map[string]struct{}, missing, unknown FindingKind, noun string) []Finding {
	if len(values) == 0 {
		return []Finding{finding(row, missing, noun+" is required")}
	}
	var findings []Finding
	for _, value := range values {
		if !known(knownValues, value) {
			findings = append(findings, finding(row, unknown, "unknown "+noun+" "+value))
		}
	}
	return findings
}

func validateProof(row Row, ref string, kind FindingKind, label string) []Finding {
	if blank(ref) {
		return []Finding{finding(row, kind, label+" is required")}
	}
	if !proofLike(ref) {
		return []Finding{finding(row, FindingInvalidProofRef, label+" must point to a test, script, or golden assertion")}
	}
	return nil
}

func validateNegativeCases(row Row) []Finding {
	got := map[string]string{}
	for _, proof := range row.NegativeCase {
		if blank(proof.Case) {
			continue
		}
		got[proof.Case] = strings.TrimSpace(proof.Ref)
	}
	allowed := map[string]struct{}{}
	var findings []Finding
	for _, name := range requiredNegativeCases {
		allowed[name] = struct{}{}
		if blank(got[name]) {
			findings = append(findings, finding(row, FindingMissingNegative, name+" negative proof is required"))
		} else if !proofLike(got[name]) {
			findings = append(findings, finding(row, FindingInvalidProofRef, name+" negative proof must point to a test, script, or golden assertion"))
		}
	}
	for name := range got {
		if _, ok := allowed[name]; !ok {
			findings = append(findings, finding(row, FindingUnknownNegative, "unknown negative case "+name))
		}
	}
	return findings
}

func proofLike(ref string) bool {
	ref = strings.TrimSpace(ref)
	lower := strings.ToLower(ref)
	if strings.HasPrefix(ref, "go test ") {
		_, tests, ok := parseGoTestRef(ref)
		return ok && len(tests) > 0
	}
	return strings.HasPrefix(ref, "scripts/") ||
		strings.HasPrefix(ref, "bash scripts/") ||
		strings.Contains(lower, "golden")
}

func parseGoTestRef(ref string) ([]string, []string, bool) {
	tokens := strings.Fields(ref)
	if len(tokens) < 4 || tokens[0] != "go" || tokens[1] != "test" {
		return nil, nil, false
	}
	var packages []string
	var run string
	for i := 2; i < len(tokens); i++ {
		token := strings.Trim(tokens[i], `"'`)
		switch {
		case token == "-run":
			i++
			if i >= len(tokens) {
				return nil, nil, false
			}
			run = strings.Trim(tokens[i], `"'`)
		case strings.HasPrefix(token, "-run="):
			run = strings.Trim(strings.TrimPrefix(token, "-run="), `"'`)
		case strings.HasPrefix(token, "-"):
			continue
		default:
			packages = append(packages, token)
		}
	}
	tests, ok := exactRunTests(run)
	if len(packages) == 0 || !ok {
		return nil, nil, false
	}
	return packages, tests, true
}

func exactRunTests(pattern string) ([]string, bool) {
	pattern = strings.Trim(strings.TrimSpace(pattern), `"'`)
	if pattern == "" {
		return nil, false
	}
	if strings.HasPrefix(pattern, "^(") && strings.HasSuffix(pattern, ")$") {
		pattern = strings.TrimSuffix(strings.TrimPrefix(pattern, "^("), ")$")
	} else if strings.HasPrefix(pattern, "^") && strings.HasSuffix(pattern, "$") {
		pattern = strings.TrimSuffix(strings.TrimPrefix(pattern, "^"), "$")
	} else {
		return nil, false
	}
	parts := strings.Split(pattern, "|")
	tests := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if !isExactTestName(part) {
			return nil, false
		}
		tests = append(tests, part)
	}
	return tests, len(tests) > 0
}

func isExactTestName(value string) bool {
	if !strings.HasPrefix(value, "Test") || len(value) == len("Test") {
		return false
	}
	for _, r := range value {
		if r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' {
			continue
		}
		return false
	}
	return true
}

func mergeCapabilityAPIRoutes(existing map[string]map[string]struct{}, routes map[string][]string) map[string]map[string]struct{} {
	if len(routes) == 0 {
		return existing
	}
	if existing == nil {
		existing = map[string]map[string]struct{}{}
	}
	for capability, values := range routes {
		capability = strings.TrimSpace(capability)
		if capability == "" {
			continue
		}
		if _, ok := existing[capability]; !ok {
			existing[capability] = map[string]struct{}{}
		}
		for _, route := range values {
			route = strings.TrimSpace(route)
			if route != "" {
				existing[capability][route] = struct{}{}
			}
		}
	}
	return existing
}

func known(values map[string]struct{}, key string) bool {
	_, ok := values[strings.TrimSpace(key)]
	return ok
}

func blank(value string) bool {
	return strings.TrimSpace(value) == ""
}

func finding(row Row, kind FindingKind, message string) Finding {
	return Finding{Kind: kind, RowID: row.ID, Message: message}
}

func sortFindings(findings []Finding) {
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].Kind != findings[j].Kind {
			return findings[i].Kind < findings[j].Kind
		}
		if findings[i].RowID != findings[j].RowID {
			return findings[i].RowID < findings[j].RowID
		}
		return findings[i].Message < findings[j].Message
	})
}
