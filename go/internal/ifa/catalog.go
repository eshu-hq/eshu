// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import "sort"

// CatalogOdu pairs one cataloged Odù with a short human-facing detail for
// coverage reporting (why the fixture exists, what it proves).
type CatalogOdu struct {
	// Odu is the cataloged scenario.
	Odu Odu
	// Detail is a one-line human description of what the Odù proves.
	Detail string
}

// Catalog returns every cataloged Odù in stable, name-sorted order. The seed
// set lives in catalog_seed.go; Catalog() is the only accessor so the seed
// slice itself stays unexported and immutable to callers.
func Catalog() []CatalogOdu {
	out := make([]CatalogOdu, len(catalogSeed))
	copy(out, catalogSeed)
	sort.Slice(out, func(i, j int) bool { return out[i].Odu.Name < out[j].Odu.Name })
	return out
}

// CatalogByName indexes the cataloged Odùs by name for coverage-manifest ref
// resolution (a manifest row names an Odù by Catalog()'s Odu.Name).
func CatalogByName() map[string]Odu {
	byName := make(map[string]Odu, len(catalogSeed))
	for _, entry := range catalogSeed {
		byName[entry.Odu.Name] = entry.Odu
	}
	return byName
}
