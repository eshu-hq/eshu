// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package declaration

import "strings"

// packageJSONRuntimes are the runtimes read from the "engines" and "volta"
// sections, in the order they are emitted. Only these four are read: the
// sections are open maps ("vscode", "electron" and tool-specific keys live in
// engines), and an unlisted key is not a runtime this contract reports.
var packageJSONRuntimes = [...]string{"node", "npm", "yarn", "pnpm"}

// packageManagerNames are the package managers a "packageManager" field may
// name: the three corepack permits (yarn, npm, pnpm). The issue names no other,
// so any other name, bun included, is rejected and counted. The name becomes
// the fact's runtime, so it is an allowlist: an arbitrary name would carry
// arbitrary file text into a fact.
var packageManagerNames = map[string]struct{}{"npm": {}, "pnpm": {}, "yarn": {}}

// entry is one string member read from a manifest section. present is false
// for a runtime the section does not name.
type entry struct {
	line    int
	value   string
	present bool
}

// runtimeEntries holds one section's entries by packageJSONRuntimes index. A
// fixed array, not a map, so a hostile file that repeats "engines" tens of
// thousands of times allocates nothing per repeat.
type runtimeEntries [len(packageJSONRuntimes)]entry

// packageJSONSections is what one pass over a package.json keeps. A key that
// repeats replaces the earlier one whole, as JSON.parse does, so the last
// occurrence wins.
type packageJSONSections struct {
	engines, volta runtimeEntries
	manager        entry
}

// extractPackageJSON reads the engines, volta and packageManager declarations
// of one package.json body. rejected counts values the credential guard
// dropped. err is non-nil when the body is not readable JSON, in which case no
// declaration is returned: the part before the error would pass for the file.
func extractPackageJSON(body string) (declarations []declaration, rejected int, err error) {
	sc := newJSONScanner(body)
	if end, err := sc.atEnd(); err != nil {
		return nil, 0, err
	} else if end {
		return nil, 0, nil
	}
	if c, _ := sc.peek(); c != '{' {
		// A manifest whose root is not an object declares nothing, but it is
		// still validated so trailing text cannot hide behind it.
		if err := sc.skipValue(); err != nil {
			return nil, 0, err
		}
		return nil, 0, sc.requireEnd()
	}
	var sections packageJSONSections
	err = sc.eachMember(func(key string, line int) error {
		switch key {
		case "engines":
			section, err := readSection(sc)
			sections.engines = section
			return err
		case "volta":
			section, err := readSection(sc)
			sections.volta = section
			return err
		case "packageManager":
			sections.manager = entry{}
			if c, err := sc.peek(); err != nil {
				return err
			} else if c != '"' {
				return sc.skipValue()
			}
			value, err := sc.stringValue(maxEscapedValueBytes)
			if err != nil {
				return err
			}
			sections.manager = entry{line: line, value: value, present: true}
			return nil
		}
		return sc.skipValue()
	})
	if err != nil {
		return nil, 0, err
	}
	if err := sc.requireEnd(); err != nil {
		return nil, 0, err
	}
	return sections.declarations()
}

// requireEnd fails when anything but whitespace and comments follows the root.
func (sc *jsonScanner) requireEnd() error {
	if end, err := sc.atEnd(); err != nil {
		return err
	} else if !end {
		return errMalformedJSON
	}
	return nil
}

// readSection reads an "engines" or "volta" value. An object yields its
// allowlisted string members; any other value yields nil.
func readSection(sc *jsonScanner) (runtimeEntries, error) {
	var section runtimeEntries
	if c, err := sc.peek(); err != nil {
		return section, err
	} else if c != '{' {
		return section, sc.skipValue()
	}
	err := sc.eachMember(func(key string, line int) error {
		slot := runtimeSlot(key)
		if slot >= 0 {
			// A repeated member replaces the earlier one whole, as JSON.parse
			// does, so clear the slot before the value is known to be a string.
			section[slot] = entry{}
		}
		if c, err := sc.peek(); err != nil {
			return err
		} else if c != '"' || slot < 0 {
			return sc.skipValue()
		}
		value, err := sc.stringValue(maxEscapedValueBytes)
		if err != nil {
			return err
		}
		section[slot] = entry{line: line, value: value, present: true}
		return nil
	})
	return section, err
}

// runtimeSlot returns the packageJSONRuntimes index of key, or -1.
func runtimeSlot(key string) int {
	for i, runtime := range packageJSONRuntimes {
		if key == runtime {
			return i
		}
	}
	return -1
}

// declarations turns the kept sections into declarations in a fixed order.
// Every string value that does not become a declaration (empty, whitespace
// only, failing the credential guard, a packageManager without "@" or with an
// unlisted name or an empty spec) adds one to rejected. A value that is not a
// string is absent, not rejected. The order is fixed:
// engines, volta, then packageManager, each runtime in packageJSONRuntimes
// order. order_index counts the declarations already placed on the same line,
// so a minified one-line manifest still gets distinct stable keys.
func (s packageJSONSections) declarations() (out []declaration, rejected int, err error) {
	onLine := map[int]int{}
	add := func(section section, runtime string, e entry) {
		raw := strings.TrimSpace(e.value)
		if raw == "" || !safeSpec(raw) {
			// An empty value declares no version, the same as a malformed one:
			// it is dropped and counted, as a go.mod directive without an
			// argument is.
			rejected++
			return
		}
		out = append(out, declaration{
			line: e.line, order: onLine[e.line], section: section,
			runtime: runtime, rawSpec: raw,
		})
		onLine[e.line]++
	}
	for i, runtime := range packageJSONRuntimes {
		if s.engines[i].present {
			add(sectionPackageJSONEngines, runtime, s.engines[i])
		}
	}
	for i, runtime := range packageJSONRuntimes {
		if s.volta[i].present {
			add(sectionPackageJSONVolta, runtime, s.volta[i])
		}
	}
	if s.manager.present {
		name, spec, ok := packageManagerSpec(s.manager.value)
		switch {
		case !ok:
			// No "@", or a name corepack does not permit: the string declares
			// no manager and version, so it is counted, not silently absent.
			rejected++
		default:
			add(sectionPackageJSONManager, name, entry{line: s.manager.line, value: spec, present: true})
		}
	}
	return out, rejected, nil
}

// packageManagerSpec splits a corepack "name@version" value into the manager
// name and the version, dropping the "+sha512.<hex>" integrity suffix so the
// hash never reaches a fact. ok is false when the value has no "@", or the
// name is not an allowlisted package manager.
func packageManagerSpec(value string) (name, spec string, ok bool) {
	name, rest, found := strings.Cut(strings.TrimSpace(value), "@")
	if !found {
		return "", "", false
	}
	if _, known := packageManagerNames[name]; !known {
		return "", "", false
	}
	spec, _, _ = strings.Cut(rest, "+")
	return name, spec, true
}
