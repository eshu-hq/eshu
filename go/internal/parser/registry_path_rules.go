// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"path/filepath"
	"strings"
)

// isPipConstraintsName reports whether a lower-cased base name is a pip
// constraints file: constraints.txt, constraints-*.txt, or constraints_*.txt
// (#8821). It is a path rule, not a PrefixNames entry, because a prefix
// matches before the extension index and would take constraints-*.yaml
// policy files (Gatekeeper constraints, for one) away from the YAML parser.
// The .txt suffix keeps the family to pip input.
func isPipConstraintsName(base string) bool {
	stem, ok := strings.CutSuffix(base, ".txt")
	if !ok {
		return false
	}
	return stem == "constraints" ||
		strings.HasPrefix(stem, "constraints-") ||
		strings.HasPrefix(stem, "constraints_")
}

func isScalaPlayRoutesPath(path string) bool {
	normalized := strings.ToLower(filepath.ToSlash(filepath.Clean(path)))
	return normalized == "conf/routes" ||
		strings.HasSuffix(normalized, "/conf/routes") ||
		strings.HasSuffix(normalized, ".routes")
}

// isMavenExtensionsPath reports whether path is a Maven core extensions file,
// .mvn/extensions.xml, at any depth. The base name extensions.xml is too
// generic to bind by name, so the parent directory is part of the rule.
func isMavenExtensionsPath(path string) bool {
	// Cheap base-name test first: LookupByPath runs once per walked file, and
	// only an extensions.xml needs the allocating normalization below.
	if !strings.EqualFold(filepath.Base(path), "extensions.xml") {
		return false
	}
	normalized := strings.ToLower(filepath.ToSlash(filepath.Clean(path)))
	return normalized == ".mvn/extensions.xml" ||
		strings.HasSuffix(normalized, "/.mvn/extensions.xml")
}
