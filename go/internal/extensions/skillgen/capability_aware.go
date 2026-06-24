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

// DefaultCapabilities returns the fully-enabled default capability set
// from the on-disk catalog at DefaultCatalogPath. The function is
// fail-closed: a missing or malformed catalog is an error rather than a
// fallback to a static subset, because the S1 design says per-collector
// MCP tools are enumerated from the live capability catalog, not from a
// static prose list, and a partial list would teach agents an
// incomplete surface.
func DefaultCapabilities() Capabilities {
	names, err := LoadDefaultCatalog()
	if err != nil {
		panic(fmt.Sprintf("skillgen: %v (this is a build-time tool; the catalog must be readable from the working directory)", err))
	}
	return DefaultCapabilitiesFor(names)
}

// DefaultCapabilitiesFor wraps a sorted collector list as a fully-enabled
// Capabilities. The Source field is set to "test" so production code
// (which always supplies the on-disk catalog) is distinguishable from
// test-only constructions.
func DefaultCapabilitiesFor(names []string) Capabilities {
	caps := Capabilities{Source: "test", Collectors: make(map[string]bool, len(names))}
	for _, name := range names {
		caps.Collectors[name] = true
	}
	return caps
}

// LoadCapabilities reads a capabilities override file and merges it with
// the on-disk catalog. The override file is gitignored and per-deployment;
// a missing override file is the default (the catalog is the source of
// truth). A present file with malformed YAML or a non-map `collectors`
// value returns an error.
func LoadCapabilities(overridePath, catalogPath string) (Capabilities, error) {
	names, err := loadCatalogFrom(catalogPath)
	if err != nil {
		return Capabilities{}, err
	}
	caps := DefaultCapabilitiesFor(names)
	caps.Source = catalogPath

	data, err := os.ReadFile(overridePath)
	if err != nil {
		if os.IsNotExist(err) {
			return caps, nil
		}
		return Capabilities{}, fmt.Errorf("read capabilities %s: %w", overridePath, err)
	}
	return parseOverride(data, overridePath, caps)
}

// parseOverride applies an override file's `collectors` map on top of the
// fully-enabled default set built from the catalog. A missing `collectors`
// key in the document is treated as no overrides so a capabilities file
// that documents a deployment but has no per-collector toggles is still
// legal.
func parseOverride(data []byte, source string, caps Capabilities) (Capabilities, error) {
	// The override file is present (LoadCapabilities verified this) so
	// Source always tracks the override path, not the catalog path. A
	// blank file means "no overrides"; the catalog-derived collectors
	// remain the source of truth.
	caps.Source = source
	if len(strings.TrimSpace(string(data))) == 0 {
		return caps, nil
	}
	var doc struct {
		Collectors map[string]bool `yaml:"collectors"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return Capabilities{}, fmt.Errorf("parse capabilities %s: %w", source, err)
	}
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
