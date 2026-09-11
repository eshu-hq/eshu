// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package call

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// BuildEntityIndex scans "file" facts in envelopes and builds the EntityIndex
// used to resolve call edges to entity IDs: functions, classes, structs,
// interfaces, and type aliases, keyed by path/line, by unique name within a
// path or repository, and by stable symbol key. Non-"file" facts are ignored.
// Names that are ambiguous within their scope (path, repository, or
// repository directory) are omitted rather than resolved incorrectly.
func BuildEntityIndex(envelopes []facts.Envelope) EntityIndex {
	index := EntityIndex{
		entitiesByPathLine:      make(map[string]string),
		spansByPath:             make(map[string][]codeFunctionSpan),
		containersByPath:        make(map[string][]codeFunctionSpan),
		UniqueNameByPath:        make(map[string]map[string]string),
		UniqueNameByRepo:        make(map[string]map[string]string),
		uniqueNameByRepoDir:     make(map[string]map[string]map[string]string),
		constructorByPath:       make(map[string]map[string]string),
		goMethodReturnTypes:     make(map[string]map[string]string),
		rustTraitMethodsByRepo:  make(map[string]map[string]string),
		pythonClassBasesByRepo:  make(map[string]map[string][]string),
		entityFileByID:          make(map[string]string),
		entityTypeByID:          make(map[string]string),
		entityByStableSymbolKey: make(map[string]codeCallSymbolResolution),
		javaScriptAliasesByPath: make(map[string][]javaScriptStaticAliasSpan),
	}
	index.repositoryImportPathsByRepo = make(map[string][]string)
	nameCandidates := make(map[string]map[string]map[string]struct{})
	repoNameCandidates := make(map[string]map[string]map[string]struct{})
	repoDirNameCandidates := make(map[string]map[string]map[string]map[string]struct{})
	goMethodReturnTypeCandidates := make(map[string]map[string]map[string]struct{})
	rustTraitMethodCandidates := make(map[string]map[string]map[string]struct{})
	symbolCandidates := make(map[string]map[string]codeCallSymbolResolution)
	typeScriptCandidates := newTypeScriptIndexCandidates()
	receiverMethodCandidates := newReceiverMethodCandidates()
	pythonClassBaseCandidates := make(map[string]map[string]map[string]pythonClassBaseCandidate)

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
		preferredPath := codeCallPreferredPath(rawPath, relativePath)
		// JavaScript alias parsing is cached once per function source because
		// generated bundles can carry thousands of dynamic call records.
		shouldCacheJavaScriptAliases := codeCallJavaScriptSourceFile(fileData, rawPath, relativePath)
		for _, item := range mapSlice(fileData["functions"]) {
			entityID := payloadcore.AnyToString(item["uid"])
			startLine := PayloadInt(item["line_number"], item["start_line"])
			endLine := PayloadInt(item["end_line"])
			if startLine <= 0 {
				continue
			}
			if endLine < startLine {
				endLine = startLine
			}
			if shouldCacheJavaScriptAliases {
				cacheJavaScriptStaticAliasSpan(
					index,
					PathKeys(rawPath, relativePath),
					startLine,
					endLine,
					payloadcore.AnyToString(item["source"]),
				)
			}
			if entityID == "" {
				continue
			}
			addCodeCallSymbolCandidates(symbolCandidates, item, entityID)
			if preferredPath != "" {
				index.entityFileByID[entityID] = preferredPath
			}
			index.entityTypeByID[entityID] = "Function"
			for _, pathKey := range PathKeys(rawPath, relativePath) {
				index.entitiesByPathLine[codeCallPathLineKey(pathKey, startLine)] = entityID
				span := codeFunctionSpan{
					startLine: startLine,
					endLine:   endLine,
					entityID:  entityID,
					names:     codeCallFunctionCandidateNames(item),
				}
				index.spansByPath[pathKey] = append(index.spansByPath[pathKey], span)
				index.containersByPath[pathKey] = append(index.containersByPath[pathKey], span)
				if name := payloadcore.AnyToString(item["name"]); name == "constructor" || name == "__init__" {
					classContext := strings.TrimSpace(payloadcore.AnyToString(item["class_context"]))
					if classContext != "" {
						if _, ok := index.constructorByPath[pathKey]; !ok {
							index.constructorByPath[pathKey] = make(map[string]string)
						}
						index.constructorByPath[pathKey][classContext] = entityID
					}
				}
				for _, candidateName := range codeCallFunctionCandidateNames(item) {
					if _, ok := nameCandidates[pathKey]; !ok {
						nameCandidates[pathKey] = make(map[string]map[string]struct{})
					}
					if _, ok := nameCandidates[pathKey][candidateName]; !ok {
						nameCandidates[pathKey][candidateName] = make(map[string]struct{})
					}
					nameCandidates[pathKey][candidateName][entityID] = struct{}{}
					if repositoryID != "" {
						if _, ok := repoNameCandidates[repositoryID]; !ok {
							repoNameCandidates[repositoryID] = make(map[string]map[string]struct{})
						}
						if _, ok := repoNameCandidates[repositoryID][candidateName]; !ok {
							repoNameCandidates[repositoryID][candidateName] = make(map[string]struct{})
						}
						repoNameCandidates[repositoryID][candidateName][entityID] = struct{}{}
						addCodeCallRepoDirNameCandidate(repoDirNameCandidates, repositoryID, preferredPath, candidateName, entityID)
					}
				}
				addGoMethodReturnTypeCandidate(goMethodReturnTypeCandidates, repositoryID, item)
				typeScriptCandidates.addFunction(repositoryID, item, entityID)
				receiverMethodCandidates.add(repositoryID, item, entityID)
				addRustTraitMethodCandidate(rustTraitMethodCandidates, repositoryID, item, entityID)
			}
		}
		for _, bucket := range []string{"classes", "structs", "interfaces", "type_aliases"} {
			for _, item := range mapSlice(fileData[bucket]) {
				entityID := payloadcore.AnyToString(item["uid"])
				if entityID == "" {
					continue
				}
				addCodeCallSymbolCandidates(symbolCandidates, item, entityID)
				if preferredPath != "" {
					index.entityFileByID[entityID] = preferredPath
				}
				index.entityTypeByID[entityID] = codeCallEntityTypeForBucket(bucket)
				for _, pathKey := range PathKeys(rawPath, relativePath) {
					startLine := PayloadInt(item["line_number"], item["start_line"])
					endLine := PayloadInt(item["end_line"])
					if startLine > 0 {
						if endLine < startLine {
							endLine = startLine
						}
						index.containersByPath[pathKey] = append(index.containersByPath[pathKey], codeFunctionSpan{
							startLine: startLine,
							endLine:   endLine,
							entityID:  entityID,
							names:     codeCallTypeCandidateNames(item),
						})
					}
					for _, candidateName := range codeCallTypeCandidateNames(item) {
						if _, ok := nameCandidates[pathKey]; !ok {
							nameCandidates[pathKey] = make(map[string]map[string]struct{})
						}
						if _, ok := nameCandidates[pathKey][candidateName]; !ok {
							nameCandidates[pathKey][candidateName] = make(map[string]struct{})
						}
						nameCandidates[pathKey][candidateName][entityID] = struct{}{}
						if repositoryID != "" {
							if _, ok := repoNameCandidates[repositoryID]; !ok {
								repoNameCandidates[repositoryID] = make(map[string]map[string]struct{})
							}
							if _, ok := repoNameCandidates[repositoryID][candidateName]; !ok {
								repoNameCandidates[repositoryID][candidateName] = make(map[string]struct{})
							}
							repoNameCandidates[repositoryID][candidateName][entityID] = struct{}{}
							addCodeCallRepoDirNameCandidate(repoDirNameCandidates, repositoryID, preferredPath, candidateName, entityID)
						}
					}
				}
				if bucket == "classes" {
					addPythonClassBaseCandidate(pythonClassBaseCandidates, repositoryID, item)
				}
				typeScriptCandidates.addType(bucket, repositoryID, item)
			}
		}
	}

	for pathKey, spans := range index.spansByPath {
		sort.Slice(spans, func(i, j int) bool {
			if spans[i].startLine == spans[j].startLine {
				return spans[i].endLine < spans[j].endLine
			}
			return spans[i].startLine < spans[j].startLine
		})
		index.spansByPath[pathKey] = spans
	}
	for pathKey, spans := range index.containersByPath {
		sort.Slice(spans, func(i, j int) bool {
			if spans[i].startLine == spans[j].startLine {
				return spans[i].endLine < spans[j].endLine
			}
			return spans[i].startLine < spans[j].startLine
		})
		index.containersByPath[pathKey] = spans
	}
	for pathKey, spans := range index.javaScriptAliasesByPath {
		sort.Slice(spans, func(i, j int) bool {
			if spans[i].startLine == spans[j].startLine {
				return spans[i].endLine < spans[j].endLine
			}
			return spans[i].startLine < spans[j].startLine
		})
		index.javaScriptAliasesByPath[pathKey] = spans
	}
	for pathKey, names := range nameCandidates {
		index.UniqueNameByPath[pathKey] = make(map[string]string, len(names))
		for name, entityIDs := range names {
			if len(entityIDs) != 1 {
				continue
			}
			for entityID := range entityIDs {
				index.UniqueNameByPath[pathKey][name] = entityID
			}
		}
	}
	for repositoryID, names := range repoNameCandidates {
		index.UniqueNameByRepo[repositoryID] = make(map[string]string, len(names))
		for name, entityIDs := range names {
			if len(entityIDs) != 1 {
				continue
			}
			for entityID := range entityIDs {
				index.UniqueNameByRepo[repositoryID][name] = entityID
			}
		}
	}
	for repositoryID, dirs := range repoDirNameCandidates {
		index.uniqueNameByRepoDir[repositoryID] = uniqueCodeCallNamesByDirectory(dirs)
	}
	for repositoryID, methods := range goMethodReturnTypeCandidates {
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
	index.rustTraitMethodsByRepo = uniqueRustTraitMethodCandidates(rustTraitMethodCandidates)
	index.pythonClassBasesByRepo = uniquePythonClassBasesByRepo(pythonClassBaseCandidates)
	index.typeScriptInterfaceMethodsByRepo = typeScriptCandidates.uniqueMethods()
	index.receiverMethodsByRepo = receiverMethodCandidates.unique()
	index.entityByStableSymbolKey = uniqueCodeCallSymbolCandidates(symbolCandidates)
	index.goExportByImportPath = buildGoCrossRepoExportIndex(envelopes)
	return index
}

func addCodeCallRepoDirNameCandidate(candidates map[string]map[string]map[string]map[string]struct{}, repositoryID, filePath, name, entityID string) {
	dir := codeCallDirectoryKey(filePath)
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

func codeCallDirectoryKey(filePath string) string {
	normalized := normalizeCodeCallPath(filePath)
	if normalized == "" || normalized == "." {
		return ""
	}
	if !strings.Contains(normalized, "/") {
		return "."
	}
	return normalizeCodeCallPath(filepath.Dir(normalized))
}

func resolveCodeEntityID(index EntityIndex, pathValue any, lineValue any) string {
	line := PayloadInt(lineValue)
	if line <= 0 {
		return ""
	}

	for _, pathKey := range PathKeys(payloadcore.AnyToString(pathValue), "") {
		if entityID := index.entitiesByPathLine[codeCallPathLineKey(pathKey, line)]; entityID != "" {
			return entityID
		}
	}
	return ""
}
