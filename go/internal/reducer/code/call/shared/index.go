// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package shared

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// entityIndexCandidates bundles the ambiguity-tracking accumulator maps
// BuildEntityIndex fills while scanning "file" facts, before collapsing each
// one to its unique-only result in finalizeEntityIndex.
type entityIndexCandidates struct {
	nameCandidates               map[string]map[string]map[string]struct{}
	repoPathNameCandidates       map[string]map[string]map[string]map[string]struct{}
	repoNameCandidates           map[string]map[string]map[string]struct{}
	repoDirNameCandidates        map[string]map[string]map[string]map[string]struct{}
	goMethodReturnTypeCandidates map[string]map[string]map[string]struct{}
	rustTraitMethodCandidates    map[string]map[string]map[string]struct{}
	symbolCandidates             map[string]map[string]codeCallSymbolResolution
	typeScriptCandidates         typeScriptIndexCandidates
	receiverMethodCandidates     receiverMethodCandidates
	pythonClassBaseCandidates    map[string]map[string]map[string]pythonClassBaseCandidate
}

func newEntityIndexCandidates() entityIndexCandidates {
	return entityIndexCandidates{
		nameCandidates:               make(map[string]map[string]map[string]struct{}),
		repoPathNameCandidates:       make(map[string]map[string]map[string]map[string]struct{}),
		repoNameCandidates:           make(map[string]map[string]map[string]struct{}),
		repoDirNameCandidates:        make(map[string]map[string]map[string]map[string]struct{}),
		goMethodReturnTypeCandidates: make(map[string]map[string]map[string]struct{}),
		rustTraitMethodCandidates:    make(map[string]map[string]map[string]struct{}),
		symbolCandidates:             make(map[string]map[string]codeCallSymbolResolution),
		typeScriptCandidates:         newTypeScriptIndexCandidates(),
		receiverMethodCandidates:     newReceiverMethodCandidates(),
		pythonClassBaseCandidates:    make(map[string]map[string]map[string]pythonClassBaseCandidate),
	}
}

// BuildEntityIndex scans "file" facts in envelopes and builds the EntityIndex
// used to resolve call edges to entity IDs: functions, classes, structs,
// interfaces, and type aliases, keyed by path/line, by unique name within a
// path or repository, and by stable symbol key. Non-"file" facts are ignored.
// Names that are ambiguous within their scope (path, repository, or
// repository directory) are omitted rather than resolved incorrectly.
func BuildEntityIndex(envelopes []facts.Envelope) EntityIndex {
	index := EntityIndex{
		entitiesByRepoPathLine:  make(map[string]map[string]string),
		spansByFile:             make(map[string]map[string][]FunctionSpan),
		containersByFile:        make(map[string]map[string][]FunctionSpan),
		uniqueNameByPath:        make(map[string]map[string]string),
		uniqueNameByRepoPath:    make(map[string]map[string]map[string]string),
		uniqueNameByRepo:        make(map[string]map[string]string),
		uniqueNameByRepoDir:     make(map[string]map[string]map[string]string),
		constructorByRepoPath:   make(map[string]map[string]map[string]string),
		goMethodReturnTypes:     make(map[string]map[string]string),
		rustTraitMethodsByRepo:  make(map[string]map[string]string),
		pythonClassBasesByRepo:  make(map[string]map[string][]string),
		entityFileByID:          make(map[string]string),
		entityTypeByID:          make(map[string]string),
		entityByStableSymbolKey: make(map[string]codeCallSymbolResolution),
		javaScriptAliasesByFile: make(map[string]map[string][]javaScriptStaticAliasSpan),
	}
	index.repositoryImportPathsByRepo = make(map[string][]string)
	candidates := newEntityIndexCandidates()

	for _, env := range envelopes {
		if env.FactKind != "file" {
			continue
		}

		fileData, ok := env.Payload["parsed_file_data"].(map[string]any)
		if !ok {
			continue
		}

		relativePath := payloadcore.PayloadStr(env.Payload, "relative_path")
		rawPath := payloadcore.AnyToString(fileData["path"])
		repositoryID := payloadcore.PayloadStr(env.Payload, "repo_id")
		preferredPath := PreferredPath(rawPath, relativePath)
		// JavaScript alias parsing is cached once per function source because
		// generated bundles can carry thousands of dynamic call records.
		shouldCacheJavaScriptAliases := codeCallJavaScriptSourceFile(fileData, rawPath, relativePath)

		addFunctionEntityCandidates(
			&index, &candidates, fileData, rawPath, relativePath, repositoryID, preferredPath, shouldCacheJavaScriptAliases,
		)
		addTypeEntityCandidates(&index, &candidates, fileData, rawPath, relativePath, repositoryID, preferredPath)
	}

	finalizeEntityIndex(&index, &candidates, envelopes)
	return index
}

