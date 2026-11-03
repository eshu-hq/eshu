// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Typed file-payload decode for the reachability readers (#9861).
//
// The JS/TS, Python, and JVM reachability readers consume only a small slice
// of the file-fact payload: repo_id, repository_id, relative_path, and seven
// parsed_file_data keys (language/lang, imports, function_calls,
// function_calls_scip, functions, classes, plus decorators inside the entity
// buckets). Every other parser key (source snippets, variables, symbols,
// tokens, loc, ...) is ballast the full map decode allocates and no reader
// touches. Decoding only the used fields through this file yields identical
// indexes at a fraction of the bytes.
//
// Decode runs in two phases. The fast phase is one struct-driven pass that
// skips unknown keys without allocating them, exactly like the theory shim:
// clean collector shapes decode here at shim speed. Any shape the struct
// pass rejects falls back to the tolerant phase, which decodes through a map
// and converts with the payloadcore helpers themselves, so tolerance holds
// by construction. Both phases produce identical envelopes: the fast phase
// normalizes (trims every scalar, drops all-empty bucket rows, filters empty
// decorators) to exactly what the tolerant phase derives.
//
// Tolerance replicates payloadcore exactly, so a hostile shape decodes the
// same way both paths read it and never dead-letters a generation:
//   - scalar string fields render via fmt.Sprint plus TrimSpace with "" for
//     absent/null (PayloadStr for every JSON value; the AnyToString use-sites
//     in the three readers trim at use);
//   - object fields treat non-object JSON as absent (PayloadMap);
//   - array fields skip non-object items and nulls (MapSlice);
//   - decorators accept arrays only and decode non-arrays as absent
//     (SemanticPayloadStringSlice returns nil for non-arrays; a bare string
//     must NOT error).
//
// decodeTypedFilePayload errors ONLY on malformed JSON, preserving
// payloadstore.UnmarshalPayload's contract.
package core

import (
	"encoding/json"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
)

// typedFileStringSlice mirrors payloadcore.SemanticPayloadStringSlice: only
// real strings survive, everything else is skipped rather than failing the
// whole decode, and non-arrays decode as absent without an error.
type typedFileStringSlice []string

