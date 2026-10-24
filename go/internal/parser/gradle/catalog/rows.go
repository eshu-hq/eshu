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

// collisionKey is the unresolved key on a row whose alias collides with
// another alias in its table: Gradle keeps only one of them, and the file does
// not say which.
const collisionKey = "catalog_alias"

// finish resolves every entry's version against [versions] and builds rows in
// document order.
func (s *scanner) finish() Result {
	for _, table := range s.aliases {
		if table != nil {
			s.counts.Collided += table.collided()
		}
	}
	rows := make([]map[string]any, 0, len(s.entries))
	for _, e := range s.entries {
		value, state, unresolved := s.resolve(e.decl.ver)
		if s.entryCollides(e) {
			value, state, unresolved = "", stateUnresolved, []string{collisionKey}
		}
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

// entryCollides reports whether e's alias shares an accessor key (and so
// possibly a normalized spelling) with another alias in its table.
func (s *scanner) entryCollides(e entry) bool {
	which := sectionLibraries
	if e.plugin {
		which = sectionPlugins
	}
	return s.aliases[which].shared(e.slot)
}

// resolve returns the version text to record, its resolution state, and the
// keys that blocked resolution. It returns a value only when the text is one
// exact version: a literal that passes exactVersionText, or a reference whose
// normalized alias names exactly one plain [versions] alias whose text passes
// it. Every other case returns no value.
func (s *scanner) resolve(v versionSpec) (value, state string, unresolved []string) {
	switch v.kind {
	case versionNone:
		return "", statePartial, nil
	case versionLiteral:
		if exactVersionText(v.text) {
			return v.text, stateResolved, nil
		}
		return "", stateUnresolved, []string{"version"}
	case versionRef:
		if def, ok := s.versionFor(v.text); ok && !def.rich && exactVersionText(def.text) {
			return def.text, stateResolved, nil
		}
		return "", stateUnresolved, []string{v.text}
	default:
		// versionRich: a constraint, not one version.
		return "", stateUnresolved, []string{"version"}
	}
}

// versionFor returns the [versions] definition a reference names. Gradle
// normalizes the reference like an alias before the lookup. A reference to a
// shared slot is not found: Gradle keeps one of the colliding definitions and
// the file does not say which.
func (s *scanner) versionFor(ref string) (versionDef, bool) {
	table := s.aliases[sectionVersions]
	if table == nil {
		return versionDef{}, false
	}
	slot, found, shared := table.lookup(ref, &s.keyBuf)
	if !found || shared {
		return versionDef{}, false
	}
	def, defined := s.versions[slot]
	return def, defined
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