// addFunctionEntityCandidates processes one file's "functions" bucket: it
// records each function's path/line entry, span, symbol keys, and candidate
// names into index and candidates.
func addFunctionEntityCandidates(
	index *EntityIndex,
	candidates *entityIndexCandidates,
	fileData map[string]any,
	rawPath string,
	relativePath string,
	repositoryID string,
	preferredPath string,
	shouldCacheJavaScriptAliases bool,
) {
	for _, item := range payloadcore.MapSlice(fileData["functions"]) {
		entityID := payloadcore.AnyToString(item["uid"])
		startLine := PayloadInt(item["line_number"], item["start_line"])
		endLine := PayloadInt(item["end_line"])
		if startLine <= 0 {
			continue
		}
		if endLine < startLine {
			endLine = startLine
		}
		fullKey, relativeKey := FileKeys(rawPath, relativePath)
		if shouldCacheJavaScriptAliases {
			cacheJavaScriptStaticAliasSpan(
				*index,
				repositoryID,
				fullKey,
				relativeKey,
				startLine,
				endLine,
				payloadcore.AnyToString(item["source"]),
			)
		}
		if entityID == "" {
			continue
		}
		addCodeCallSymbolCandidates(candidates.symbolCandidates, item, entityID)
		if preferredPath != "" {
			index.entityFileByID[entityID] = preferredPath
		}
		index.entityTypeByID[entityID] = "Function"
		span := FunctionSpan{
			StartLine: startLine,
			EndLine:   endLine,
			EntityID:  entityID,
			names:     codeCallFunctionCandidateNames(item),
		}
		addFileSpan(index.spansByFile, repositoryID, fullKey, relativeKey, span)
		addFileSpan(index.containersByFile, repositoryID, fullKey, relativeKey, span)
		addRepoPathEntityCandidates(index, candidates, repositoryID, fullKey, relativeKey, startLine, item, entityID)
		for _, pathKey := range PathKeys(rawPath, relativePath) {
			for _, candidateName := range codeCallFunctionCandidateNames(item) {
				addNameCandidate(candidates.nameCandidates, pathKey, candidateName, entityID)
				if repositoryID != "" {
					addNameCandidate(candidates.repoNameCandidates, repositoryID, candidateName, entityID)
					addCodeCallRepoDirNameCandidate(candidates.repoDirNameCandidates, repositoryID, preferredPath, candidateName, entityID)
				}
			}
			addGoMethodReturnTypeCandidate(candidates.goMethodReturnTypeCandidates, repositoryID, item)
			candidates.typeScriptCandidates.addFunction(repositoryID, item, entityID)
			candidates.receiverMethodCandidates.add(repositoryID, item, entityID)
			addRustTraitMethodCandidate(candidates.rustTraitMethodCandidates, repositoryID, item, entityID)
		}
	}
}

