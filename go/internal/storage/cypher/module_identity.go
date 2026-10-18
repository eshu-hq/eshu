// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"cmp"
	"fmt"
	"slices"

	"github.com/eshu-hq/eshu/go/internal/projector/canonical"
)

// canonicalNodeModuleUpsertCypher MERGEs the import-graph Module node on its
// import_key, name + "@" + language. That pair is the module's identity: a Go
// `time` and a Python `time` are unrelated modules that share a name, while two
// repositories importing the same Go `time` share one node. Repository and
// scope are deliberately NOT in the key -- that shared node IS the
// cross-repository dependency link, and the relationships catalog records the
// same intent by marking IMPORTS targetAttributable=false.
//
// The key is one property, computed in Go per row, because only a uniqueness
// constraint makes a concurrent MERGE safe: Neo4j documents that MERGE alone
// guarantees the pattern exists, not that it is unique. Issue #8168 measured
// it on the pinned Neo4j -- two writers MERGEing the same keys at once created
// duplicate nodes in 99 of 100 iterations. The module_import_key constraint
// (graph schema_tables.go) cannot be a (name, lang) constraint, because
// semantic Module entities carry both properties and may share them; they
// carry no import_key, so the constraint never sees them, and this MERGE never
// binds one.
//
// Every property is written ON CREATE only. An unconditional SET takes an
// exclusive lock on the node, and the canonical writer holds it until the
// whole repository transaction commits, so every repository importing Go
// `context` would wait for the others in turn.
//
// Rows arrive sorted by import_key (CanonicalModuleRows), which gives every
// writer one total lock order on the constraint's index entries; without it
// two writers listing the same modules in different orders deadlock.
const canonicalNodeModuleUpsertCypher = `UNWIND $rows AS row
MERGE (m:Module {import_key: row.import_key})
ON CREATE SET m.name = row.name, m.lang = row.language, m.evidence_source = 'projector/canonical'`

// CanonicalNodeModuleUpsertCypher is the import-graph Module upsert that
// CanonicalNodeWriter batches with the rows CanonicalModuleRows builds (keys
// `import_key`, `name`, `language`). It is exported only so
// internal/backendconformance can run the exact production statement against
// both backends; production callers go through the writer.
const CanonicalNodeModuleUpsertCypher = canonicalNodeModuleUpsertCypher

// canonicalNodeImportEdgeCypher wires each file to the module it imports. The
// target is the exact canonical node, resolved on module_import_key, which
// carries the importing file's own language. Matching on name alone would
// attach one file to every same-named module, and matching on (name, lang)
// would also bind a semantic Module entity that carries the same pair.
//
// type_only, deferred, and inferred (issue #7345) are the parser's import flags
// folded across the (file, module) pair; see foldImportFlags in
// projector/canonical. They are SET unconditionally and always as explicit
// booleans. The MERGE matches on endpoints, so a re-projected edge is an
// existing edge, and only an unconditional SET of an explicit false overwrites a
// stale true; a CASE-guarded or write-only-true SET would keep it. The SET reads
// only the row, never the existing edge. An edge written before this change
// has no flag property at all, which a reader must treat as "flags unknown",
// not as false.
//
// source_package (issues #7347/#7348/#7349) is the importing file's own
// package: the Go package import path, the owning npm package name, or
// the owning Java package, derived by the projector. It is SET
// unconditionally and always as an explicit string for the same
// stale-overwrite reason; an edge written before it existed has no
// such property, which a package-cycle reader must treat as
// "package unknown" and exclude, never as an empty package.
const canonicalNodeImportEdgeCypher = `UNWIND $rows AS row
MATCH (f:File {path: row.file_path})
MATCH (m:Module {import_key: row.module_import_key})
MERGE (f)-[r:IMPORTS]->(m)
SET r.imported_name = row.imported_name, r.alias = row.alias, r.line_number = row.line_number,
    r.evidence_source = 'projector/canonical', r.generation_id = row.generation_id,
    r.type_only = row.type_only, r.deferred = row.deferred, r.inferred = row.inferred,
    r.source_package = row.source_package`

// CanonicalModuleImportKeyPendingCypher counts what a graph written before
// #8168 still holds and the import_key writer must never meet: canonical
// Module nodes (uid IS NULL) without an import_key, and IMPORTS edges on a
// uid-bearing (semantic) Module that the old (name, lang) MERGE adopted. It
// returns one row with the columns `unkeyed` and `adopted_imports`. Both must
// be zero before a schema marker with the Module=import_key contract is
// written: eshu-bootstrap-data-plane runs it as the last step of the Module
// migration, and eshu-bootstrap-index runs it before it marks a schema it
// applied itself. It reads every Module node, so it belongs on the schema
// path, never on a request path.
const CanonicalModuleImportKeyPendingCypher = `CALL () {
  MATCH (m:Module)
  WHERE m.uid IS NULL AND m.import_key IS NULL AND m.name IS NOT NULL
  RETURN count(m) AS unkeyed
}
CALL () {
  MATCH ()-[r:IMPORTS]->(s:Module)
  WHERE s.uid IS NOT NULL
  RETURN count(r) AS adopted_imports
}
RETURN unkeyed, adopted_imports`

