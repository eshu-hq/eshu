// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package declaration

import (
	"sort"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
)

// rawTextNonPinNames lists the raw_text exact names the parser registry admits
// that are not runtime-version pin files: build, tool and editor configuration
// with no runtime version to declare. The registry has no machine-readable
// "version pin" class, so this allowlist is the other half of the guard below:
// every raw_text exact name is either a pin file in pinFileSourceKinds or one
// of these. A name added to the registry must be placed in one of the two
// lists, so a new pin file can never be admitted and silently emit nothing.
var rawTextNonPinNames = map[string]struct{}{
	"Puppetfile": {}, "Berksfile": {},
	"Makefile": {}, "GNUmakefile": {}, "justfile": {},
	".editorconfig": {}, ".gitattributes": {}, ".dockerignore": {}, ".gitignore": {},
	".eslintrc": {}, ".prettierrc": {}, ".babelrc": {}, ".npmignore": {},
	"tox.ini": {}, "mypy.ini": {}, "pytest.ini": {}, ".flake8": {},
	"rustfmt.toml": {}, "clippy.toml": {}, "renovate.json5": {},
}

func rawTextExactNames(t *testing.T) []string {
	t.Helper()
	definition, ok := parser.DefaultRegistry().LookupByParserKey("raw_text")
	if !ok {
		t.Fatal(`parser registry has no "raw_text" definition`)
	}
	names := append([]string(nil), definition.ExactNames...)
	sort.Strings(names)
	return names
}

// TestPinFileSourceKindsCoverEveryRegistryVersionPin fails when the parser
// registry admits a raw_text name that is neither a pin file this package
// emits from nor a known non-pin configuration file (#8969 review: the
// registry gained .swift-version while this package still listed eleven
// names).
func TestPinFileSourceKindsCoverEveryRegistryVersionPin(t *testing.T) {
	t.Parallel()

	for _, name := range rawTextExactNames(t) {
		_, isPin := pinFileSourceKinds[name]
		_, isOther := rawTextNonPinNames[name]
		switch {
		case isPin && isOther:
			t.Errorf("%q is in both pinFileSourceKinds and rawTextNonPinNames", name)
		case !isPin && !isOther:
			t.Errorf("parser registry admits raw_text %q but it is in neither pinFileSourceKinds nor rawTextNonPinNames: "+
				"a version-pin file needs a source kind, a runtime and table-test rows here; any other file goes in rawTextNonPinNames", name)
		}
	}
}

// TestPinFileSourceKindsAreAllRegistryAdmitted is the reverse direction: every
// name this package emits from must be admitted by the registry as raw_text,
// or the content walk would never hand its body to the emitter. Each name also
// needs a runtime: a multi-runtime parser or a fileRuntime entry.
func TestPinFileSourceKindsAreAllRegistryAdmitted(t *testing.T) {
	t.Parallel()

	registry := parser.DefaultRegistry()
	multiRuntime := map[string]bool{sourceKindToolVersions: true, sourceKindSdkmanrc: true}
	for name, kind := range pinFileSourceKinds {
		definition, ok := registry.LookupByPath("services/api/" + name)
		admitted := ok && (definition.ParserKey == "raw_text" || name == "runtime.txt" && definition.ParserKey == "runtime_txt")
		if !admitted {
			t.Errorf("pin file %q is not admitted by the parser registry (got %q, %v)", name, definition.ParserKey, ok)
		}
		if !multiRuntime[kind] && fileRuntime[kind] == "" {
			t.Errorf("pin file %q (source kind %q) has no runtime in fileRuntime", name, kind)
		}
	}
	for name := range rawTextNonPinNames {
		if _, ok := registry.LookupByPath(name); !ok {
			t.Errorf("rawTextNonPinNames lists %q, which the parser registry no longer admits: drop it", name)
		}
	}
}