// addTypeEntityCandidates processes one file's classes/structs/interfaces/
// type_aliases buckets: it records each declaration's container span, symbol
// keys, and candidate names into index and candidates.
func addTypeEntityCandidates(
	index *EntityIndex,
	candidates *entityIndexCandidates,
	fileData map[string]any,
	rawPath string,
	relativePath string,
	repositoryID string,
	preferredPath string,
) {
	for _, bucket := range []string{"classes", "structs", "interfaces", "type_aliases"} {
		for _, item := range payloadcore.MapSlice(fileData[bucket]) {
			entityID := payloadcore.AnyToString(item["uid"])
			if entityID == "" {
				continue
			}
			fullKey, relativeKey := FileKeys(rawPath, relativePath)
			for _, fileKey := range [2]string{fullKey, relativeKey} {
				if fileKey == "" {
					continue
				}
				for _, candidateName := range codeCallTypeCandidateNames(item) {
					addCodeCallRepoPathNameCandidate(candidates.repoPathNameCandidates, repositoryID, fileKey, candidateName, entityID)
				}
			}
			addCodeCallSymbolCandidates(candidates.symbolCandidates, item, entityID)
			if preferredPath != "" {
				index.entityFileByID[entityID] = preferredPath
			}
			index.entityTypeByID[entityID] = codeCallEntityTypeForBucket(bucket)
			if startLine := PayloadInt(item["line_number"], item["start_line"]); startLine > 0 {
				endLine := PayloadInt(item["end_line"])
				if endLine < startLine {
					endLine = startLine
				}
				addFileSpan(index.containersByFile, repositoryID, fullKey, relativeKey, FunctionSpan{
					StartLine: startLine,
					EndLine:   endLine,
					EntityID:  entityID,
					names:     codeCallTypeCandidateNames(item),
				})
			}
			for _, pathKey := range PathKeys(rawPath, relativePath) {
				for _, candidateName := range codeCallTypeCandidateNames(item) {
					addNameCandidate(candidates.nameCandidates, pathKey, candidateName, entityID)
					if repositoryID != "" {
						addNameCandidate(candidates.repoNameCandidates, repositoryID, candidateName, entityID)
						addCodeCallRepoDirNameCandidate(candidates.repoDirNameCandidates, repositoryID, preferredPath, candidateName, entityID)
					}
				}
			}
			if bucket == "classes" {
				addPythonClassBaseCandidate(candidates.pythonClassBaseCandidates, repositoryID, item)
			}
			candidates.typeScriptCandidates.addType(bucket, repositoryID, item)
		}
	}
}

// addNameCandidate records that entityID is a candidate for candidateName
// within scope (a path key or a repository ID); the same helper backs both
// the per-path and per-repository candidate maps.
func addNameCandidate(
	candidates map[string]map[string]map[string]struct{},
	scope string,
	candidateName string,
	entityID string,
) {
	if _, ok := candidates[scope]; !ok {
		candidates[scope] = make(map[string]map[string]struct{})
	}
	if _, ok := candidates[scope][candidateName]; !ok {
		candidates[scope][candidateName] = make(map[string]struct{})
	}
	candidates[scope][candidateName][entityID] = struct{}{}
}

