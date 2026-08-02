// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	codegraphv1 "github.com/eshu-hq/eshu/sdk/go/factschema/codegraph/v1"
)

// importIdentity is the canonical identity of one File-[:IMPORTS]->Module edge.
// It mirrors the MERGE key canonicalNodeImportEdgeCypher writes, so two parser
// entries that collapse to the same edge are folded here — deterministically,
// keeping the first source line — rather than racing to overwrite each other's
// properties at write time.
type importIdentity struct {
	filePath     string
	moduleName   string
	importedName string
}

// extractImportsFromFiles builds the generation's File-[:IMPORTS]->Module edge
// rows, and the Module nodes they point at, from the per-file "imports" bucket
// the language parsers write into every file fact's parsed_file_data.
//
// This is the producer issue #5691 found missing. The canonical writer, the
// delta refresh, the retract path, and the /code/import-dependencies read
// surface were all complete; nothing populated CanonicalMaterialization.Imports,
// so a freshly indexed stack carried zero IMPORTS edges and symbol_graph.imports
// answered empty. The legacy extractRelationships path only matched the
// Python-runtime-era module_name/imported_module fact payloads, which no Go
// collector emits.
//
// Normalization across the language parsers reduces to one rule, because the
// bucket carries at most two name keys: the module is Source when the parser
// distinguishes module from symbol (Python, JavaScript/TypeScript), otherwise
// Name (Go, the C-family header parsers). The imported symbol is Name only when
// it actually differs from the module — a module-only import such as Go's
// `import "fmt"` or JavaScript's side-effect `import "x"` repeats the module in
// both keys and must not claim to import a symbol named after its own module.
//
// A file whose imports bucket is malformed is skipped rather than failing the
// generation: the rest of that repository's graph truth still projects, matching
// how extractFilesWithQuarantine treats an undecodable file fact.
func extractImportsFromFiles(envelopes []facts.Envelope, repoPath string) ([]ImportRow, []ModuleRow) {
	fileFacts := FilterFileFacts(envelopes)
	if len(fileFacts) == 0 {
		return nil, nil
	}

	rows := make([]ImportRow, 0, len(fileFacts))
	rowIndex := make(map[importIdentity]int, len(fileFacts))
	moduleLanguages := make(map[string]string)

	for i := range fileFacts {
		if fileFacts[i].IsTombstone {
			continue
		}

		file, err := decodeCodegraphFile(fileFacts[i])
		if err != nil {
			continue
		}
		relativePath := strings.TrimSpace(file.RelativePath)
		if relativePath == "" {
			continue
		}

		// The extractor reads only the named fields, so the per-entry
		// Attributes remainder map would be allocated and immediately discarded
		// for every import in the repository.
		entries, err := factschema.DecodeParsedFileDataImports(
			file.ParsedFileData,
			factschema.WithoutAttributesRemainder(),
		)
		if err != nil || len(entries) == 0 {
			continue
		}

		filePath := qualifyPath(repoPath, relativePath)
		fileLanguage := strings.TrimSpace(codegraphDerefString(file.Language))

		for _, entry := range entries {
			row, ok := importRowFromEntry(entry, filePath)
			if !ok {
				continue
			}
			identity := importIdentity{row.FilePath, row.ModuleName, row.ImportedName}
			if at, duplicate := rowIndex[identity]; duplicate {
				if preferImportRow(row, rows[at]) {
					rows[at] = row
				}
				continue
			}
			rowIndex[identity] = len(rows)
			rows = append(rows, row)
			recordModuleLanguage(moduleLanguages, row.ModuleName, fileLanguage)
		}
	}

	return rows, moduleRowsFromLanguages(moduleLanguages)
}

// preferImportRow decides which of two parser entries that collapse to the same
// IMPORTS edge supplies the edge's properties. The graph carries one line number
// per edge, so the choice must not depend on bucket ordering — the parsers sort
// the imports bucket by name, leaving same-identity ties in an order no
// producer guarantees. The earliest attributed source line wins, and a known
// line always beats an unattributed 0, which the extractor reads as "unknown
// line" rather than line zero.
func preferImportRow(candidate, current ImportRow) bool {
	if current.LineNumber == 0 {
		return candidate.LineNumber != 0
	}
	return candidate.LineNumber != 0 && candidate.LineNumber < current.LineNumber
}

// recordModuleLanguage keeps one language per imported module, chosen
// order-independently: a module imported from files in more than one language
// (a TypeScript and a JavaScript file importing the same package) would
// otherwise take whichever file fact happened to arrive first, making the
// projected Module.lang depend on envelope ordering. Smallest non-empty
// language wins; an unknown file language never displaces a known one.
func recordModuleLanguage(moduleLanguages map[string]string, moduleName, fileLanguage string) {
	current, known := moduleLanguages[moduleName]
	switch {
	case !known:
		moduleLanguages[moduleName] = fileLanguage
	case current == "":
		moduleLanguages[moduleName] = fileLanguage
	case fileLanguage != "" && fileLanguage < current:
		moduleLanguages[moduleName] = fileLanguage
	}
}

// importRowFromEntry normalizes one parser import entry into an edge row. ok is
// false when the entry names no importable module — an entry with neither name
// nor source is a malformed producer emission, and minting an empty-named
// Module node for it would put an anonymous global node in the graph that every
// repository's unusable imports then attach to.
func importRowFromEntry(entry codegraphv1.Import, filePath string) (ImportRow, bool) {
	name := strings.TrimSpace(entry.Name)
	source := strings.TrimSpace(entry.Source)

	moduleName := source
	if moduleName == "" {
		moduleName = name
	}
	if moduleName == "" {
		return ImportRow{}, false
	}

	importedName := ""
	if source != "" && name != source {
		importedName = name
	}

	lineNumber := entry.LineNumber
	if lineNumber < 0 {
		lineNumber = 0
	}

	return ImportRow{
		FilePath:     filePath,
		ModuleName:   moduleName,
		ImportedName: importedName,
		Alias:        strings.TrimSpace(entry.Alias),
		LineNumber:   lineNumber,
	}, true
}

// moduleRowsFromLanguages turns the deduped module-name set into ModuleRow
// entries in a stable order, so two projections of the same generation write
// the same batch and a golden snapshot diff stays meaningful.
func moduleRowsFromLanguages(moduleLanguages map[string]string) []ModuleRow {
	if len(moduleLanguages) == 0 {
		return nil
	}
	names := make([]string, 0, len(moduleLanguages))
	for name := range moduleLanguages {
		names = append(names, name)
	}
	sort.Strings(names)

	rows := make([]ModuleRow, 0, len(names))
	for _, name := range names {
		rows = append(rows, ModuleRow{Name: name, Language: moduleLanguages[name]})
	}
	return rows
}

// mergeImportModules appends the modules an import extraction discovered to the
// materialization's existing Module set, skipping any name already present so
// the entity-derived and import-derived module sets do not double-write.
func mergeImportModules(existing []ModuleRow, discovered []ModuleRow) []ModuleRow {
	if len(discovered) == 0 {
		return existing
	}
	seen := make(map[string]struct{}, len(existing))
	for _, m := range existing {
		seen[m.Name] = struct{}{}
	}
	for _, m := range discovered {
		if _, ok := seen[m.Name]; ok {
			continue
		}
		seen[m.Name] = struct{}{}
		existing = append(existing, m)
	}
	return existing
}
