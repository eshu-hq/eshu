package projector

import (
	"path"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/content"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// buildCanonicalMaterialization extracts canonical graph materialization data
// from a set of fact envelopes. The returned CanonicalMaterialization carries
// all node and edge writes needed to project one repository generation into
// the canonical Neo4j graph.
func buildCanonicalMaterialization(
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
	inputFacts []facts.Envelope,
) CanonicalMaterialization {
	mat := CanonicalMaterialization{
		ScopeID:         scopeValue.ScopeID,
		GenerationID:    generation.GenerationID,
		RepoID:          scopeValue.Metadata["repo_id"],
		RepoPath:        scopeValue.Metadata["repo_path"],
		FirstGeneration: !scopeValue.HasPriorGeneration(),
	}

	if len(inputFacts) == 0 {
		return mat
	}

	// Extract repository.
	mat.Repository = extractRepository(inputFacts)
	if mat.Repository != nil {
		if mat.Repository.RepoID != "" {
			mat.RepoID = mat.Repository.RepoID
		}
		if mat.Repository.Path != "" {
			mat.RepoPath = mat.Repository.Path
		}
	}

	repoID := mat.RepoID
	repoPath := mat.RepoPath

	// Extract files.
	mat.Files = extractFiles(inputFacts, repoID, repoPath)

	// Build directory chain from file paths.
	mat.Directories = buildDirectoryChain(mat.Files, repoPath, repoID)

	// Extract entities.
	mat.Entities = extractEntities(inputFacts, repoID, repoPath)

	// Extract modules from Module-type entity facts. The Go parser emits
	// Module entities as content_entity facts (entity_type=Module) rather
	// than separate import/module facts with module_name payload keys.
	mat.Modules = extractModulesFromEntities(inputFacts)

	// Extract modules, imports, parameters, class members, nested functions
	// from all non-tombstoned facts. This handles Python-era payload keys
	// (module_name, imported_module, param_name, etc.) and merges any
	// additionally discovered modules into the set above.
	extractRelationships(inputFacts, &mat)
	extractTerraformStateRows(&mat, inputFacts)

	return mat
}

// extractRepository builds a RepositoryRow from the first RepositoryObserved
// fact envelope.
func extractRepository(envelopes []facts.Envelope) *RepositoryRow {
	repoFacts := FilterRepositoryFacts(envelopes)
	if len(repoFacts) == 0 {
		return nil
	}

	p := repoFacts[0].Payload
	repoID, _ := payloadString(p, "repo_id")
	name, _ := payloadString(p, "name")
	repoPath, _ := payloadString(p, "path")
	localPath, _ := payloadString(p, "local_path")
	remoteURL, _ := payloadString(p, "remote_url")
	repoSlug, _ := payloadString(p, "repo_slug")

	// The collector does not emit "path" — fall back to local_path which is
	// unique per repository and satisfies the Repository.path constraint.
	if repoPath == "" {
		repoPath = localPath
	}

	// The collector does not emit "has_remote" — derive from remote_url
	// presence which the collector sets when the repository has an origin.
	hasRemote := false
	if ptr := payloadBoolPtr(p, "has_remote"); ptr != nil {
		hasRemote = *ptr
	} else {
		hasRemote = remoteURL != ""
	}

	return &RepositoryRow{
		RepoID:    repoID,
		Name:      name,
		Path:      repoPath,
		LocalPath: localPath,
		RemoteURL: remoteURL,
		RepoSlug:  repoSlug,
		HasRemote: hasRemote,
	}
}

// extractFiles builds FileRow entries from file fact envelopes, skipping
// tombstones. The canonical path (used as the Neo4j MERGE key) is
// repoPath/relative_path — a globally unique path that prevents cross-repo
// constraint collisions. The relative_path is preserved separately for display.
func extractFiles(envelopes []facts.Envelope, repoID, repoPath string) []FileRow {
	fileFacts := FilterFileFacts(envelopes)
	var rows []FileRow

	for i := range fileFacts {
		if fileFacts[i].IsTombstone {
			continue
		}

		p := fileFacts[i].Payload
		relativePath, _ := payloadString(p, "relative_path")
		if relativePath == "" {
			continue
		}

		fullPath := qualifyPath(repoPath, relativePath)
		name := path.Base(relativePath)
		language, _ := payloadString(p, "language")
		dirPath := path.Dir(fullPath)

		rows = append(rows, FileRow{
			Path:         fullPath,
			RelativePath: relativePath,
			Name:         name,
			Language:     language,
			RepoID:       repoID,
			DirPath:      dirPath,
		})
	}

	return rows
}

// buildDirectoryChain walks file paths to produce a deduped, depth-sorted
// list of DirectoryRow entries. Directories are computed relative to the
// repository root path.
func buildDirectoryChain(files []FileRow, repoPath string, repoID string) []DirectoryRow {
	if len(files) == 0 {
		return nil
	}

	seen := make(map[string]struct{})
	var dirs []DirectoryRow

	for _, f := range files {
		dirPath := f.DirPath
		// Walk from the file's parent directory up to (but not including)
		// the repository root.
		for dirPath != "" && dirPath != repoPath && dirPath != "." && dirPath != "/" {
			if _, ok := seen[dirPath]; ok {
				break // already recorded this dir and all its ancestors
			}
			seen[dirPath] = struct{}{}

			parentPath := path.Dir(dirPath)
			dirName := path.Base(dirPath)

			// Compute depth: number of path segments between repoPath and
			// dirPath (0-indexed from the first level under repo).
			rel := strings.TrimPrefix(dirPath, repoPath+"/")
			depth := strings.Count(rel, "/")

			dirs = append(dirs, DirectoryRow{
				Path:       dirPath,
				Name:       dirName,
				ParentPath: parentPath,
				RepoID:     repoID,
				Depth:      depth,
			})

			dirPath = parentPath
		}
	}

	sort.Slice(dirs, func(i, j int) bool {
		if dirs[i].Depth != dirs[j].Depth {
			return dirs[i].Depth < dirs[j].Depth
		}
		return dirs[i].Path < dirs[j].Path
	})

	return dirs
}

// extractEntities builds EntityRow entries from ParsedEntityObserved fact
// envelopes. Entities with unmapped types or tombstoned facts are skipped.
// FilePath is set to the repo-qualified full path (repoPath/relative_path) so
// it matches File.path and avoids cross-repo constraint collisions.
func extractEntities(envelopes []facts.Envelope, repoID, repoPath string) []EntityRow {
	entityFacts := FilterEntityFacts(envelopes)
	var rows []EntityRow

	for i := range entityFacts {
		if entityFacts[i].IsTombstone {
			continue
		}

		p := entityFacts[i].Payload

		entityType, _ := payloadString(p, "entity_type")
		if entityType == "" {
			continue
		}

		label, ok := EntityTypeLabel(entityType)
		if !ok {
			continue
		}

		// Module and Parameter have dedicated write phases (F and G) that use
		// different MERGE keys (name for Module, composite key for Parameter).
		// Writing them through the generic entity phase (E) which MERGEs by
		// uid would violate their uniqueness constraints.
		if label == "Module" || label == "Parameter" {
			continue
		}

		entityName, _ := payloadString(p, "entity_name")
		relativePath, _ := payloadString(p, "relative_path")
		startLine, _ := payloadInt(p, "start_line")
		endLine, _ := payloadInt(p, "end_line")
		language, _ := payloadString(p, "language")

		entityRepoID, ok := payloadString(p, "repo_id")
		if !ok {
			entityRepoID = repoID
		}

		entityID, _ := payloadString(p, "entity_id")
		if entityID == "" {
			entityID = content.CanonicalEntityID(entityRepoID, relativePath, entityType, entityName, startLine)
		}

		fullPath := qualifyPath(repoPath, relativePath)
		metadata := extractEntityMetadata(p)

		rows = append(rows, EntityRow{
			EntityID:     entityID,
			Label:        label,
			EntityName:   entityName,
			FilePath:     fullPath,
			RelativePath: relativePath,
			StartLine:    startLine,
			EndLine:      endLine,
			Language:     language,
			RepoID:       entityRepoID,
			Metadata:     metadata,
		})
	}

	return rows
}

// extractEntityMetadata pulls the entity_metadata sub-map from a fact payload
// and preserves its native JSON-compatible values.
func extractEntityMetadata(payload map[string]any) map[string]any {
	raw, ok := payload["entity_metadata"]
	if !ok {
		return nil
	}

	typed, ok := raw.(map[string]any)
	if !ok || len(typed) == 0 {
		return nil
	}

	return cloneAnyMap(typed)
}

// extractModulesFromEntities extracts ModuleRow entries from entity facts
// whose entity_type maps to the "Module" Neo4j label. The Go parser emits
// Module entities as content_entity facts rather than separate import/module
// facts, so this function bridges the gap.
func extractModulesFromEntities(envelopes []facts.Envelope) []ModuleRow {
	entityFacts := FilterEntityFacts(envelopes)
	seen := make(map[string]struct{})
	var rows []ModuleRow

	for i := range entityFacts {
		if entityFacts[i].IsTombstone {
			continue
		}

		p := entityFacts[i].Payload
		entityType, _ := payloadString(p, "entity_type")
		label, ok := EntityTypeLabel(entityType)
		if !ok || label != "Module" {
			continue
		}

		entityName, _ := payloadString(p, "entity_name")
		if entityName == "" {
			continue
		}

		if _, ok := seen[entityName]; ok {
			continue // dedupe by name — Module MERGE key is name
		}
		seen[entityName] = struct{}{}

		language, _ := payloadString(p, "language")
		rows = append(rows, ModuleRow{
			Name:     entityName,
			Language: language,
		})
	}

	return rows
}

// extractRelationships scans all non-tombstoned facts for module, import,
// parameter, class member, and nested function payload patterns. All file
// paths are repo-qualified via mat.RepoPath to match canonical File/Entity paths.
func extractRelationships(envelopes []facts.Envelope, mat *CanonicalMaterialization) {
	// Seed the seen set with modules already extracted from entity facts.
	moduleSeen := make(map[string]struct{}, len(mat.Modules))
	for _, m := range mat.Modules {
		moduleSeen[m.Name] = struct{}{}
	}
	repoPath := mat.RepoPath

	for i := range envelopes {
		if envelopes[i].IsTombstone {
			continue
		}

		p := envelopes[i].Payload

		// Imports: facts with imported_module or module_name payload.
		moduleName, hasModule := payloadString(p, "module_name")
		importedModule, hasImported := payloadString(p, "imported_module")

		if hasModule || hasImported {
			modName := moduleName
			if modName == "" {
				modName = importedModule
			}

			// Track modules (deduped by name).
			if modName != "" {
				if _, ok := moduleSeen[modName]; !ok {
					moduleSeen[modName] = struct{}{}
					language, _ := payloadString(p, "language")
					mat.Modules = append(mat.Modules, ModuleRow{
						Name:     modName,
						Language: language,
					})
				}
			}

			// Import row — qualify relative_path with repoPath.
			relPath, _ := payloadString(p, "relative_path")
			filePath := qualifyPath(repoPath, relPath)
			if relPath == "" {
				filePath = envelopes[i].SourceRef.SourceURI
			}
			importedName, _ := payloadString(p, "imported_name")
			alias, _ := payloadString(p, "alias")
			lineNumber, _ := payloadInt(p, "line_number")

			importModule := importedModule
			if importModule == "" {
				importModule = moduleName
			}

			mat.Imports = append(mat.Imports, ImportRow{
				FilePath:     filePath,
				ModuleName:   importModule,
				ImportedName: importedName,
				Alias:        alias,
				LineNumber:   lineNumber,
			})
		}

		// Parameters: facts with param_name payload key.
		paramName, hasParam := payloadString(p, "param_name")
		if hasParam {
			funcName, _ := payloadString(p, "function_name")
			relPath, _ := payloadString(p, "relative_path")
			funcLine, _ := payloadInt(p, "function_line")

			mat.Parameters = append(mat.Parameters, ParameterRow{
				ParamName:    paramName,
				FilePath:     qualifyPath(repoPath, relPath),
				FunctionName: funcName,
				FunctionLine: funcLine,
			})
		}

		// Class members: facts with class_name AND function_name.
		className, hasClass := payloadString(p, "class_name")
		funcName, hasFunc := payloadString(p, "function_name")
		if hasClass && hasFunc && !hasParam {
			relPath, _ := payloadString(p, "relative_path")
			funcLine, _ := payloadInt(p, "function_line")

			mat.ClassMembers = append(mat.ClassMembers, ClassMemberRow{
				ClassName:    className,
				FunctionName: funcName,
				FilePath:     qualifyPath(repoPath, relPath),
				FunctionLine: funcLine,
			})
		}

		// Nested functions: facts with outer_name AND inner_name.
		outerName, hasOuter := payloadString(p, "outer_name")
		innerName, hasInner := payloadString(p, "inner_name")
		if hasOuter && hasInner {
			relPath, _ := payloadString(p, "relative_path")
			innerLine, _ := payloadInt(p, "inner_line")

			mat.NestedFuncs = append(mat.NestedFuncs, NestedFunctionRow{
				OuterName: outerName,
				InnerName: innerName,
				FilePath:  qualifyPath(repoPath, relPath),
				InnerLine: innerLine,
			})
		}
	}
}

// qualifyPath builds a globally unique canonical path by joining repoPath and
// relativePath. If either component is empty the other is returned as-is.
func qualifyPath(repoPath, relativePath string) string {
	if repoPath == "" {
		return relativePath
	}
	if relativePath == "" {
		return repoPath
	}
	return repoPath + "/" + relativePath
}
