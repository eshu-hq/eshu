// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package catalog

// Row config kinds. Only configKindDependency is admitted by the package
// consumption reducer; a plugin id is not a Maven coordinate, so plugin rows
// use their own kind and are never matched against package identity.
const (
	configKindDependency = "dependency"
	configKindPlugin     = "gradle_plugin"
)

// Resolution states, the same vocabulary the build-script parser uses.
const (
	stateResolved   = "resolved"
	statePartial    = "partial"
	stateUnresolved = "unresolved"
)

// finish resolves every entry's version against [versions] and builds rows in
// document order.
func (s *scanner) finish() Result {
	rows := make([]map[string]any, 0, len(s.entries))
	for _, e := range s.entries {
		value, state, unresolved := s.resolve(e.decl.ver)
		switch state {
		case stateResolved:
			s.counts.Resolved++
		case statePartial:
			s.counts.Partial++
		default:
			s.counts.Unresolved++
		}
		rows = append(rows, buildRow(e, value, state, unresolved))
	}
	return Result{State: StateParsed, Rows: rows, Counts: s.counts}
}

// resolve returns the version text to record, its resolution state, and the
// keys that blocked resolution. It returns a value only when the text is known
// verbatim: a literal that passes validVersionText, or a reference to a plain
// [versions] alias whose text passes it. Every other case returns no value.
func (s *scanner) resolve(v versionSpec) (value, state string, unresolved []string) {
	switch v.kind {
	case versionNone:
		return "", statePartial, nil
	case versionLiteral:
		if validVersionText(v.text) {
			return v.text, stateResolved, nil
		}
		return "", stateUnresolved, []string{"version"}
	case versionRef:
		def, found := s.versions[v.text]
		if found && !def.rich && validVersionText(def.text) {
			return def.text, stateResolved, nil
		}
		return "", stateUnresolved, []string{v.text}
	default:
		// versionRich: a constraint, not one version.
		return "", stateUnresolved, []string{"version"}
	}
}

// buildRow shapes one entry like the build-script parser's dependency rows.
// A catalog entry declares a dependency without proving the project uses it,
// so the row sets no direct_dependency and no dependency_scope.
func buildRow(e entry, value, state string, unresolved []string) map[string]any {
	section, kind := "libraries", configKindDependency
	if e.plugin {
		section, kind = "plugins", configKindPlugin
	}
	row := map[string]any{
		"name":                        e.decl.name,
		"line_number":                 e.line,
		"value":                       value,
		"section":                     section,
		"config_kind":                 kind,
		"package_manager":             PackageManager,
		"lang":                        LanguageName,
		"dependency_resolution_state": state,
		"dependency_path_kind":        "manifest",
		"catalog_alias":               e.alias,
	}
	if e.decl.ver.kind == versionRef {
		row["version_ref"] = e.decl.ver.text
	}
	if len(unresolved) > 0 {
		row["dependency_unresolved_keys"] = unresolved
	}
	return row
}
