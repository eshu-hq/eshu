package skillgen

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Capabilities is the per-deployment capability summary the skillgen
// reads from skill-fragments/capabilities.local.yaml. The file is
// gitignored; the default (all collectors enabled) is what a fresh
// contributor sees.
//
// v1 only models collector enablement. Future versions may grow
// capability- or profile-level knobs; the field shape is a struct
// (rather than a free-form map) so the contract is explicit.
type Capabilities struct {
	// Source is the path the capabilities were loaded from, or "default"
	// when the constructor returned a fully-enabled default set.
	Source string
	// Collectors maps a collector name to its enabled flag. A missing
	// collector name is treated as enabled (per the v1 contract: the
	// default is all enabled; the override disables a subset).
	Collectors map[string]bool
}

// defaultCollectors is the canonical collector name list S2 enumerates in
// the per-collector-matrix fragment. The list is intentionally short in
// v1; the S1 design says per-collector MCP tools are enumerated from the
// live capability catalog, not from a static prose list, and a future
// slice can swap this constant for a catalog-driven list.
var defaultCollectors = []string{
	"code",
	"terraform",
	"helm",
	"kustomize",
	"argo",
	"aws",
	"azure",
	"gcp",
	"kubernetes",
}

// DefaultCapabilities returns the fully-enabled default capability set.
func DefaultCapabilities() Capabilities {
	caps := Capabilities{Source: "default", Collectors: make(map[string]bool, len(defaultCollectors))}
	for _, name := range defaultCollectors {
		caps.Collectors[name] = true
	}
	return caps
}

// LoadCapabilities reads a capabilities file. A missing file returns the
// default set with Source set to "default" (not the path); this is the
// expected state for a contributor who has not configured per-deployment
// overrides. A present file with malformed YAML or a non-map
// `collectors` value returns an error.
func LoadCapabilities(path string) (Capabilities, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultCapabilities(), nil
		}
		return Capabilities{}, fmt.Errorf("read capabilities %s: %w", path, err)
	}
	return parseCapabilities(data, path)
}

// parseCapabilities decodes a capabilities YAML document. The schema is:
//
//	collectors:
//	  aws: false
//	  azure: true
//
// A `collectors` key missing from the document is treated as the default
// (all enabled) so a capabilities file that documents a deployment but
// has no overrides is still legal.
func parseCapabilities(data []byte, source string) (Capabilities, error) {
	if len(strings.TrimSpace(string(data))) == 0 {
		return DefaultCapabilities(), nil
	}
	var doc struct {
		Collectors map[string]bool `yaml:"collectors"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return Capabilities{}, fmt.Errorf("parse capabilities %s: %w", source, err)
	}
	caps := DefaultCapabilities()
	caps.Source = source
	if doc.Collectors == nil {
		return caps, nil
	}
	// Apply overrides in sorted name order so a missing collector is
	// treated as enabled and an explicit `false` flips it to disabled.
	names := make([]string, 0, len(doc.Collectors))
	for name := range doc.Collectors {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		enabled := doc.Collectors[name]
		caps.Collectors[name] = enabled
	}
	return caps, nil
}

// IsEnabled reports whether a named collector is enabled. Unknown names
// return true (the default) so the v1 contract is "the file disables
// a subset; everything else is on".
func (c Capabilities) IsEnabled(name string) bool {
	if c.Collectors == nil {
		return true
	}
	enabled, ok := c.Collectors[name]
	if !ok {
		return true
	}
	return enabled
}

// EnabledCollectors returns the sorted list of enabled collector names.
func (c Capabilities) EnabledCollectors() []string {
	var out []string
	for name, enabled := range c.Collectors {
		if enabled {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// DisabledCollectors returns the sorted list of disabled collector names.
// Collectors not present in the map are not in the disabled list; they
// fall back to the default (enabled) and are absent from the rendered
// "disabled" enumeration.
func (c Capabilities) DisabledCollectors() []string {
	var out []string
	for name, enabled := range c.Collectors {
		if !enabled {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}
