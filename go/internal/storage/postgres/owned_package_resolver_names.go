// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/packageidentity"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// ownedNameLookupForms returns the keys to look up for one OSV package name,
// and whether a row may be owned without answering to any of them.
//
// The base key is packageidentity.LowerASCII over the space-trimmed name, which
// is byte-identical to the SQL expression of migration 247
// (LOWER(TRIM(entity_name) COLLATE "C") COLLATE "default"). The fold is a
// lookup concern only: the virtual slot hashes the stored name, never a key
// returned here.
//
// A PyPI key is the PEP 503 name, compared with the same fold over the stored
// name. A swift key is the repository key; a swift name with none (a bare feed
// name) stays the folded name, which matches a repository's final path segment,
// and is partial because a package can live in a repository of another name. A
// Maven coordinate adds its slash spelling, which Package URL paths store.
func ownedNameLookupForms(ecosystem, name string) ([]string, bool) {
	name = strings.Trim(name, " ")
	if name == "" {
		return nil, false
	}
	folded := packageidentity.LowerASCII(name)
	switch workflow.CanonicalOwnedPackageEcosystem(ecosystem) {
	case "pypi":
		return []string{packageidentity.NormalizePyPIName(folded)}, false
	case "swift":
		if key := packageidentity.SwiftRepositoryKey(folded); key != "" {
			return []string{key}, false
		}
		return []string{folded}, true
	case "maven":
		forms := []string{folded}
		if strings.Count(folded, ":") == 1 && !strings.Contains(folded, "/") {
			forms = append(forms, strings.Replace(folded, ":", "/", 1))
		}
		return forms, false
	}
	return []string{folded}, false
}
