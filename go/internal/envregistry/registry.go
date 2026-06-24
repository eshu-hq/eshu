// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package envregistry

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// VarType is the value type of a registered environment variable. It drives
// value validation in Validate.
type VarType string

const (
	// VarString is free-form text with no value validation.
	VarString VarType = "string"
	// VarInt must parse as a base-10 integer.
	VarInt VarType = "int"
	// VarBool must parse as a Go bool (strconv.ParseBool).
	VarBool VarType = "bool"
	// VarDuration must parse as a Go duration (time.ParseDuration).
	VarDuration VarType = "duration"
	// VarEnum must equal one of Entry.Allowed.
	VarEnum VarType = "enum"
	// VarDSN is a connection string; treated as free-form text for validation.
	VarDSN VarType = "dsn"
)

// Entry declares one supported ESHU_* environment variable.
type Entry struct {
	// Name is the canonical variable name (e.g. "ESHU_POSTGRES_DSN").
	Name string
	// Type drives value validation.
	Type VarType
	// Default is the documented default, or "" when unset/required.
	Default string
	// Subsystem is the owning subsystem (e.g. "postgres", "reducer").
	Subsystem string
	// Description is a one-line operator-facing summary.
	Description string
	// Allowed lists the valid values for VarEnum variables.
	Allowed []string
	// Aliases are alternative names whose value feeds the same setting
	// (including legacy non-ESHU_ names). Setting an alias is valid.
	Aliases []string
	// Deprecated marks a variable that still works but should be replaced.
	Deprecated bool
	// ReplacedBy names the preferred variable when Deprecated is true.
	ReplacedBy string
}

// FindingKind classifies a validation finding.
type FindingKind string

const (
	// FindingInvalidValue means a registered variable holds a value that does
	// not parse for its declared type. Always an error.
	FindingInvalidValue FindingKind = "invalid_value"
	// FindingDeprecated means a deprecated variable or alias is set. A warning.
	FindingDeprecated FindingKind = "deprecated"
	// FindingUnknown means an ESHU_* variable is not registered. Reported as a
	// warning, and only when it closely resembles a known variable (likely a
	// typo) or strict mode is requested, so legitimate out-of-scope variables
	// (e.g. collector variables) do not produce noise.
	FindingUnknown FindingKind = "unknown"
)

// Finding is one validation result for a single variable.
type Finding struct {
	Name    string
	Kind    FindingKind
	Message string
	// Error is true for findings that should fail `eshu config validate`.
	Error bool
}

// Registry is an immutable lookup over a set of entries, indexed by canonical
// name and by alias.
type Registry struct {
	entries  []Entry
	byName   map[string]*Entry
	byAlias  map[string]*Entry
	prefixes map[string]struct{}
}

// New builds a Registry from entries, rejecting duplicate names or aliases so a
// declaration mistake fails fast rather than silently shadowing.
func New(entries []Entry) (*Registry, error) {
	r := &Registry{
		byName:   make(map[string]*Entry, len(entries)),
		byAlias:  make(map[string]*Entry),
		prefixes: make(map[string]struct{}),
	}
	r.entries = make([]Entry, len(entries))
	copy(r.entries, entries)
	for i := range r.entries {
		e := &r.entries[i]
		if e.Name == "" {
			return nil, fmt.Errorf("envregistry: entry %d has empty name", i)
		}
		if _, dup := r.byName[e.Name]; dup {
			return nil, fmt.Errorf("envregistry: duplicate name %q", e.Name)
		}
		r.byName[e.Name] = e
		if prefix := subsystemPrefix(e.Name); prefix != "" {
			r.prefixes[prefix] = struct{}{}
		}
		for _, alias := range e.Aliases {
			if alias == "" {
				continue
			}
			if _, dup := r.byName[alias]; dup {
				return nil, fmt.Errorf("envregistry: alias %q collides with a canonical name", alias)
			}
			if existing, dup := r.byAlias[alias]; dup && existing.Name != e.Name {
				return nil, fmt.Errorf("envregistry: alias %q registered by both %q and %q", alias, existing.Name, e.Name)
			}
			r.byAlias[alias] = e
		}
	}
	return r, nil
}

