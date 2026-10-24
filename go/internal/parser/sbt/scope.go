// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sbt

import "strings"

// configOverlong is the configuration spelling recorded for an identifier
// chain longer than MaxFieldBytes. It is never a configuration name.
const configOverlong = "(overlong)"

// isThisBuildAxis reports a `ThisBuild /` axis before a key.
func isThisBuildAxis(prev2, prev token) bool {
	return prev.kind == kindSymbol && prev.text == "/" && prev2.kind == kindIdent && prev2.text == "ThisBuild"
}

// configPartScope maps one configuration name to the dependency scope
// vocabulary shared with the Gradle and Maven parsers, or reports it unknown.
// A mapping such as `test->default(compile)` is judged by its left side.
//
// The compiler-tool configurations (`plugin`, `scala-tool`, `scala-doc-tool`)
// and the artifact-type configurations (`docs`, `sources`, `pom`) are not
// classpath scopes a consumer can reason about, so they are unknown rather than
// a resolved compile; Gradle likewise gives annotationProcessor and kapt their
// own scope instead of folding them into compile.
func configPartScope(part string) (string, bool) {
	name := strings.ToLower(strings.TrimSpace(part))
	if arrow := strings.Index(name, "->"); arrow >= 0 {
		name = strings.TrimSpace(name[:arrow])
	}
	switch name {
	case "compile", "optional", "compile-internal":
		return "compile", true
	case "runtime", "runtime-internal":
		return "runtime", true
	case "provided":
		return "provided", true
	case "test", "it", "integrationtest", "test-internal":
		return "test", true
	}
	return "", false
}

// partName returns the lower-cased configuration name of one list part, ignoring
// any `->` mapping.
func partName(part string) string {
	name := strings.TrimSpace(part)
	if arrow := strings.Index(name, "->"); arrow >= 0 {
		name = strings.TrimSpace(name[:arrow])
	}
	return name
}

// configSpec is a resolved configuration: the canonical text carried on the
// row, the scope it maps to, and whether the scanner could map it. It is built
// once per `% config` or `.map(_ % config)` and then copied by value, so the
// operand string is never split or stored per dependency.
type configSpec struct {
	// text is the configuration spelling kept on the row. It is the operand
	// when that is at most MaxConfigBytes, else configOverlong.
	text     string
	scope    string
	known    bool
	optional bool
}

// resolveConfig maps an sbt configuration spelling to a configSpec. An empty or
// separator-only spelling (`% ""`, `% ","`) names no configuration, so it is
// unknown like any other spelling the table cannot map: sbt gives an empty
// configuration no documented meaning, and guessing the default compile scope
// for it would report a resolved compile row for a declaration nobody can read.
// A spelling over MaxConfigBytes is treated like an overlong identifier chain:
// it is not scanned, not stored, and unknown.
//
// A list of parts resolves by this table, where "any" means at least one part
// maps to that scope and every part must be a recognised configuration:
//
//	all parts one scope            -> that scope
//	any compile                    -> compile
//	runtime without compile        -> runtime (runtime is also on the test classpath)
//	provided with test             -> provided
//	runtime with provided          -> unknown (compile-time and run-time only)
//
// known is false when any part is not recognised (a custom Config value, a
// variable, an empty list); the scope is then compile only as a placeholder and
// the caller must report the row as partial, never as a resolved compile scope.
func resolveConfig(config string) configSpec {
	if len(config) > MaxConfigBytes {
		return configSpec{text: configOverlong, scope: "compile"}
	}
	spec := configSpec{text: config, scope: "compile"}
	var compile, runtime, provided, test, parts int
	for rest := config; rest != ""; {
		part := rest
		if cut := strings.IndexAny(rest, ",;"); cut >= 0 {
			part, rest = rest[:cut], rest[cut+1:]
		} else {
			rest = ""
		}
		if strings.TrimSpace(part) == "" {
			continue
		}
		parts++
		partScope, ok := configPartScope(part)
		if !ok {
			return spec
		}
		switch partScope {
		case "compile":
			compile++
		case "runtime":
			runtime++
		case "provided":
			provided++
		default:
			test++
		}
		if strings.EqualFold(partName(part), "optional") {
			spec.optional = true
		}
	}
	if parts == 0 {
		return spec
	}
	switch {
	case compile > 0:
		spec.scope = "compile"
	case runtime > 0 && provided > 0:
		return spec
	case runtime > 0:
		spec.scope = "runtime"
	case provided > 0:
		spec.scope = "provided"
	default:
		spec.scope = "test"
	}
	spec.known = true
	return spec
}
