// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/sdk/go/factschema"
	codegraphv1 "github.com/eshu-hq/eshu/sdk/go/factschema/codegraph/v1"
)

// importIdentity is the identity of one File-[:IMPORTS]->Module edge: its two
// endpoints, and nothing else.
//
// That is not a simplification, it is what the graph backend enforces. A
// relationship property map in a MERGE pattern — `MERGE (f)-[r:IMPORTS
// {imported_name: row.imported_name}]->(m)` — is NOT part of relationship
// identity on the pinned NornicDB build: a second MERGE with a different
// property value matches the first edge and overwrites it, leaving one edge
// where Cypher semantics call for two. That was measured against the pinned
// build before this extractor was written; see
// docs/public/reference/nornicdb-pitfalls.md.
//
// So the extractor folds every parser entry for one (file, module) pair into
// the single edge the backend can actually hold, and only carries a per-symbol
// property onto that edge when every entry agrees on it. Emitting one row per
// symbol would not have produced one edge per symbol — it would have produced
// one edge whose properties were decided by batch ordering.
type importIdentity struct {
	filePath   string
	moduleName string
}

// moduleIdentity is the identity of one import-graph Module node: its name and
// the language it belongs to. A Go `time` and a Python `time` are unrelated
// modules that share a name, so they are two nodes; every repository importing
// the same Go `time` still shares one. It mirrors the writer's
// `MERGE (m:Module {name, lang})` key exactly, so the in-process dedupe cannot
// collapse two modules the graph would have kept apart.
type moduleIdentity struct {
	name     string
	language string
}

// importAccumulator folds the parser's per-symbol import entries for one
// (file, module) pair into the properties their shared edge can honestly carry.
type importAccumulator struct {
	filePath   string
	moduleName string

	// moduleLanguage is the importing file's language, which resolves the
	// edge's target to the right Module node. Every entry folded into one
	// accumulator comes from the same file, so it never varies within one.
	moduleLanguage string

	// importedName and alias hold the agreed value across every folded entry;
	// nameConflict / aliasConflict record that the entries disagreed, in which
	// case the edge carries no value rather than an arbitrary one.
	importedName  string
	nameConflict  bool
	alias         string
	aliasConflict bool

	// lineNumber is the earliest attributed source line, so the edge points at
	// the first place the file imports the module.
	lineNumber int

	order int
}

// extractImportsFromFiles builds the generation's File-[:IMPORTS]->Module edge
// rows, and the Module nodes they point at, from the per-file "imports" bucket
// the language parsers write into every file fact's parsed_file_data.
//
// This is the producer issue #5691 found missing. The canonical writer, the
// delta refresh, the retract path and the /code/import-dependencies read
// surface were all complete; nothing populated
// CanonicalMaterialization.Imports, so a freshly indexed stack carried zero
// IMPORTS edges and symbol_graph.imports answered empty. The legacy
// extractRelationships path only matches the Python-runtime-era
// module_name/imported_module fact payloads, which no Go collector emits.
//
// Normalization across the language parsers reduces to one rule, because the
// bucket carries at most two name keys: the module is Source when the parser
// distinguishes module from symbol (Python, JavaScript/TypeScript), otherwise
// Name (Go, the C-family header parsers). The imported symbol is Name only when
// it actually differs from the module — a module-only import such as Go's
// `import "fmt"` or JavaScript's side-effect `import "x"` repeats the module in
// both keys and must not claim to import a symbol named after its own module.
//
// The input is the parsedFileRef slice extractFilesWithQuarantine already built
// while decoding each file fact, so no file fact is decoded twice.
//
// A file whose imports bucket is malformed is skipped AND quarantined, so it
// reaches the operator as an input_invalid dead-letter rather than as silence.
// Skipping alone would be an accuracy hole with teeth: on a delta or
// reconciliation write the refresh statement deletes that file's existing
// IMPORTS edges before this extractor runs, so a bucket that fails to decode
// would leave the file with no import edges, its File node intact, and nothing
// in the metrics or logs to say why.
func extractImportsFromFiles(files []parsedFileRef) ([]ImportRow, []ModuleRow, []quarantinedFact) {
	if len(files) == 0 {
		return nil, nil, nil
	}

	folded := make(map[importIdentity]*importAccumulator, len(files))
	modules := make(map[moduleIdentity]struct{})
	var quarantined []quarantinedFact

	for _, file := range files {
		// The extractor reads only the named fields, so the per-entry
		// Attributes remainder map would be allocated and immediately discarded
		// for every import in the repository.
		entries, err := factschema.DecodeParsedFileDataImports(
			file.ParsedFileData,
			factschema.WithoutAttributesRemainder(),
		)
		if err != nil {
			quarantined = append(quarantined, quarantinedFact{
				factID:         file.FactID,
				factKind:       file.FactKind,
				field:          "parsed_file_data.imports",
				classification: factschema.ClassificationInputInvalid,
			})
			continue
		}
		if len(entries) == 0 {
			continue
		}

		filePath := file.Path
		fileLanguage := file.Language

		for _, entry := range entries {
			moduleName, importedName, alias, lineNumber, ok := normalizeImportEntry(entry)
			if !ok {
				continue
			}
			identity := importIdentity{filePath: filePath, moduleName: moduleName}
			acc, known := folded[identity]
			if !known {
				acc = &importAccumulator{
					filePath:       filePath,
					moduleName:     moduleName,
					moduleLanguage: fileLanguage,
					importedName:   importedName,
					alias:          alias,
					lineNumber:     lineNumber,
					order:          len(folded),
				}
				folded[identity] = acc
				modules[moduleIdentity{name: moduleName, language: fileLanguage}] = struct{}{}
				continue
			}
			acc.fold(importedName, alias, lineNumber)
		}
	}

	return importRowsFrom(folded), moduleRowsFrom(modules), quarantined
}