// UnmarshalJSON keeps the trimmed, non-empty strings of an array and decodes
// any non-array (including a bare string) as absent without an error.
func (s *typedFileStringSlice) UnmarshalJSON(raw []byte) error {
	if len(raw) == 0 || strings.TrimSpace(string(raw)) == "null" {
		*s = nil
		return nil
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return err
	}
	items, ok := value.([]any)
	if !ok {
		*s = nil
		return nil
	}
	var out typedFileStringSlice
	for _, item := range items {
		text, ok := item.(string)
		if !ok {
			continue
		}
		if trimmed := strings.TrimSpace(text); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	*s = out
	return nil
}

// typedFileImport holds the only import-row keys any of the three readers
// consume: source/name/alias/import_type/lang (JS/TS+Python) plus
// full_import_name (JVM).
type typedFileImport struct {
	Source         string `json:"source"`
	Name           string `json:"name"`
	Alias          string `json:"alias"`
	ImportType     string `json:"import_type"`
	Lang           string `json:"lang"`
	FullImportName string `json:"full_import_name"`
}

// typedFileCall holds the only call-row keys consumed: full_name/name/lang
// (JS/TS+Python) plus inferred_obj_type (JVM).
type typedFileCall struct {
	FullName        string `json:"full_name"`
	Name            string `json:"name"`
	Lang            string `json:"lang"`
	InferredObjType string `json:"inferred_obj_type"`
}

// typedFileSCIPCall holds the only SCIP-row keys consumed: package,
// package_name, import_source, callee_package, callee_symbol (JS/TS) plus
// callee_name (JVM).
type typedFileSCIPCall struct {
	Package       string `json:"package"`
	PackageName   string `json:"package_name"`
	ImportSource  string `json:"import_source"`
	CalleePackage string `json:"callee_package"`
	CalleeSymbol  string `json:"callee_symbol"`
	CalleeName    string `json:"callee_name"`
}

// typedFileEntity holds the only entity keys consumed: decorators
// (Python functions/classes buckets).
type typedFileEntity struct {
	Decorators typedFileStringSlice `json:"decorators"`
}

// typedParsedFileData holds the only parsed_file_data keys consumed:
// language/lang plus the five buckets above. Every other parser key is
// skipped by the decoder and never allocated as map/object graphs.
type typedParsedFileData struct {
	Language  string              `json:"language"`
	Lang      string              `json:"lang"`
	Imports   []typedFileImport   `json:"imports"`
	Calls     []typedFileCall     `json:"function_calls"`
	SCIPCalls []typedFileSCIPCall `json:"function_calls_scip"`
	Functions []typedFileEntity   `json:"functions"`
	Classes   []typedFileEntity   `json:"classes"`
}

// typedFilePayload holds the only envelope-payload keys consumed: repo_id,
// repository_id (JS/TS fallback), relative_path (JVM), parsed_file_data.
type typedFilePayload struct {
	RepoID       string               `json:"repo_id"`
	RepositoryID string               `json:"repository_id"`
	RelativePath string               `json:"relative_path"`
	Parsed       *typedParsedFileData `json:"parsed_file_data"`
}

// decodeTypedFilePayload decodes raw file-fact payload bytes into the used
// fields only. Clean shapes decode in one struct-driven pass; hostile shapes
// fall back to the tolerant map conversion. It errors only on malformed
// JSON; a shape mismatch at any field decodes as absent, never as an error.
func decodeTypedFilePayload(raw []byte) (typedFilePayload, error) {
	if len(raw) == 0 {
		return typedFilePayload{}, nil
	}
	var fast typedFilePayload
	if err := json.Unmarshal(raw, &fast); err == nil {
		return normalizeTypedFilePayload(fast), nil
	}
	var wide map[string]any
	if err := json.Unmarshal(raw, &wide); err != nil {
		return typedFilePayload{}, err
	}
	return typedFilePayloadFromMap(wide), nil
}

// normalizeTypedFilePayload brings a fast-phase decode to exactly what the
// tolerant phase derives: every scalar trimmed ("<nil>" to ""), all-empty
// bucket rows dropped (the tolerant phase skips null items), so both phases
// project identical envelopes.
func normalizeTypedFilePayload(out typedFilePayload) typedFilePayload {
	out.RepoID = normalizeTypedFileString(out.RepoID)
	out.RepositoryID = normalizeTypedFileString(out.RepositoryID)
	out.RelativePath = normalizeTypedFileString(out.RelativePath)
	if out.Parsed == nil {
		return out
	}
	parsed := out.Parsed
	parsed.Language = normalizeTypedFileString(parsed.Language)
	parsed.Lang = normalizeTypedFileString(parsed.Lang)
	parsed.Imports = normalizeTypedFileImports(parsed.Imports)
	parsed.Calls = normalizeTypedFileCalls(parsed.Calls)
	parsed.SCIPCalls = normalizeTypedFileSCIPCalls(parsed.SCIPCalls)
	parsed.Functions = normalizeTypedFileEntities(parsed.Functions)
	parsed.Classes = normalizeTypedFileEntities(parsed.Classes)
	return out
}

// normalizeTypedFileString trims a fast-phase scalar like PayloadStr.
func normalizeTypedFileString(value string) string {
	if trimmed := strings.TrimSpace(value); trimmed != "<nil>" {
		return trimmed
	}
	return ""
}

// normalizeTypedFileImports trims every row and drops all-empty rows.
func normalizeTypedFileImports(rows []typedFileImport) []typedFileImport {
	kept := rows[:0]
	for _, entry := range rows {
		entry.Source = normalizeTypedFileString(entry.Source)
		entry.Name = normalizeTypedFileString(entry.Name)
		entry.Alias = normalizeTypedFileString(entry.Alias)
		entry.ImportType = normalizeTypedFileString(entry.ImportType)
		entry.Lang = normalizeTypedFileString(entry.Lang)
		entry.FullImportName = normalizeTypedFileString(entry.FullImportName)
		if entry != (typedFileImport{}) {
			kept = append(kept, entry)
		}
	}
	if len(kept) == 0 {
		return nil
	}
	return kept
}

// normalizeTypedFileCalls trims every row and drops all-empty rows.
func normalizeTypedFileCalls(rows []typedFileCall) []typedFileCall {
	kept := rows[:0]
	for _, entry := range rows {
		entry.FullName = normalizeTypedFileString(entry.FullName)
		entry.Name = normalizeTypedFileString(entry.Name)
		entry.Lang = normalizeTypedFileString(entry.Lang)
		entry.InferredObjType = normalizeTypedFileString(entry.InferredObjType)
		if entry != (typedFileCall{}) {
			kept = append(kept, entry)
		}
	}
	if len(kept) == 0 {
		return nil
	}
	return kept
}

// normalizeTypedFileSCIPCalls trims every row and drops all-empty rows.
func normalizeTypedFileSCIPCalls(rows []typedFileSCIPCall) []typedFileSCIPCall {
	kept := rows[:0]
	for _, entry := range rows {
		entry.Package = normalizeTypedFileString(entry.Package)
		entry.PackageName = normalizeTypedFileString(entry.PackageName)
		entry.ImportSource = normalizeTypedFileString(entry.ImportSource)
		entry.CalleePackage = normalizeTypedFileString(entry.CalleePackage)
		entry.CalleeSymbol = normalizeTypedFileString(entry.CalleeSymbol)
		entry.CalleeName = normalizeTypedFileString(entry.CalleeName)
		if entry != (typedFileSCIPCall{}) {
			kept = append(kept, entry)
		}
	}
	if len(kept) == 0 {
		return nil
	}
	return kept
}

// normalizeTypedFileEntities drops all-empty rows. Decorators arrive already
// trimmed and filtered by their decode.
func normalizeTypedFileEntities(rows []typedFileEntity) []typedFileEntity {
	kept := rows[:0]
	for _, entry := range rows {
		if len(entry.Decorators) != 0 {
			kept = append(kept, entry)
		}
	}
	if len(kept) == 0 {
		return nil
	}
	return kept
}

// typedFilePayloadFromMap converts a map-decoded payload into the used
// fields with the payloadcore helpers themselves, so the tolerant phase
// matches the map path by construction. All-empty bucket rows are dropped to
// match the normalized fast phase.
func typedFilePayloadFromMap(payload map[string]any) typedFilePayload {
	out := typedFilePayload{
		RepoID:       payloadcore.PayloadStr(payload, "repo_id"),
		RepositoryID: payloadcore.PayloadStr(payload, "repository_id"),
		RelativePath: payloadcore.PayloadStr(payload, "relative_path"),
	}
	fileData := payloadcore.PayloadMap(payload, "parsed_file_data")
	if fileData == nil {
		return out
	}
	parsed := &typedParsedFileData{
		Language: payloadcore.PayloadStr(fileData, "language"),
		Lang:     payloadcore.PayloadStr(fileData, "lang"),
	}
	for _, item := range payloadcore.MapSlice(fileData["imports"]) {
		entry := typedFileImport{
			Source:         payloadcore.PayloadStr(item, "source"),
			Name:           payloadcore.PayloadStr(item, "name"),
			Alias:          payloadcore.PayloadStr(item, "alias"),
			ImportType:     payloadcore.PayloadStr(item, "import_type"),
			Lang:           payloadcore.PayloadStr(item, "lang"),
			FullImportName: payloadcore.PayloadStr(item, "full_import_name"),
		}
		if entry != (typedFileImport{}) {
			parsed.Imports = append(parsed.Imports, entry)
		}
	}
	for _, item := range payloadcore.MapSlice(fileData["function_calls"]) {
		entry := typedFileCall{
			FullName:        payloadcore.PayloadStr(item, "full_name"),
			Name:            payloadcore.PayloadStr(item, "name"),
			Lang:            payloadcore.PayloadStr(item, "lang"),
			InferredObjType: payloadcore.PayloadStr(item, "inferred_obj_type"),
		}
		if entry != (typedFileCall{}) {
			parsed.Calls = append(parsed.Calls, entry)
		}
	}
	for _, item := range payloadcore.MapSlice(fileData["function_calls_scip"]) {
		entry := typedFileSCIPCall{
			Package:       payloadcore.PayloadStr(item, "package"),
			PackageName:   payloadcore.PayloadStr(item, "package_name"),
			ImportSource:  payloadcore.PayloadStr(item, "import_source"),
			CalleePackage: payloadcore.PayloadStr(item, "callee_package"),
			CalleeSymbol:  payloadcore.PayloadStr(item, "callee_symbol"),
			CalleeName:    payloadcore.PayloadStr(item, "callee_name"),
		}
		if entry != (typedFileSCIPCall{}) {
			parsed.SCIPCalls = append(parsed.SCIPCalls, entry)
		}
	}
	for _, item := range payloadcore.MapSlice(fileData["functions"]) {
		if decorators := typedFileStringSlice(payloadcore.SemanticPayloadStringSlice(item, "decorators")); len(decorators) != 0 {
			parsed.Functions = append(parsed.Functions, typedFileEntity{Decorators: decorators})
		}
	}
	for _, item := range payloadcore.MapSlice(fileData["classes"]) {
		if decorators := typedFileStringSlice(payloadcore.SemanticPayloadStringSlice(item, "decorators")); len(decorators) != 0 {
			parsed.Classes = append(parsed.Classes, typedFileEntity{Decorators: decorators})
		}
	}
	out.Parsed = parsed
	return out
}

// projectTypedFileEnvelope rebuilds the envelope the existing per-file bodies
// read, carrying ONLY the used keys. Parity between the index built from
// these envelopes and the index built from full map-decoded envelopes proves
// a typed decode of the used fields is sufficient.
func projectTypedFileEnvelope(typed typedFilePayload, factID, scopeID string, tombstone bool) facts.Envelope {
	payloadMap := map[string]any{
		"repo_id":       typed.RepoID,
		"repository_id": typed.RepositoryID,
		"relative_path": typed.RelativePath,
	}
	if typed.Parsed != nil {
		fileData := map[string]any{
			"language": typed.Parsed.Language,
			"lang":     typed.Parsed.Lang,
		}
		imports := make([]any, 0, len(typed.Parsed.Imports))
		for _, entry := range typed.Parsed.Imports {
			imports = append(imports, map[string]any{
				"source": entry.Source, "name": entry.Name, "alias": entry.Alias,
				"import_type": entry.ImportType, "lang": entry.Lang,
				"full_import_name": entry.FullImportName,
			})
		}
		fileData["imports"] = imports
		calls := make([]any, 0, len(typed.Parsed.Calls))
		for _, entry := range typed.Parsed.Calls {
			calls = append(calls, map[string]any{
				"full_name": entry.FullName, "name": entry.Name, "lang": entry.Lang,
				"inferred_obj_type": entry.InferredObjType,
			})
		}
		fileData["function_calls"] = calls
		scip := make([]any, 0, len(typed.Parsed.SCIPCalls))
		for _, entry := range typed.Parsed.SCIPCalls {
			scip = append(scip, map[string]any{
				"package": entry.Package, "package_name": entry.PackageName,
				"import_source": entry.ImportSource, "callee_package": entry.CalleePackage,
				"callee_symbol": entry.CalleeSymbol, "callee_name": entry.CalleeName,
			})
		}
		fileData["function_calls_scip"] = scip
		functions := make([]any, 0, len(typed.Parsed.Functions))
		for _, entity := range typed.Parsed.Functions {
			decorators := make([]any, 0, len(entity.Decorators))
			for _, decorator := range entity.Decorators {
				decorators = append(decorators, decorator)
			}
			functions = append(functions, map[string]any{"decorators": decorators})
		}
		fileData["functions"] = functions
		classes := make([]any, 0, len(typed.Parsed.Classes))
		for _, entity := range typed.Parsed.Classes {
			decorators := make([]any, 0, len(entity.Decorators))
			for _, decorator := range entity.Decorators {
				decorators = append(decorators, decorator)
			}
			classes = append(classes, map[string]any{"decorators": decorators})
		}
		fileData["classes"] = classes
		payloadMap["parsed_file_data"] = fileData
	}
	return facts.Envelope{
		FactID: factID, FactKind: factload.FactKindFile, ScopeID: scopeID,
		Payload: payloadMap, IsTombstone: tombstone,
	}
}

// DecodeTypedFileEnvelope is the storage seam over the typed file-payload
// decode: it decodes raw file-fact payload bytes into the used fields only
// and projects the trimmed envelope the per-file bodies read. FactID,
// ScopeID, and the tombstone bit travel outside the payload column, so the
// caller supplies them; generation, observed-at, and source-ref metadata
// stays with the caller's row scan. It errors only on malformed JSON.
func DecodeTypedFileEnvelope(raw []byte, factID, scopeID string, tombstone bool) (facts.Envelope, error) {
	typed, err := decodeTypedFilePayload(raw)
	if err != nil {
		return facts.Envelope{}, err
	}
	return projectTypedFileEnvelope(typed, factID, scopeID, tombstone), nil
}
