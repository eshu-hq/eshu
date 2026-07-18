// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package queryplan

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	queryKindCypher       = "cypher"
	queryKindSQLReadModel = "sql_read_model"
)

var (
	createSchemaNamePattern = regexp.MustCompile(`(?i)\bCREATE\s+(?:CONSTRAINT|INDEX)\s+([A-Za-z_][A-Za-z0-9_]*)\b`)
	unlabeledMatchPattern   = regexp.MustCompile(`(?i)\bMATCH\s*\([A-Za-z_][A-Za-z0-9_]*\s*(?:\)|\{)`)
	unboundedStarPattern    = regexp.MustCompile(`\[[^\]]*\*\s*\]`)
	openRangeStarPattern    = regexp.MustCompile(`\[[^\]]*\*\s*\d+\s*\.\.\s*\]`)
)

// Manifest describes the set of hot paths that a query-plan regression gate
// must validate.
type Manifest struct {
	Version        int              `yaml:"version"`
	RequiredIDs    []string         `yaml:"required_ids"`
	Entries        []Entry          `yaml:"entries"`
	SourceCoverage []SourceCoverage `yaml:"source_coverage"`
}

// Entry describes one hot query or read model in the query-plan gate.
type Entry struct {
	ID              string          `yaml:"id"`
	Domain          string          `yaml:"domain"`
	Backend         string          `yaml:"backend"`
	QueryKind       string          `yaml:"query_kind"`
	Source          SourceRef       `yaml:"source"`
	Cypher          string          `yaml:"cypher,omitempty"`
	CypherSHA256    string          `yaml:"cypher_sha256,omitempty"`
	QueryFragment   string          `yaml:"query_fragment,omitempty"`
	RequiredAnchors []Anchor        `yaml:"required_anchors,omitempty"`
	RequiredSchema  []string        `yaml:"required_schema,omitempty"`
	RequiredLimits  []string        `yaml:"required_limits,omitempty"`
	RequiresOrder   bool            `yaml:"requires_order,omitempty"`
	AllowUnlabeled  bool            `yaml:"allow_unlabeled,omitempty"`
	Plan            PlanExpectation `yaml:"plan,omitempty"`
	Caveats         []string        `yaml:"caveats,omitempty"`
}

// SourceRef points a manifest entry at the production source that owns it.
type SourceRef struct {
	File     string `yaml:"file"`
	Symbol   string `yaml:"symbol"`
	LineHint int    `yaml:"line_hint,omitempty"`
}

// Anchor declares the label and property expected to bound a hot Cypher query.
type Anchor struct {
	Label    string `yaml:"label"`
	Property string `yaml:"property"`
}

// PlanExpectation captures a small backend plan fixture summary.
type PlanExpectation struct {
	Operators          []string `yaml:"operators,omitempty"`
	ForbiddenOperators []string `yaml:"forbidden_operators,omitempty"`
}

