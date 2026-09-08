// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"sort"

	"github.com/eshu-hq/eshu/go/internal/ifa/familyodu"
)

// familyodu.CatalogOdu lives in familyodu/types.go beside the family fixtures.

// Catalog returns every cataloged Odù in stable, name-sorted order. The seed
// set lives in catalog_seed.go; Catalog() is the only accessor so the seed
// slice itself stays unexported and immutable to callers.
func Catalog() []familyodu.CatalogOdu {
	out := make([]familyodu.CatalogOdu, len(catalogSeed))
	copy(out, catalogSeed)
	sort.Slice(out, func(i, j int) bool { return out[i].Odu.Name < out[j].Odu.Name })
	return out
}

// CatalogByName indexes the cataloged Odùs by name for coverage-manifest ref
// resolution (a manifest row names an Odù by Catalog()'s familyodu.Odu.Name).
func CatalogByName() map[string]familyodu.Odu {
	byName := make(map[string]familyodu.Odu, len(catalogSeed))
	for _, entry := range catalogSeed {
		byName[entry.Odu.Name] = entry.Odu
	}
	return byName
}
