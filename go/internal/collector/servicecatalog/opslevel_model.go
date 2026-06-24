// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package servicecatalog

import "strings"

// opslevelDocument is the typed projection of one opslevel.yml document. OpsLevel
// declares a top-level version plus a single component (or the deprecated
// service) block per document. Only the fields the producer maps into facts are
// modeled.
type opslevelDocument struct {
	Version   any                `yaml:"version"`
	Component *opslevelComponent `yaml:"component"`
	Service   *opslevelComponent `yaml:"service"`
}

// opslevelComponent models the shared shape of the OpsLevel component and the
// deprecated service block. OpsLevel uses YAML 1.1, so string fields may decode
// from quoted or bare scalars; only declared keys are read.
type opslevelComponent struct {
	Name         string               `yaml:"name"`
	Type         string               `yaml:"type"`
	Description  string               `yaml:"description"`
	Lifecycle    string               `yaml:"lifecycle"`
	Tier         string               `yaml:"tier"`
	Owner        string               `yaml:"owner"`
	System       string               `yaml:"system"`
	Product      string               `yaml:"product"`
	Language     string               `yaml:"language"`
	Framework    string               `yaml:"framework"`
	Aliases      []string             `yaml:"aliases"`
	Repositories []opslevelRepository `yaml:"repositories"`
	Dependencies []opslevelDependency `yaml:"dependencies"`
	Tools        []opslevelTool       `yaml:"tools"`
}

// opslevelRepository models one repositories entry. OpsLevel references repos by
// provider plus a name slug (Org/Group/repo), never a full URL.
type opslevelRepository struct {
	Name        string `yaml:"name"`
	Provider    string `yaml:"provider"`
	Path        string `yaml:"path"`
	DisplayName string `yaml:"display_name"`
}

// opslevelDependency models one dependencies entry. OpsLevel identifies the
// target by an alias.
type opslevelDependency struct {
	Alias string `yaml:"alias"`
	Notes string `yaml:"notes"`
}

// opslevelTool models one tools entry: an operational link with a category, a
// URL, and an optional display name and environment.
type opslevelTool struct {
	Name        string `yaml:"name"`
	DisplayName string `yaml:"displayName"`
	Category    string `yaml:"category"`
	URL         string `yaml:"url"`
	Environment string `yaml:"environment"`
}

// opslevelComponentKind is the default entity kind used when a component omits an
// explicit type; OpsLevel components without a type are treated as components.
const opslevelComponentKind = "component"

// supportedOpsLevelVersions enumerates the opslevel.yml schema versions the
// producer fully understands. Other versions still emit a minimally-parsed
// entity plus an unsupported_descriptor_version warning rather than a silent
// drop.
var supportedOpsLevelVersions = map[string]bool{
	"1": true,
}

// opslevelProviderHosts maps a known OpsLevel git provider to its canonical
// public host. A repository declared with a known provider expands into a
// derivable URL; an unknown provider stays a name-only slug the reducer rejects.
// Self-hosted providers (for example a private GitHub Enterprise host) have no
// canonical public host here, so they intentionally stay name-only rather than
// risk a wrong host.
var opslevelProviderHosts = map[string]string{
	"github":       "github.com",
	"gitlab":       "gitlab.com",
	"bitbucket":    "bitbucket.org",
	"azure_devops": "dev.azure.com",
}

// component returns the declared component, falling back to the deprecated
// service block when present.
func (d opslevelDocument) component() *opslevelComponent {
	if d.Component != nil {
		return d.Component
	}
	return d.Service
}

// version returns the declared schema version as a trimmed string. OpsLevel
// allows integer or string scalars, so both decode shapes normalize here.
func (d opslevelDocument) version() string {
	switch value := d.Version.(type) {
	case string:
		return strings.TrimSpace(value)
	case int:
		return strings.TrimSpace(itoa(value))
	case int64:
		return strings.TrimSpace(itoa(int(value)))
	case float64:
		// YAML rarely decodes version as float, but guard the path so a 1.0
		// scalar still normalizes to "1" rather than a silent mismatch.
		if value == float64(int(value)) {
			return itoa(int(value))
		}
	}
	return ""
}

// entityRef returns the canonical `component:opslevel/name` reference for one
// OpsLevel component, or an empty string when no stable name or alias yields a
// non-empty slug. The OpsLevel block is always a component (its free-form
// `type`, e.g. service or database, flows into entity_type, not the ref kind,
// mirroring how Backstage uses kind for the ref and spec.type for entity_type).
// OpsLevel aliases are the stable user-facing identifiers, so the first declared
// alias that slugifies to a non-empty segment wins; otherwise the component name
// is slugified. The opslevel namespace segment keeps the ref distinct from a
// Backstage `component:default/...` ref so the reducer never merges two
// providers' entities under one key.
//
// primaryAnchor already rejects an anchor that slugifies to an empty string (for
// example a punctuation-only "---"), so a non-empty anchor here always slugifies
// to a non-empty segment. Emitting "component:opslevel/" with an empty slug would
// collide across every such entity and break reducer correlation; the empty
// anchor instead falls through to the caller's invalid_ref warning.
func (c opslevelComponent) entityRef() string {
	anchor := c.primaryAnchor()
	if anchor == "" {
		return ""
	}
	return opslevelComponentKind + ":" + ProviderOpsLevelNamespace + "/" + slugify(anchor)
}

