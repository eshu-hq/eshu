// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package python

import (
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// djangoURLRouteNames is the set of call names that denote a Django URL
// dispatcher (the first positional argument is a route pattern). When an
// import-from statement imports from django.conf.urls or django.urls,
// only names in this set (or their aliases) are treated as routing evidence.
var djangoURLRouteNames = map[string]struct{}{
	"path":    {},
	"url":     {},
	"re_path": {},
}

// pythonDjangoURLImportNames returns the set of simple names actually
// imported from django.conf.urls or django.urls (legacy and modern). It
// resolves aliases (import url as my_url → "my_url" in the set) and
// only includes names that match the djangoURLRouteNames set above. A
// bare `from django.urls import reverse` returns an empty map because
// "reverse" is not a URL dispatcher name, preventing false route
// detection for non-URLconf modules that happen to import from
// django.urls.
func pythonDjangoURLImportNames(root *tree_sitter.Node, source []byte) map[string]struct{} {
	names := make(map[string]struct{})
	walkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "import_from_statement" {
			return
		}
		text := strings.TrimSpace(nodeText(node, source))
		// Extract the module path: "from MODULE import NAMES".
		module, imports, ok := strings.Cut(text, " import ")
		if !ok {
			return
		}
		module = strings.TrimPrefix(module, "from ")
		module = strings.TrimSpace(module)
		if module != "django.conf.urls" && module != "django.urls" {
			return
		}
		// imports may be "(name1, name2)" or "name1, name2".
		imports = strings.TrimSpace(imports)
		imports = strings.TrimPrefix(imports, "(")
		imports = strings.TrimSuffix(imports, ")")
		for _, clause := range strings.Split(imports, ",") {
			clause = strings.TrimSpace(clause)
			if clause == "" {
				continue
			}
			// Handle "name as alias" → collect the alias.
			if before, after, hasAs := strings.Cut(clause, " as "); hasAs {
				alias := strings.TrimSpace(after)
				base := strings.TrimSpace(before)
				if alias != "" && base != "" {
					if _, ok := djangoURLRouteNames[base]; ok {
						names[alias] = struct{}{}
					}
				}
				continue
			}
			// Bare name — collect it directly.
			name := strings.TrimSpace(clause)
			if name != "" {
				if _, ok := djangoURLRouteNames[name]; ok {
					names[name] = struct{}{}
				}
			}
		}
	})
	return names
}

// pythonDjangoURLImportNamesGathered mirrors pythonDjangoURLImportNames
// but iterates a pre-gathered import slice.
func pythonDjangoURLImportNamesGathered(gathered []*tree_sitter.Node, source []byte) map[string]struct{} {
	names := make(map[string]struct{})
	for _, node := range gathered {
		if node.Kind() != "import_from_statement" {
			continue
		}
		text := strings.TrimSpace(nodeText(node, source))
		module, imports, ok := strings.Cut(text, " import ")
		if !ok {
			continue
		}
		module = strings.TrimPrefix(module, "from ")
		module = strings.TrimSpace(module)
		if module != "django.conf.urls" && module != "django.urls" {
			continue
		}
		imports = strings.TrimSpace(imports)
		imports = strings.TrimPrefix(imports, "(")
		imports = strings.TrimSuffix(imports, ")")
		for _, clause := range strings.Split(imports, ",") {
			clause = strings.TrimSpace(clause)
			if clause == "" {
				continue
			}
			if before, after, hasAs := strings.Cut(clause, " as "); hasAs {
				alias := strings.TrimSpace(after)
				base := strings.TrimSpace(before)
				if alias != "" && base != "" {
					if _, ok := djangoURLRouteNames[base]; ok {
						names[alias] = struct{}{}
					}
				}
				continue
			}
			name := strings.TrimSpace(clause)
			if name != "" {
				if _, ok := djangoURLRouteNames[name]; ok {
					names[name] = struct{}{}
				}
			}
		}
	}
	return names
}