// Entries returns the registered entries sorted by subsystem then name.
func (r *Registry) Entries() []Entry {
	out := make([]Entry, len(r.entries))
	copy(out, r.entries)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Subsystem != out[j].Subsystem {
			return out[i].Subsystem < out[j].Subsystem
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// Lookup resolves a variable by canonical name or alias.
func (r *Registry) Lookup(name string) (*Entry, bool) {
	if e, ok := r.byName[name]; ok {
		return e, true
	}
	if e, ok := r.byAlias[name]; ok {
		return e, true
	}
	return nil, false
}

// Covers reports whether name is a registered canonical name or alias.
func (r *Registry) Covers(name string) bool {
	_, ok := r.Lookup(name)
	return ok
}

// Validate checks an environment snapshot (variable name to value) and returns
// findings. Only ESHU_* names are considered. When strict is true, every
// unregistered ESHU_* name is reported as an unknown warning; otherwise unknown
// names are reported only when they closely resemble a registered name (a
// likely typo), so legitimate out-of-scope variables do not produce noise.
func (r *Registry) Validate(env map[string]string, strict bool) []Finding {
	names := make([]string, 0, len(env))
	for name := range env {
		if strings.HasPrefix(name, "ESHU_") {
			names = append(names, name)
		}
	}
	sort.Strings(names)

	var findings []Finding
	for _, name := range names {
		value := env[name]
		entry, known := r.Lookup(name)
		if !known {
			if finding, report := r.unknownFinding(name, strict); report {
				findings = append(findings, finding)
			}
			continue
		}
		if entry.Deprecated {
			replacement := entry.ReplacedBy
			if replacement == "" {
				replacement = entry.Name
			}
			findings = append(findings, Finding{
				Name:    name,
				Kind:    FindingDeprecated,
				Message: fmt.Sprintf("%s is deprecated; use %s", name, replacement),
			})
		}
		if msg, ok := validateValue(entry, value); !ok {
			findings = append(findings, Finding{
				Name:    name,
				Kind:    FindingInvalidValue,
				Message: msg,
				Error:   true,
			})
		}
	}
	return findings
}

// unknownFinding decides whether an unregistered ESHU_* name should be
// reported. In strict mode every unknown name is reported. Otherwise it is
// reported only when it closely resembles a registered name (likely a typo).
func (r *Registry) unknownFinding(name string, strict bool) (Finding, bool) {
	if suggestion, ok := r.closestName(name); ok {
		return Finding{
			Name:    name,
			Kind:    FindingUnknown,
			Message: fmt.Sprintf("%s is not a registered variable; did you mean %s?", name, suggestion),
		}, true
	}
	if strict {
		return Finding{
			Name:    name,
			Kind:    FindingUnknown,
			Message: fmt.Sprintf("%s is not a registered variable", name),
		}, true
	}
	return Finding{}, false
}

// closestName returns a registered name within a small edit distance of the
// given name, restricted to names sharing the same subsystem prefix so a
// suggestion is only offered when it is plausibly a typo of a core variable.
func (r *Registry) closestName(name string) (string, bool) {
	prefix := subsystemPrefix(name)
	if prefix == "" {
		return "", false
	}
	if _, ok := r.prefixes[prefix]; !ok {
		return "", false
	}
	best := ""
	bestDist := 3 // require distance < 3 to suggest
	for i := range r.entries {
		candidate := r.entries[i].Name
		if subsystemPrefix(candidate) != prefix {
			continue
		}
		if d := levenshtein(name, candidate); d < bestDist {
			bestDist = d
			best = candidate
		}
	}
	return best, best != ""
}

// validateValue checks a value against an entry's type, returning a human
// message when invalid.
func validateValue(entry *Entry, value string) (string, bool) {
	if value == "" {
		return "", true
	}
	switch entry.Type {
	case VarInt:
		if _, err := strconv.Atoi(strings.TrimSpace(value)); err != nil {
			return fmt.Sprintf("%s=%q is not a valid integer", entry.Name, value), false
		}
	case VarBool:
		if _, err := strconv.ParseBool(strings.TrimSpace(value)); err != nil {
			return fmt.Sprintf("%s=%q is not a valid bool", entry.Name, value), false
		}
	case VarDuration:
		if _, err := time.ParseDuration(strings.TrimSpace(value)); err != nil {
			return fmt.Sprintf("%s=%q is not a valid duration", entry.Name, value), false
		}
	case VarEnum:
		for _, allowed := range entry.Allowed {
			if value == allowed {
				return "", true
			}
		}
		return fmt.Sprintf("%s=%q is not one of %s", entry.Name, value, strings.Join(entry.Allowed, ", ")), false
	case VarString, VarDSN:
		// No structural validation; any non-empty value is accepted.
	}
	return "", true
}

// subsystemPrefix returns the "ESHU_<TOKEN>" prefix of a name, used to scope
// typo suggestions to the same subsystem family.
func subsystemPrefix(name string) string {
	if !strings.HasPrefix(name, "ESHU_") {
		return ""
	}
	rest := strings.TrimPrefix(name, "ESHU_")
	idx := strings.IndexByte(rest, '_')
	if idx <= 0 {
		return name
	}
	return "ESHU_" + rest[:idx]
}

// levenshtein returns the edit distance between two strings. It is used only
// for small, bounded variable names, so the simple O(n*m) form is fine.
func levenshtein(a, b string) int {
	if a == b {
		return 0
	}
	la, lb := len(a), len(b)
	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}
	prev := make([]int, lb+1)
	curr := make([]int, lb+1)
	for j := 0; j <= lb; j++ {
		prev[j] = j
	}
	for i := 1; i <= la; i++ {
		curr[0] = i
		for j := 1; j <= lb; j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			curr[j] = min3(curr[j-1]+1, prev[j]+1, prev[j-1]+cost)
		}
		prev, curr = curr, prev
	}
	return prev[lb]
}

func min3(a, b, c int) int {
	m := a
	if b < m {
		m = b
	}
	if c < m {
		m = c
	}
	return m
}
