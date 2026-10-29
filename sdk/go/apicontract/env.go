// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package apicontract

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
	// warning, and only when it closely resembles a known name (likely a typo)
	// or strict mode is requested, so legitimate out-of-scope variables do not
	// produce noise.
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

// Registry is the code-owned source of truth for an ESHU_* variable table.
// The core envregistry.Registry is the full platform table; this type is the
// identical mechanism scoped to the published client table from
// ClientEntries, so a thin client validates exactly the variables it owns.
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
			return nil, fmt.Errorf("apicontract: entry %d has empty name", i)
		}
		if _, dup := r.byName[e.Name]; dup {
			return nil, fmt.Errorf("apicontract: duplicate name %q", e.Name)
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
				return nil, fmt.Errorf("apicontract: alias %q collides with a canonical name", alias)
			}
			if existing, dup := r.byAlias[alias]; dup && existing.Name != e.Name {
				return nil, fmt.Errorf("apicontract: alias %q registered by both %q and %q", alias, existing.Name, e.Name)
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
// names are reported only when they closely resemble a registered name (likely
// a typo), so legitimate out-of-scope variables do not produce noise.
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
// reported only when it closely resembles a registered name (a likely typo).
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
// suggestion is only offered when it is plausibly a typo of a client variable.
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

// clientEntries is the published client variable table: the ESHU_* names a
// thin CLI reads or writes outside local-owner paths, with field values
// identical to the core envregistry entries they mirror. The core drift test
// fails when the registry moves one of these names; add the renamed entry
// here in the same change. Variables the thin client reads that the core
// registry does not cover yet (ESHU_SERVICE_URL, ESHU_REMOTE_TIMEOUT_SECONDS)
// stay out until the registry owns them.
var clientEntries = []Entry{
	// Kept in Registry.Entries order (subsystem, then name) so the table
	// round-trips through New(...).Entries() unchanged.
	{Name: "ESHU_API_KEY", Type: VarString, Subsystem: "api", Description: "Bearer token for API authentication. Setting it (or `ESHU_SCOPED_TOKENS_FILE`/`ESHU_AUTH_RESOURCE_URI`) also enables read enforcement: headerless requests to non-public routes are rejected with 401. With none of the three set the read surface is open (local/demo dev-mode)."},
	{Name: "ESHU_HOME", Type: VarString, Subsystem: "api", Description: "Root directory for persisted API key and configuration (defaults to ~/.eshu)."},
	{Name: "ESHU_QUERY_PROFILE", Type: VarEnum, Default: "production", Subsystem: "api", Allowed: []string{"production", "local_authoritative", "local_lightweight"}, Description: "Query execution profile."},
	{Name: "ESHU_GRAPH_BACKEND", Type: VarEnum, Default: "nornicdb", Subsystem: "graph", Allowed: []string{"neo4j", "nornicdb"}, Description: "Graph database backend."},
	{Name: "ESHU_NEO4J_DATABASE", Type: VarString, Subsystem: "graph", Aliases: []string{"NEO4J_DATABASE"}, Description: "Graph backend database name; defaults to neo4j for neo4j and nornic for nornicdb."},
	{Name: "ESHU_MCP_ALLOW_UNAUTHENTICATED", Type: VarBool, Default: "false", Subsystem: "mcp", Description: "Dev/loopback escape hatch (issue #5168): when true, ESHU_MCP_TRANSPORT=http is allowed to start with no resolvable credential source (no ESHU_API_KEY, no ESHU_SCOPED_TOKENS_FILE, no ESHU_AUTH_RESOURCE_URI). Without it, that configuration refuses to start. Every initialize/tools/list/tools/call/ping request and SSE session is unauthenticated in that state; never set this on a publicly reachable port."},
	{Name: "ESHU_MCP_RESULT_SHAPE", Type: VarEnum, Default: "dual", Subsystem: "mcp", Allowed: []string{"dual", "structured"}, Description: "Wire layout of an MCP tools/call result (issue #8800). dual, the default, is today's result: a text summary, the envelope again as an embedded resource, and structuredContent. structured sends one text summary and structuredContent only, so a tool result's payload crosses the wire once; error results follow the same rule, with no eshu://tool-error/dispatch resource. The response budget counts the serialized result as sent in the connection's shape and the 262144-byte limit does not change. This sets the process default, including stdio; a client can override it with the shape query parameter: on POST /mcp/message for that request, on GET /sse for the whole SSE session (the endpoint event carries it, and a POST that names its own shape still wins). An unknown value refuses to start, and an unknown shape parameter gets a 400. Switch the connection or this default back to dual to roll back."},
	{Name: "ESHU_MCP_TOKEN", Type: VarString, Subsystem: "mcp", Description: "Per-user Eshu API/MCP bearer token referenced -- never inlined -- by `eshu mcp setup` client snippets in token posture (issue #5169, F-8). Create it in the console (Profile -> API tokens, issue #5164) or via POST /api/v0/auth/local/api-tokens. The MCP client process interpolates it into the Authorization header; Eshu server processes never read this variable. `eshu mcp setup --verify` uses it for the staged probes when --api-key is not set."},
	{Name: "ESHU_MCP_TOOL_SURFACE", Type: VarEnum, Default: "full", Subsystem: "mcp", Allowed: []string{"full", "router", "core_router"}, Description: "Which tools MCP tools/list advertises (issue #8758, epic #8752): full lists every registered tool (today's list, unchanged), router lists find_tool and call_tool only, core_router lists a small core set plus find_tool and call_tool. It changes advertisement only: a tool the surface hides still dispatches on tools/call, tools hidden by ESHU_QUERY_PROFILE stay hidden from tools/list in every mode, and the profile's per-call gate is unchanged. An unknown value refuses to start. A client can override it with the surface query parameter: on POST /mcp/message it applies to that request, and on GET /sse it applies to the whole SSE session (the endpoint event carries it, and a POST that names its own surface still wins). An unknown surface value gets a 400. router and core_router serve find_tool with the lexical ranker built at startup on every surface (issue #8771), and the server refuses to start on them without a ranker; core_router advertises find_tool, call_tool, and the 10 core tools listed in go/internal/mcp/router/core.go (issue #8759). full lists the same tools as before but also builds the ranker at startup."},
	{Name: "ESHU_MCP_TRANSPORT", Type: VarEnum, Default: "http", Subsystem: "mcp", Allowed: []string{"http", "stdio"}, Description: "MCP server transport mode."},
}

// ClientEntries returns a copy of the published client variable table. The
// table is fixed at this module version; validating a different registry
// scope needs a newer module pin, never a hand-edited copy.
func ClientEntries() []Entry {
	out := make([]Entry, len(clientEntries))
	copy(out, clientEntries)
	return out
}