// ParseCanonicalModuleImportKeyPending reads the one row
// CanonicalModuleImportKeyPendingCypher returns. It fails on anything but one
// row with integer `unkeyed` and `adopted_imports` columns, so a caller that
// writes the schema marker only on two zeros can never read a malformed answer
// as a clean graph.
func ParseCanonicalModuleImportKeyPending(rows []map[string]any) (unkeyed, adoptedImports int64, err error) {
	if len(rows) != 1 {
		return 0, 0, fmt.Errorf("module import key pending check returned %d rows, want 1", len(rows))
	}
	unkeyed, okUnkeyed := rows[0]["unkeyed"].(int64)
	adoptedImports, okAdopted := rows[0]["adopted_imports"].(int64)
	if !okUnkeyed || !okAdopted {
		return 0, 0, fmt.Errorf("module import key pending check returned %v, want integer unkeyed and adopted_imports", rows[0])
	}
	return unkeyed, adoptedImports, nil
}

// CanonicalModuleDuplicateKeysCypher is the duplicate query from issue #8168:
// one row per (name, lang) that more than one keyed canonical Module carries.
// The module_import_key constraint makes any row impossible, so zero rows is
// the invariant the golden-corpus gate holds a replayed graph to.
const CanonicalModuleDuplicateKeysCypher = `MATCH (m:Module) WHERE m.import_key IS NOT NULL
WITH m.name AS name, m.lang AS lang, count(m) AS n WHERE n > 1
RETURN name, lang, n`

// CanonicalModuleUnkeyedCountCypher returns one row whose `unkeyed` column
// counts canonical Module nodes (uid IS NULL) without an import_key. Every
// canonical Module the writer creates carries one, so a fresh graph holds
// zero; the golden-corpus gate holds a replayed graph to that.
const CanonicalModuleUnkeyedCountCypher = `MATCH (m:Module) WHERE m.uid IS NULL AND m.import_key IS NULL
RETURN count(m) AS unkeyed`

// CanonicalModuleImportKey returns the identity key of the canonical
// import-graph Module for name and language: name + "@" + language. It is the
// IMPORTS target_id the relationships catalog already publishes, so a reader
// and the writer name the node the same way. An empty language is a real
// language and keeps the separator ("index@"). It is canonical.ModuleImportKey,
// the key the projector's oversized-key guard measures.
func CanonicalModuleImportKey(name, language string) string {
	return canonical.ModuleImportKey(name, language)
}

// CanonicalModuleRows builds the Module upsert rows for modules, sorted by
// import_key, without reordering modules. Every canonical writer (ingester,
// reducer, bootstrap-index) goes through this one order, so concurrent
// repository transactions take the module_import_key index-entry locks in the
// same sequence and cannot form a lock-order cycle. Duplicate keys stay
// adjacent; the MERGE matches the second one.
func CanonicalModuleRows(modules []canonical.ModuleRow) []map[string]any {
	if len(modules) == 0 {
		return nil
	}
	rows := make([]map[string]any, len(modules))
	for i, m := range modules {
		rows[i] = map[string]any{
			"import_key": CanonicalModuleImportKey(m.Name, m.Language),
			"name":       m.Name,
			"language":   m.Language,
		}
	}
	slices.SortStableFunc(rows, func(a, b map[string]any) int {
		return cmp.Compare(a["import_key"].(string), b["import_key"].(string))
	})
	return rows
}

// --- Phase L: Modules ---

func (w *CanonicalNodeWriter) buildModuleStatements(mat canonical.CanonicalMaterialization) []Statement {
	rows := CanonicalModuleRows(mat.Modules)
	if len(rows) == 0 {
		return nil
	}
	return BuildBatchedStatements(canonicalNodeModuleUpsertCypher, rows, w.batchSize)
}

// canonicalImportEdgeRows builds the IMPORTS rows sorted by module_import_key,
// then file path, then line. Creating a relationship locks the target node's
// relationship group, and the Module nodes are shared across repositories, so
// the edges must take those locks in the same order the Module upsert does.
// Measured on the pinned Neo4j with sorted Module rows and unsorted edge rows,
// 8 concurrent writers hit DeadlockDetected in 87 of 800 transactions; sorted,
// none did (#8168).
func canonicalImportEdgeRows(mat canonical.CanonicalMaterialization) []map[string]any {
	if len(mat.Imports) == 0 {
		return nil
	}
	rows := make([]map[string]any, len(mat.Imports))
	for i, imp := range mat.Imports {
		rows[i] = map[string]any{
			"file_path": imp.FilePath,
			// The exact canonical Module this file imports; see
			// canonicalNodeImportEdgeCypher.
			"module_import_key": CanonicalModuleImportKey(imp.ModuleName, imp.ModuleLanguage),
			"imported_name":     imp.ImportedName,
			"alias":             imp.Alias,
			"line_number":       imp.LineNumber,
			"generation_id":     mat.GenerationID,
			// The importing file's own package: Go import path (#7347),
			// owning npm package name (#7348), or owning Java package
			// (#7349). Always present as an explicit string so the SET
			// overwrites a stale package after the file moved packages.
			"source_package": imp.SourcePackage,
			// The parser's import flags, folded per edge. Always present as
			// explicit booleans so the SET overwrites a stale true.
			"type_only": imp.TypeOnly,
			"deferred":  imp.Deferred,
			"inferred":  imp.Inferred,
		}
	}
	slices.SortStableFunc(rows, func(a, b map[string]any) int {
		return cmp.Or(
			cmp.Compare(a["module_import_key"].(string), b["module_import_key"].(string)),
			cmp.Compare(a["file_path"].(string), b["file_path"].(string)),
			cmp.Compare(a["line_number"].(int), b["line_number"].(int)),
		)
	})
	return rows
}
