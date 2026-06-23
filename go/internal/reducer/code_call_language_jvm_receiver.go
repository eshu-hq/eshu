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
	// matchTypeFileName requires the imported file to be named after the type
	// (`pkg/Type.ext`). Java enforces one public class per file matching the
	// filename, so this disambiguates package directories. Kotlin allows a type
	// to live in any file, so Kotlin matches the package directory only and
	// trusts the prescan import map to point at the real declaring file.
	matchTypeFileName bool
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
	for _, candidateName := range jvmReceiverCandidateNames(ctx.call, "") {
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
// the parser recorded them. typeOverride replaces the call's inferred receiver
// type when non-empty, so import-bound resolution can look up the declared type
// (e.g. `Service`) rather than a local alias (e.g. `Svc`) that the prescan index
// and callee declarations are not keyed by.
func jvmReceiverCandidateNames(call map[string]any, typeOverride string) []string {
	receiverType := strings.TrimSpace(typeOverride)
	if receiverType == "" {
		receiverType = strings.TrimSpace(anyToString(call["inferred_obj_type"]))
	}
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
	paths, declaredType, attempted := jvmImportedReceiverPaths(ctx, config)
	if len(paths) == 0 {
		return "", attempted
	}
	var resolvedEntityID string
	for _, candidateName := range jvmReceiverCandidateNames(ctx.call, declaredType) {
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
	_, _, attempted := jvmImportedReceiverPaths(ctx, config)
	return attempted
}

// jvmImportedReceiverPaths resolves the imported declaration files for a
// receiver-typed call. It returns the unique candidate paths, the declared type
// the import binds the receiver to, and whether an explicit import binding was
// attempted. The declared type may differ from the receiver's inferred type when
// an alias is in play (`import a.b.Service as Svc`): the receiver reads `Svc` but
// the prescan import map and callee declarations are keyed by `Service`, so the
// resolver keys repositoryImports and the candidate names by the declared type.
func jvmImportedReceiverPaths(
	ctx codeCallResolveContext,
	config jvmReceiverResolverConfig,
) ([]string, string, bool) {
	receiverType := strings.TrimSpace(anyToString(ctx.call["inferred_obj_type"]))
	if receiverType == "" {
		return nil, "", false
	}
	qualifiedReceiverType := strings.TrimSpace(anyToString(ctx.call["inferred_obj_qualified_type"]))
	importEntries := mapSlice(ctx.fileData["imports"])
	if len(importEntries) == 0 {
		return nil, "", false
	}

	var paths []string
	resolvedDeclaredType := ""
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
		declaredType := jvmImportEntryDeclaredType(entry, receiverType, config)
		if declaredType == "" {
			continue
		}
		pathsByDeclaredType := ctx.repositoryImports[declaredType]
		if len(pathsByDeclaredType) == 0 {
			continue
		}
		for _, source := range codeCallImportEntrySources(entry) {
			if !jvmImportSourceMatchesQualifiedReceiver(source, declaredType, qualifiedReceiverType) {
				continue
			}
			attempted = true
			resolvedDeclaredType = declaredType
			for _, path := range pathsByDeclaredType {
				if jvmImportSourceMatchesPath(source, declaredType, path, config) {
					appendPath(path)
				}
			}
		}
	}
	if len(paths) != 1 {
		return nil, "", attempted
	}
	return paths, resolvedDeclaredType, attempted
}

// jvmImportEntryDeclaredType returns the declared type name an import entry binds
// the receiver to, or "" when the entry does not introduce the receiver. The
// declared type is the type's simple name as declared at its source (the trailing
// segment of the import `name`/`source`), which differs from the local receiver
// name under aliasing. It is the key the prescan import map and callee
// class_context declarations use.
func jvmImportEntryDeclaredType(
	entry map[string]any,
	receiverType string,
	config jvmReceiverResolverConfig,
) string {
	receiverType = strings.TrimSpace(receiverType)
	if receiverType == "" {
		return ""
	}
	if _, ok := config.importTypes[strings.TrimSpace(anyToString(entry["import_type"]))]; !ok {
		return ""
	}
	declaredFromName := codeCallTrailingName(anyToString(entry["name"]))
	declaredFromSource := codeCallTrailingName(anyToString(entry["source"]))
	// The local name the receiver uses is the alias when present, otherwise the
	// trailing name of the imported path.
	if alias := strings.TrimSpace(anyToString(entry["alias"])); alias == receiverType {
		if declaredFromName != "" {
			return declaredFromName
		}
		if declaredFromSource != "" {
			return declaredFromSource
		}
		return receiverType
	}
	if declaredFromName == receiverType || declaredFromSource == receiverType {
		return receiverType
	}
	// A wildcard `import pkg.*` brings the receiver type in under its own name.
	if strings.HasSuffix(strings.TrimSpace(anyToString(entry["source"])), ".*") {
		return receiverType
	}
	return ""
}

func jvmImportSourceMatchesPath(
	source string,
	declaredType string,
	path string,
	config jvmReceiverResolverConfig,
) bool {
	source = strings.TrimSpace(source)
	declaredType = strings.TrimSpace(declaredType)
	path = normalizeCodeCallPath(path)
	if source == "" || declaredType == "" || path == "" {
		return false
	}
	if strings.HasSuffix(source, ".*") {
		source = strings.TrimSuffix(source, ".*") + "." + declaredType
	}
	if config.matchTypeFileName {
		sourcePath := strings.ReplaceAll(source, ".", "/") + config.sourceExtension
		return strings.HasSuffix(path, sourcePath)
	}
	// The type may be declared in any file (Kotlin), so match the package
	// directory and let the prescan import map decide the real file. Strip the
	// declared-type segment from the dotted source to get the package path.
	packageSource := source
	if idx := strings.LastIndex(packageSource, "."); idx >= 0 {
		packageSource = packageSource[:idx]
	} else {
		packageSource = ""
	}
	packageDir := strings.ReplaceAll(packageSource, ".", "/")
	if packageDir == "" {
		return true
	}
	return strings.HasSuffix(codeCallDirectoryKey(path), packageDir)
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