// LoadManifestFile decodes a query-plan gate manifest from disk.
func LoadManifestFile(path string) (Manifest, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- path is an internal CLI flag or test literal pointing to a local manifest file, not user-supplied HTTP input
	if err != nil {
		return Manifest{}, err
	}
	var manifest Manifest
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

// ValidateManifest validates hot path entries against static query-plan and
// schema evidence rules.
func ValidateManifest(manifest Manifest, schemaStatements []string) error {
	var violations []string
	if manifest.Version != 1 {
		violations = append(violations, fmt.Sprintf("unsupported manifest version %d", manifest.Version))
	}

	entriesByID := make(map[string]Entry, len(manifest.Entries))
	for _, entry := range manifest.Entries {
		if entry.ID == "" {
			violations = append(violations, "entry missing id")
			continue
		}
		if _, exists := entriesByID[entry.ID]; exists {
			violations = append(violations, fmt.Sprintf("duplicate hot path %s", entry.ID))
			continue
		}
		entriesByID[entry.ID] = entry
	}
	for _, id := range manifest.RequiredIDs {
		if _, exists := entriesByID[id]; !exists {
			violations = append(violations, fmt.Sprintf("missing required hot path %s", id))
		}
	}

	schemaNames := schemaNames(schemaStatements)
	for _, entry := range manifest.Entries {
		violations = append(violations, validateEntry(entry, schemaNames)...)
	}

	if len(violations) > 0 {
		return errors.New(strings.Join(violations, "; "))
	}
	return nil
}

func validateEntry(entry Entry, schemaNames map[string]struct{}) []string {
	var violations []string
	prefix := entry.ID
	if prefix == "" {
		prefix = "<missing-id>"
	}
	if strings.TrimSpace(entry.Domain) == "" {
		violations = append(violations, fmt.Sprintf("%s: missing domain", prefix))
	}
	if strings.TrimSpace(entry.Backend) == "" {
		violations = append(violations, fmt.Sprintf("%s: missing backend", prefix))
	}
	if strings.TrimSpace(entry.Source.File) == "" || strings.TrimSpace(entry.Source.Symbol) == "" {
		violations = append(violations, fmt.Sprintf("%s: missing source file or symbol", prefix))
	}

	switch entry.QueryKind {
	case queryKindCypher:
		violations = append(violations, validateCypherEntry(entry)...)
	case queryKindSQLReadModel:
		if len(entry.Caveats) == 0 {
			violations = append(violations, fmt.Sprintf("%s: sql_read_model entries require a backend caveat", prefix))
		}
	default:
		violations = append(violations, fmt.Sprintf("%s: unsupported query_kind %q", prefix, entry.QueryKind))
	}

	for _, schemaName := range entry.RequiredSchema {
		if _, exists := schemaNames[schemaName]; !exists {
			violations = append(violations, fmt.Sprintf("%s: missing schema evidence %s", prefix, schemaName))
		}
	}
	violations = append(violations, validatePlan(entry)...)
	return violations
}

func validateCypherEntry(entry Entry) []string {
	var violations []string
	prefix := entry.ID
	cypher := normalizeCypher(entry.Cypher)
	if cypher == "" {
		return append(violations, fmt.Sprintf("%s: cypher entries require cypher text", prefix))
	}

	if unboundedStarPattern.MatchString(cypher) || openRangeStarPattern.MatchString(cypher) {
		violations = append(violations, fmt.Sprintf("%s: unbounded variable-length traversal", prefix))
	}
	if !entry.AllowUnlabeled && unlabeledMatchPattern.MatchString(cypher) {
		violations = append(violations, fmt.Sprintf("%s: unlabeled MATCH anchor", prefix))
	}
	if containsToken(cypher, "SKIP") && !hasOrderBeforeSkip(cypher) {
		violations = append(violations, fmt.Sprintf("%s: SKIP without ORDER BY", prefix))
	}
	if entry.RequiresOrder && !containsPhrase(cypher, "ORDER BY") {
		violations = append(violations, fmt.Sprintf("%s: missing required ORDER BY", prefix))
	}
	for _, limit := range entry.RequiredLimits {
		if !containsPhrase(cypher, "LIMIT "+strings.TrimSpace(limit)) {
			violations = append(violations, fmt.Sprintf("%s: missing required LIMIT %s", prefix, limit))
		}
	}
	for _, anchor := range entry.RequiredAnchors {
		violations = append(violations, validateAnchor(prefix, cypher, anchor)...)
	}
	return violations
}

func validateAnchor(prefix, cypher string, anchor Anchor) []string {
	var violations []string
	if anchor.Label == "" || anchor.Property == "" {
		return append(violations, fmt.Sprintf("%s: required anchor missing label or property", prefix))
	}
	labelNeedle := ":" + anchor.Label
	propertyMapNeedle := anchor.Property + ":"
	propertyAccessNeedle := "." + anchor.Property
	if !strings.Contains(cypher, labelNeedle) {
		violations = append(violations, fmt.Sprintf("%s: missing anchor label %s", prefix, anchor.Label))
	}
	if !strings.Contains(cypher, propertyMapNeedle) && !strings.Contains(cypher, propertyAccessNeedle) {
		violations = append(violations, fmt.Sprintf("%s: missing anchor property %s.%s", prefix, anchor.Label, anchor.Property))
	}
	return violations
}

func validatePlan(entry Entry) []string {
	var violations []string
	if len(entry.Plan.Operators) == 0 || len(entry.Plan.ForbiddenOperators) == 0 {
		return violations
	}
	seen := make(map[string]struct{}, len(entry.Plan.Operators))
	for _, operator := range entry.Plan.Operators {
		seen[strings.TrimSpace(operator)] = struct{}{}
	}
	for _, forbidden := range entry.Plan.ForbiddenOperators {
		forbidden = strings.TrimSpace(forbidden)
		if _, exists := seen[forbidden]; exists {
			violations = append(violations, fmt.Sprintf("%s: forbidden plan operator %s", entry.ID, forbidden))
		}
	}
	return violations
}

func schemaNames(statements []string) map[string]struct{} {
	names := make(map[string]struct{}, len(statements))
	for _, stmt := range statements {
		matches := createSchemaNamePattern.FindStringSubmatch(stmt)
		if len(matches) == 2 {
			names[matches[1]] = struct{}{}
		}
	}
	return names
}

func normalizeCypher(cypher string) string {
	return strings.Join(strings.Fields(cypher), " ")
}

func containsToken(text, token string) bool {
	return strings.Contains(strings.ToUpper(text), strings.ToUpper(token))
}

func containsPhrase(text, phrase string) bool {
	return strings.Contains(strings.ToUpper(text), strings.ToUpper(phrase))
}

func hasOrderBeforeSkip(cypher string) bool {
	upper := strings.ToUpper(cypher)
	skipIndex := strings.Index(upper, "SKIP")
	orderIndex := strings.Index(upper, "ORDER BY")
	return skipIndex >= 0 && orderIndex >= 0 && orderIndex < skipIndex
}