// ProviderOpsLevelNamespace is the entity-ref namespace segment for OpsLevel
// components. It mirrors the provider name and keeps OpsLevel refs distinct from
// other providers' refs in the shared reducer entity key.
const ProviderOpsLevelNamespace = "opslevel"

// primaryAnchor returns the stable identifier OpsLevel resolution keys on: the
// first alias that slugifies to a non-empty segment, else the component name.
// An alias composed entirely of punctuation (for example "---") slugifies to
// "", carries no usable identity, and is skipped so a junk alias never shadows a
// usable component name. The returned string is the original (non-slugified)
// anchor; callers slugify it. The result is empty only when neither any alias
// nor the name yields a non-empty slug, which the caller surfaces as
// invalid_ref rather than admitting a junk "component:opslevel/" reference.
func (c opslevelComponent) primaryAnchor() string {
	for _, alias := range c.Aliases {
		trimmed := strings.TrimSpace(alias)
		if trimmed != "" && slugify(trimmed) != "" {
			return trimmed
		}
	}
	if name := strings.TrimSpace(c.Name); name != "" && slugify(name) != "" {
		return name
	}
	return ""
}

// entityType returns the declared component type verbatim.
func (c opslevelComponent) entityType() string {
	return strings.TrimSpace(c.Type)
}

// ownerRef returns the OpsLevel owner alias verbatim, preserving the exact
// identifier as provenance. The reducer records the full reference; collapsing or
// rewriting it would break owner filters and reporting.
func (c opslevelComponent) ownerRef() string {
	return strings.TrimSpace(c.Owner)
}

// repositoryLocator resolves the component's first usable repository entry into a
// derived URL (known provider + slug) or a name-only slug (unknown/self-hosted
// provider or missing provider). It never fabricates a repository_id. The first
// repository entry that yields any locator wins, matching OpsLevel's primary
// repository semantics.
func (c opslevelComponent) repositoryLocator() (repoURL string, repoName string) {
	for _, repo := range c.Repositories {
		name := strings.TrimSpace(repo.Name)
		if name == "" {
			continue
		}
		if url := opslevelRepositoryURL(repo.Provider, name); url != "" {
			return url, ""
		}
		// Known provider host but malformed slug, or unknown provider: emit the
		// declared slug as a name-only locator the reducer rejects.
		return "", name
	}
	return "", ""
}

// dependencies returns the declared dependency aliases.
func (c opslevelComponent) dependencies() []string {
	out := make([]string, 0, len(c.Dependencies))
	for _, dep := range c.Dependencies {
		alias := strings.TrimSpace(dep.Alias)
		if alias != "" {
			out = append(out, alias)
		}
	}
	return out
}

// opslevelRepositoryURL expands a known OpsLevel provider plus a `Org/.../repo`
// slug into a canonical public https URL. It returns an empty string for an
// unknown or self-hosted provider, or a slug that is not a path (no `/`), so the
// caller falls back to a name-only locator instead of guessing a wrong URL.
func opslevelRepositoryURL(provider, name string) string {
	host, ok := opslevelProviderHosts[strings.ToLower(strings.TrimSpace(provider))]
	if !ok {
		return ""
	}
	slug := strings.Trim(strings.TrimSpace(name), "/")
	if slug == "" || !strings.Contains(slug, "/") {
		return ""
	}
	candidate := "https://" + host + "/" + slug
	if !isSafeURL(candidate) {
		return ""
	}
	return candidate
}

// slugify lowercases and hyphenates a free-form name into a stable entity-ref
// segment. It collapses any run of non-alphanumeric characters into a single
// hyphen and trims leading/trailing hyphens, so "Checkout API" becomes
// "checkout-api" deterministically.
func slugify(value string) string {
	var b strings.Builder
	lastHyphen := false
	for _, r := range strings.ToLower(strings.TrimSpace(value)) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			lastHyphen = false
		default:
			if !lastHyphen && b.Len() > 0 {
				b.WriteByte('-')
				lastHyphen = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

// itoa is a tiny stdlib-free integer formatter used only for normalizing the
// declared version scalar. It avoids importing strconv for a single call site.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	negative := n < 0
	if negative {
		n = -n
	}
	var digits [20]byte
	i := len(digits)
	for n > 0 {
		i--
		digits[i] = byte('0' + n%10)
		n /= 10
	}
	if negative {
		i--
		digits[i] = '-'
	}
	return string(digits[i:])
}