// fold merges one more parser entry into an existing (file, module) edge.
func (a *importAccumulator) fold(importedName, alias string, lineNumber int) {
	if importedName != a.importedName {
		a.nameConflict = true
	}
	if alias != a.alias {
		a.aliasConflict = true
	}
	if lineNumber != 0 && (a.lineNumber == 0 || lineNumber < a.lineNumber) {
		a.lineNumber = lineNumber
	}
}

// importRowsFrom flattens the folded edges into writer rows in discovery order,
// dropping any property the folded entries disagreed on.
func importRowsFrom(folded map[importIdentity]*importAccumulator) []ImportRow {
	if len(folded) == 0 {
		return nil
	}
	accs := make([]*importAccumulator, 0, len(folded))
	for _, acc := range folded {
		accs = append(accs, acc)
	}
	sort.Slice(accs, func(i, j int) bool { return accs[i].order < accs[j].order })

	rows := make([]ImportRow, 0, len(accs))
	for _, acc := range accs {
		row := ImportRow{
			FilePath:       acc.filePath,
			ModuleName:     acc.moduleName,
			ModuleLanguage: acc.moduleLanguage,
			ImportedName:   acc.importedName,
			Alias:          acc.alias,
			LineNumber:     acc.lineNumber,
		}
		if acc.nameConflict {
			row.ImportedName = ""
		}
		if acc.aliasConflict {
			row.Alias = ""
		}
		rows = append(rows, row)
	}
	return rows
}

// normalizeImportEntry maps one parser import entry onto the edge properties.
// ok is false when the entry names no importable module — an entry with neither
// name nor source is a malformed producer emission, and minting an empty-named
// Module node for it would put an anonymous global node in the graph that every
// repository's unusable imports then attach to.
func normalizeImportEntry(entry codegraphv1.Import) (moduleName, importedName, alias string, lineNumber int, ok bool) {
	name := strings.TrimSpace(entry.Name)
	source := strings.TrimSpace(entry.Source)

	moduleName = source
	if moduleName == "" {
		moduleName = name
	}
	if moduleName == "" {
		return "", "", "", 0, false
	}

	if source != "" && name != source {
		importedName = name
	}

	lineNumber = entry.LineNumber
	if lineNumber < 0 {
		lineNumber = 0
	}

	return moduleName, importedName, strings.TrimSpace(entry.Alias), lineNumber, true
}

// moduleRowsFrom turns the deduped (name, language) module set into ModuleRow
// entries in a stable order, so two projections of the same generation write
// the same batch and a golden snapshot diff stays meaningful.
//
// There is deliberately no language tie-break here. The previous version had
// one -- a module seen from files in more than one language kept the smallest
// non-empty language -- because Module identity was the name alone and one
// language had to be chosen. Under (name, language) identity there is nothing
// to choose: each language gets its own row and its own node.
func moduleRowsFrom(modules map[moduleIdentity]struct{}) []ModuleRow {
	if len(modules) == 0 {
		return nil
	}
	rows := make([]ModuleRow, 0, len(modules))
	for identity := range modules {
		rows = append(rows, ModuleRow{Name: identity.name, Language: identity.language})
	}
	sortModuleRows(rows)
	return rows
}

// sortModuleRows orders module rows by (name, language) so the projected batch
// is deterministic across runs of the same generation.
func sortModuleRows(rows []ModuleRow) {
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Name != rows[j].Name {
			return rows[i].Name < rows[j].Name
		}
		return rows[i].Language < rows[j].Language
	})
}

// mergeImportModules appends the modules an import extraction discovered to the
// materialization's existing Module set, skipping any (name, language) pair
// already present so the entity-derived and import-derived module sets do not
// double-write.
//
// The dedupe key is the full node identity, not the name. Keying on name alone
// dropped every discovered module whose name an earlier row already carried, so
// a Python `time` imported by one repository never reached the writer once a Go
// `time` had been declared -- the same collision the writer had, one layer up,
// where fixing only the Cypher would have left it in place.
func mergeImportModules(existing []ModuleRow, discovered []ModuleRow) []ModuleRow {
	if len(discovered) == 0 {
		return existing
	}
	seen := make(map[moduleIdentity]struct{}, len(existing))
	for _, m := range existing {
		seen[moduleIdentity{name: m.Name, language: m.Language}] = struct{}{}
	}
	for _, m := range discovered {
		identity := moduleIdentity{name: m.Name, language: m.Language}
		if _, known := seen[identity]; known {
			continue
		}
		seen[identity] = struct{}{}
		existing = append(existing, m)
	}
	return existing
}
