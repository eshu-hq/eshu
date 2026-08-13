// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package offlinetier_test

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/projector"
)

// TestImportEdgeFixtureImportsResolveToDeclaredModules checks the live-tier
// fixture in this package without needing the live tier.
//
// Module identity is (name, lang), and the writer's IMPORTS phase resolves its
// target with `MATCH (m:Module {name: row.module_name, lang: row.module_language})`.
// A MATCH that finds nothing yields no row, so the MERGE never runs: the edge
// is not written, and nothing errors or logs. A fixture whose ImportRow names
// the module but not its language therefore turns the live proof red for a
// reason that has nothing to do with the code under test -- and only when
// someone runs the live tier, which is how these rows sat wrong on the branch
// that introduced the (name, lang) key.
//
// This runs in the ordinary suite so the mismatch surfaces at desk speed.
func TestImportEdgeFixtureImportsResolveToDeclaredModules(t *testing.T) {
	t.Parallel()

	mat := importEdgeMaterialization("gen-fixture-check", true, importEdgeRows())

	if len(mat.Imports) == 0 {
		t.Fatal("fixture declares no import rows, so this check would prove nothing")
	}

	declared := make(map[projector.ModuleRow]struct{}, len(mat.Modules))
	for _, m := range mat.Modules {
		declared[projector.ModuleRow{Name: m.Name, Language: m.Language}] = struct{}{}
	}
	for _, imp := range mat.Imports {
		key := projector.ModuleRow{Name: imp.ModuleName, Language: imp.ModuleLanguage}
		if _, ok := declared[key]; !ok {
			t.Fatalf("import row %+v targets Module{name=%q, lang=%q}, which no fixture module row declares; "+
				"the writer would match no node and drop the edge silently. Declared: %+v",
				imp, imp.ModuleName, imp.ModuleLanguage, mat.Modules)
		}
	}
}