// addRepoPathEntityCandidates records one function's repository- and
// file-scoped entries: its path/line identity, its constructor binding when
// it declares one, and its candidate names. All three are stored under the
// repository ID plus the file's own keys only (see [FileKeys]), never under
// a bare file name, so same-file lookups cannot reach a same-named file
// elsewhere (#7642).
func addRepoPathEntityCandidates(
	index *EntityIndex,
	candidates *entityIndexCandidates,
	repositoryID string,
	fullKey string,
	relativeKey string,
	startLine int,
	item map[string]any,
	entityID string,
) {
	candidateNames := codeCallFunctionCandidateNames(item)
	for _, fileKey := range [2]string{fullKey, relativeKey} {
		if fileKey == "" {
			continue
		}
		if _, ok := index.entitiesByRepoPathLine[repositoryID]; !ok {
			index.entitiesByRepoPathLine[repositoryID] = make(map[string]string)
		}
		index.entitiesByRepoPathLine[repositoryID][codeCallPathLineKey(fileKey, startLine)] = entityID
		if name := payloadcore.AnyToString(item["name"]); name == "constructor" || name == "__init__" {
			if classContext := strings.TrimSpace(payloadcore.AnyToString(item["class_context"])); classContext != "" {
				if _, ok := index.constructorByRepoPath[repositoryID]; !ok {
					index.constructorByRepoPath[repositoryID] = make(map[string]map[string]string)
				}
				if _, ok := index.constructorByRepoPath[repositoryID][fileKey]; !ok {
					index.constructorByRepoPath[repositoryID][fileKey] = make(map[string]string)
				}
				index.constructorByRepoPath[repositoryID][fileKey][classContext] = entityID
			}
		}
		for _, candidateName := range candidateNames {
			addCodeCallRepoPathNameCandidate(candidates.repoPathNameCandidates, repositoryID, fileKey, candidateName, entityID)
		}
	}
}

// addCodeCallRepoPathNameCandidate records that entityID is a candidate for
// candidateName within one file (repositoryID plus the file's own key); the
// finalize step keeps the name only when exactly one entity claims it. An
// empty repositoryID is stored as-is, so a repository-less fixture still
// resolves within its own file keys.
func addCodeCallRepoPathNameCandidate(
	candidates map[string]map[string]map[string]map[string]struct{},
	repositoryID string,
	fileKey string,
	candidateName string,
	entityID string,
) {
	if fileKey == "" || candidateName == "" || entityID == "" {
		return
	}
	if _, ok := candidates[repositoryID]; !ok {
		candidates[repositoryID] = make(map[string]map[string]map[string]struct{})
	}
	if _, ok := candidates[repositoryID][fileKey]; !ok {
		candidates[repositoryID][fileKey] = make(map[string]map[string]struct{})
	}
	if _, ok := candidates[repositoryID][fileKey][candidateName]; !ok {
		candidates[repositoryID][fileKey][candidateName] = make(map[string]struct{})
	}
	candidates[repositoryID][fileKey][candidateName][entityID] = struct{}{}
}

// finalizeEntityIndex sorts every span slice into ascending line order and
// collapses each ambiguity-tracking candidate map in candidates to its
// unique-only result on index.
func finalizeEntityIndex(index *EntityIndex, candidates *entityIndexCandidates, envelopes []facts.Envelope) {
	sortSpansByFile(index.spansByFile)
	sortSpansByFile(index.containersByFile)
	for _, files := range index.javaScriptAliasesByFile {
		for _, spans := range files {
			sort.Slice(spans, func(i, j int) bool {
				if spans[i].StartLine == spans[j].StartLine {
					return spans[i].EndLine < spans[j].EndLine
				}
				return spans[i].StartLine < spans[j].StartLine
			})
		}
	}

	for pathKey, names := range candidates.nameCandidates {
		index.uniqueNameByPath[pathKey] = make(map[string]string, len(names))
		for name, entityIDs := range names {
			if len(entityIDs) != 1 {
				continue
			}
			for entityID := range entityIDs {
				index.uniqueNameByPath[pathKey][name] = entityID
			}
		}
	}
	for repositoryID, names := range candidates.repoNameCandidates {
		index.uniqueNameByRepo[repositoryID] = make(map[string]string, len(names))
		for name, entityIDs := range names {
			if len(entityIDs) != 1 {
				continue
			}
			for entityID := range entityIDs {
				index.uniqueNameByRepo[repositoryID][name] = entityID
			}
		}
	}
	for repositoryID, files := range candidates.repoPathNameCandidates {
		index.uniqueNameByRepoPath[repositoryID] = make(map[string]map[string]string, len(files))
		for fileKey, names := range files {
			index.uniqueNameByRepoPath[repositoryID][fileKey] = make(map[string]string, len(names))
			for name, entityIDs := range names {
				if len(entityIDs) != 1 {
					continue
				}
				for entityID := range entityIDs {
					index.uniqueNameByRepoPath[repositoryID][fileKey][name] = entityID
				}
			}
		}
	}
	for repositoryID, dirs := range candidates.repoDirNameCandidates {
		index.uniqueNameByRepoDir[repositoryID] = uniqueCodeCallNamesByDirectory(dirs)
	}
	for repositoryID, methods := range candidates.goMethodReturnTypeCandidates {
		index.goMethodReturnTypes[repositoryID] = make(map[string]string, len(methods))
		for methodName, returnTypes := range methods {
			if len(returnTypes) != 1 {
				continue
			}
			for returnType := range returnTypes {
				index.goMethodReturnTypes[repositoryID][methodName] = returnType
			}
		}
	}
	index.rustTraitMethodsByRepo = uniqueRustTraitMethodCandidates(candidates.rustTraitMethodCandidates)
	index.pythonClassBasesByRepo = uniquePythonClassBasesByRepo(candidates.pythonClassBaseCandidates)
	index.typeScriptInterfaceMethodsByRepo = candidates.typeScriptCandidates.uniqueMethods()
	index.receiverMethodsByRepo = candidates.receiverMethodCandidates.unique()
	index.entityByStableSymbolKey = uniqueCodeCallSymbolCandidates(candidates.symbolCandidates)
	index.goExportByImportPath = buildGoCrossRepoExportIndex(envelopes)
}

