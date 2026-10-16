// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package composer

import (
	"regexp"
	"strings"
)

// packageNamePattern is composer's own package-name grammar (the "name"
// property of the composer.json schema): lowercase vendor/name, each part
// alphanumeric runs joined by '.', '_', or one or two '-'. A name that fails it
// is never stamped or keyed. The grammar also keeps composer names disjoint
// from npm names: an unscoped npm name has no '/', and a scoped one starts with
// '@', so a package:<name># key can be routed to the right manifest kind by
// its name alone.
var packageNamePattern = regexp.MustCompile(`^[a-z0-9]([_.-]?[a-z0-9]+)*/[a-z0-9](([_.]|-{1,2})?[a-z0-9]+)*$`)

// ValidPackageName reports whether name is a composer package name under the
// composer.json schema grammar. Surrounding whitespace is not trimmed: a name
// with it is not one composer would accept.
func ValidPackageName(name string) bool {
	return packageNamePattern.MatchString(name)
}

// coversClass reports whether a psr-4 or psr-0 namespace prefix covers the
// fully qualified class name fqcn. A psr-4 prefix always ends in '\', so plain
// string-prefix matching stays on a namespace boundary.
func coversClass(prefix string, fqcn string) bool {
	return prefix != "" && strings.HasPrefix(fqcn, prefix) && len(fqcn) > len(prefix)
}

// normalizePrefix trims a leading '\' composer tolerates in autoload keys.
func normalizePrefix(prefix string) string {
	return strings.TrimPrefix(strings.TrimSpace(prefix), `\`)
}

// autoloadDirs returns the directories of one autoload entry, whose value is
// a string or an array of strings. Anything else maps to no directory.
func autoloadDirs(value any) []string {
	switch typed := value.(type) {
	case string:
		return []string{typed}
	case []any:
		dirs := make([]string, 0, len(typed))
		for _, item := range typed {
			if dir, ok := item.(string); ok {
				dirs = append(dirs, dir)
			}
		}
		return dirs
	default:
		return nil
	}
}
