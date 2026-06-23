package reducer

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
)

// jvmReceiverResolverConfig parameterizes the shared imported-receiver resolver
// used by JVM-family languages (Java, Kotlin) whose parsers emit a receiver
// type (inferred_obj_type), package-qualified imports, and a file layout that
// mirrors the dotted import path on disk. Languages differ only in the import
// kinds they emit and the source-file extension their package paths map to.
type jvmReceiverResolverConfig struct {
	// importTypes is the set of import_type values that introduce a usable
	// type binding. Java emits only "import"; Kotlin additionally emits
	// "alias" for `import a.b.C as D`.
	importTypes map[string]struct{}
	// sourceExtension is the file extension a dotted package source maps to,
	// e.g. ".java" or ".kt".
	sourceExtension string
}

// resolveJVMReceiverCallee binds a receiver-typed call to the declaration of an
// imported type when that import resolves to exactly one repository path, then
// falls back to repository-scoped type-inference candidate names. It mirrors the
// closed resolution-provenance contract (ADR #2222): import-bound matches record
// import_binding and inference matches record type_inferred. Ambiguous bindings
// resolve to nothing rather than inventing an edge.
func resolveJVMReceiverCallee(
	ctx codeCallResolveContext,
	config jvmReceiverResolverConfig,
) (string, string, codeprovenance.Method) {
	if entityID, attempted := resolveJVMImportedReceiverCallee(ctx, config); entityID != "" {
		return entityID, ctx.index.entityFileByID[entityID], codeprovenance.MethodImportBinding
	} else if attempted {
		return "", "", ""
	}
	for _, candidateName := range jvmReceiverCandidateNames(ctx.call) {
		entityID := resolveJVMReceiverCandidate(ctx, candidateName)
		if entityID == "" {
			continue
		}
		return entityID, ctx.index.entityFileByID[entityID], codeprovenance.MethodTypeInferred
	}
	return "", "", ""
}

// jvmReceiverCandidateNames returns the ordered "Type.method" candidate names
// for a receiver-typed call, widened by argument-type and arity signatures when
// the parser recorded them.
func jvmReceiverCandidateNames(call map[string]any) []string {
	receiverType := strings.TrimSpace(anyToString(call["inferred_obj_type"]))
	callName := strings.TrimSpace(anyToString(call["name"]))
	if receiverType == "" || callName == "" {
		return nil
	}
	candidates := []string{receiverType + "." + callName}
	if argumentTypes := codeCallMetadataStringSlice(call, "argument_types"); len(argumentTypes) > 0 {
		candidates = codeCallAppendTypedSignatureNames(candidates, argumentTypes)
	}
	if arity, ok := codeCallMetadataInt(call, "argument_count"); ok {
		candidates = codeCallAppendArityNames(candidates, arity)
	}
	return candidates
}

func resolveJVMReceiverCandidate(ctx codeCallResolveContext, candidateName string) string {
	candidateName = strings.TrimSpace(candidateName)
	if candidateName == "" || ctx.repositoryID == "" {
		return ""
	}
	callerDir := codeCallDirectoryKey(codeCallPreferredPath(ctx.rawPath, ctx.relativePath))
	if callerDir != "" {
		if entityID := ctx.index.uniqueNameByRepoDir[ctx.repositoryID][callerDir][candidateName]; entityID != "" {
			return entityID
		}
	}
	return ctx.index.uniqueNameByRepo[ctx.repositoryID][candidateName]
}

// resolveJVMImportedReceiverCallee resolves a receiver-typed call to the unique
// declaration reachable through the file's imports. The boolean reports whether
// an import binding was attempted, which the dispatch uses to block the broad
// repo-unique-name fallback when an explicit import exists.
func resolveJVMImportedReceiverCallee(
	ctx codeCallResolveContext,
	config jvmReceiverResolverConfig,
) (string, bool) {
	if ctx.repositoryID == "" {
		return "", false
	}
	paths, attempted := jvmImportedReceiverPaths(ctx, config)
	if len(paths) == 0 {
		return "", attempted
	}
	var resolvedEntityID string
	for _, candidateName := range jvmReceiverCandidateNames(ctx.call) {
		for _, path := range paths {
			entityID := ctx.index.uniqueNameByPath[path][candidateName]
			if entityID == "" || entityID == resolvedEntityID {
				continue
			}
			if resolvedEntityID != "" {
				return "", true
			}
			resolvedEntityID = entityID
		}
	}
	return resolvedEntityID, attempted
}