func sortSpansByFile(spansByFile map[string]map[string][]FunctionSpan) {
	for _, files := range spansByFile {
		for _, spans := range files {
			sort.Slice(spans, func(i, j int) bool {
				if spans[i].StartLine == spans[j].StartLine {
					return spans[i].EndLine < spans[j].EndLine
				}
				return spans[i].StartLine < spans[j].StartLine
			})
		}
	}
}

func addCodeCallRepoDirNameCandidate(candidates map[string]map[string]map[string]map[string]struct{}, repositoryID, filePath, name, entityID string) {
	dir := DirectoryKey(filePath)
	if repositoryID == "" || name == "" || entityID == "" {
		return
	}
	if _, ok := candidates[repositoryID]; !ok {
		candidates[repositoryID] = make(map[string]map[string]map[string]struct{})
	}
	if _, ok := candidates[repositoryID][dir]; !ok {
		candidates[repositoryID][dir] = make(map[string]map[string]struct{})
	}
	if _, ok := candidates[repositoryID][dir][name]; !ok {
		candidates[repositoryID][dir][name] = make(map[string]struct{})
	}
	candidates[repositoryID][dir][name][entityID] = struct{}{}
}

// DirectoryKey returns the normalized, slash-separated directory containing
// filePath, or "" when filePath is empty or already the repository root.
func DirectoryKey(filePath string) string {
	normalized := NormalizePath(filePath)
	if normalized == "" || normalized == "." {
		return ""
	}
	if !strings.Contains(normalized, "/") {
		return "."
	}
	return NormalizePath(filepath.Dir(normalized))
}

// ResolveEntityID resolves the entity declared at pathValue/lineValue via the
// exact path/line index, or "" when lineValue is not a positive line number
// or no entity is recorded there.
//
// The lookup uses only the file's own identity within repositoryID: the
// normalized pathValue. It never consults a bare file name or another
// repository's path, so a path/line with no declaration in its own file
// resolves to "" instead of borrowing an entity from a same-named file
// elsewhere (#7642).
func ResolveEntityID(index EntityIndex, repositoryID string, pathValue any, lineValue any) string {
	line := PayloadInt(lineValue)
	if line <= 0 {
		return ""
	}

	fullKey, _ := FileKeys(payloadcore.AnyToString(pathValue), "")
	if fullKey == "" {
		return ""
	}
	return index.entitiesByRepoPathLine[repositoryID][codeCallPathLineKey(fullKey, line)]
}
