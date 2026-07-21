// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package environment

import (
	"fmt"
	"strings"
)

// Normalize trims whitespace and lowercases the raw environment string. This is
// the single normalization rule used across the platform.
func Normalize(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}

// Canonical returns the canonical environment name for raw after normalization
// and alias resolution. Known aliases (production→prod, staging→stage,
// development→dev) are mapped to their canonical form. Unknown values pass
// through normalized — they are never rejected and never invented.
func Canonical(raw string) string {
	normalized := Normalize(raw)
	if normalized == "" {
		return ""
	}
	if canonical, ok := aliasToCanonical[normalized]; ok {
		return canonical
	}
	return normalized
}

// IsKnownToken reports whether token is one of the 12 known environment tokens
// used for artifact-path token detection: prod, production, qa, stage, staging,
// uat, preprod, dev, development, test, sandbox, preview.
func IsKnownToken(token string) bool {
	return knownTokens[token]
}

// aliasToCanonical maps alias forms to their canonical environment name.
var aliasToCanonical = map[string]string{
	"production":  "prod",
	"staging":     "stage",
	"development": "dev",
}

// knownTokens is the 12-token union for artifact-path detection.
var knownTokens = map[string]bool{
	"prod":        true,
	"production":  true,
	"qa":          true,
	"stage":       true,
	"staging":     true,
	"uat":         true,
	"preprod":     true,
	"dev":         true,
	"development": true,
	"test":        true,
	"sandbox":     true,
	"preview":     true,
}

// allKnownTokens returns the 12-token slice for verification. Unexported;
// exposed to tests via TestIsKnownToken in the same package.
func allKnownTokens() []string {
	tokens := make([]string, 0, len(knownTokens))
	for token := range knownTokens {
		tokens = append(tokens, token)
	}
	return tokens
}

// EvidenceClass is a closed typed string enumerating environment evidence
// classes. Use ParseEvidenceClass to validate an arbitrary string.
type EvidenceClass string

// Evidence class constants.
const (
	// Existing producers (emitted today).
	EvidenceClassPathOverlay       EvidenceClass = "path_overlay"
	EvidenceClassNamespaceFallback EvidenceClass = "namespace_fallback"
	EvidenceClassArtifactPathToken EvidenceClass = "artifact_path_token"
	EvidenceClassCIObservation     EvidenceClass = "ci_observation"
	EvidenceClassCloudTag          EvidenceClass = "cloud_tag"
	EvidenceClassOperatorDeclared  EvidenceClass = "operator_declared"
	EvidenceClassHostnameInference EvidenceClass = "hostname_inference"

	// Defined for later wiring.
	EvidenceClassExplicitAliasConfig EvidenceClass = "explicit_alias_config"
	EvidenceClassArgoCDDestination   EvidenceClass = "argocd_destination"
	EvidenceClassNamespaceLabel      EvidenceClass = "namespace_label"
)

// validEvidenceClass is the set of valid EvidenceClass values.
var validEvidenceClass = map[EvidenceClass]bool{
	EvidenceClassPathOverlay:         true,
	EvidenceClassNamespaceFallback:   true,
	EvidenceClassArtifactPathToken:   true,
	EvidenceClassCIObservation:       true,
	EvidenceClassCloudTag:            true,
	EvidenceClassOperatorDeclared:    true,
	EvidenceClassHostnameInference:   true,
	EvidenceClassExplicitAliasConfig: true,
	EvidenceClassArgoCDDestination:   true,
	EvidenceClassNamespaceLabel:      true,
}

// ParseEvidenceClass validates and returns an EvidenceClass from a string.
func ParseEvidenceClass(s string) (EvidenceClass, error) {
	ec := EvidenceClass(s)
	if !validEvidenceClass[ec] {
		return "", fmt.Errorf("unknown evidence class: %q", s)
	}
	return ec, nil
}

// AllEvidenceClasses returns all valid EvidenceClass values.
func AllEvidenceClasses() []EvidenceClass {
	classes := make([]EvidenceClass, 0, len(validEvidenceClass))
	for c := range validEvidenceClass {
		classes = append(classes, c)
	}
	return classes
}

// AliasEntry pairs a canonical environment name with its accepted aliases.
type AliasEntry struct {
	Canonical string
	Aliases   []string
}

// Aliases returns the shared alias table: canonical names and their accepted
// aliases. Callers that need substring-based alias detection (e.g. hostname
// evidence) iterate the entries; callers that need exact-match canonicalization
// use Canonical.
func Aliases() []AliasEntry {
	return []AliasEntry{
		{Canonical: "prod", Aliases: []string{"prod", "production"}},
		{Canonical: "qa", Aliases: []string{"qa"}},
		{Canonical: "stage", Aliases: []string{"stage", "staging"}},
		{Canonical: "dev", Aliases: []string{"dev", "development"}},
		{Canonical: "test", Aliases: []string{"test"}},
		{Canonical: "sandbox", Aliases: []string{"sandbox"}},
		{Canonical: "preview", Aliases: []string{"preview"}},
	}
}

// State is a closed typed string for environment state vocabulary.
type State string

const (
	// StateBound indicates an environment is resolved and bound.
	StateBound State = "bound"
	// StateEnvironmentUnbound indicates an environment is unbound — no
	// evidence resolved an environment for the workload.
	StateEnvironmentUnbound State = "environment-unbound"
)