// jvmImportedReceiverBindingBlocksRepoFallback reports whether the file imported
// the receiver type, so the dispatch must not fall back to an ambiguous
// repo-unique-name guess after the resolver declines.
func jvmImportedReceiverBindingBlocksRepoFallback(
	ctx codeCallResolveContext,
	config jvmReceiverResolverConfig,
) bool {
	_, attempted := jvmImportedReceiverPaths(ctx, config)
	return attempted
}

func jvmImportedReceiverPaths(
	ctx codeCallResolveContext,
	config jvmReceiverResolverConfig,
) ([]string, bool) {
	receiverType := strings.TrimSpace(anyToString(ctx.call["inferred_obj_type"]))
	if receiverType == "" {
		return nil, false
	}
	qualifiedReceiverType := strings.TrimSpace(anyToString(ctx.call["inferred_obj_qualified_type"]))
	importEntries := mapSlice(ctx.fileData["imports"])
	pathsByReceiver := ctx.repositoryImports[receiverType]
	if len(importEntries) == 0 || len(pathsByReceiver) == 0 {
		return nil, false
	}

	var paths []string
	attempted := false
	appendPath := func(path string) {
		path = normalizeCodeCallPath(path)
		if path == "" {
			return
		}
		for _, existing := range paths {
			if existing == path {
				return
			}
		}
		paths = append(paths, path)
	}

	for _, entry := range importEntries {
		if !jvmImportEntryMatchesReceiver(entry, receiverType, config) {
			continue
		}
		for _, source := range codeCallImportEntrySources(entry) {
			if !jvmImportSourceMatchesQualifiedReceiver(source, receiverType, qualifiedReceiverType) {
				continue
			}
			attempted = true
			for _, path := range pathsByReceiver {
				if jvmImportSourceMatchesPath(source, receiverType, path, config) {
					appendPath(path)
				}
			}
		}
	}
	if len(paths) != 1 {
		return nil, attempted
	}
	return paths, attempted
}

func jvmImportEntryMatchesReceiver(
	entry map[string]any,
	receiverType string,
	config jvmReceiverResolverConfig,
) bool {
	receiverType = strings.TrimSpace(receiverType)
	if receiverType == "" {
		return false
	}
	if _, ok := config.importTypes[strings.TrimSpace(anyToString(entry["import_type"]))]; !ok {
		return false
	}
	for _, value := range []string{
		anyToString(entry["alias"]),
		codeCallTrailingName(anyToString(entry["name"])),
		codeCallTrailingName(anyToString(entry["source"])),
	} {
		if strings.TrimSpace(value) == receiverType {
			return true
		}
	}
	source := strings.TrimSpace(anyToString(entry["source"]))
	return strings.HasSuffix(source, ".*")
}

func jvmImportSourceMatchesPath(
	source string,
	receiverType string,
	path string,
	config jvmReceiverResolverConfig,
) bool {
	source = strings.TrimSpace(source)
	receiverType = strings.TrimSpace(receiverType)
	path = normalizeCodeCallPath(path)
	if source == "" || receiverType == "" || path == "" {
		return false
	}
	if strings.HasSuffix(source, ".*") {
		source = strings.TrimSuffix(source, ".*") + "." + receiverType
	}
	sourcePath := strings.ReplaceAll(source, ".", "/") + config.sourceExtension
	return strings.HasSuffix(path, sourcePath)
}

func jvmImportSourceMatchesQualifiedReceiver(source string, receiverType string, qualifiedReceiverType string) bool {
	qualifiedReceiverType = strings.TrimSpace(qualifiedReceiverType)
	if qualifiedReceiverType == "" || !strings.Contains(qualifiedReceiverType, ".") {
		return true
	}
	source = strings.TrimSpace(source)
	receiverType = strings.TrimSpace(receiverType)
	if source == "" || receiverType == "" {
		return false
	}
	if strings.HasSuffix(source, ".*") {
		source = strings.TrimSuffix(source, ".*") + "." + receiverType
	}
	return source == qualifiedReceiverType
}
