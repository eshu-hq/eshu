// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package jvm

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/reducer/code/call/shared"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
)

// ReceiverConfig parameterizes the shared imported-receiver resolver used by
// JVM-family languages (Java, Kotlin) whose parsers emit a receiver type
// (inferred_obj_type), package-qualified imports, and a file layout that
// mirrors the dotted import path on disk. Languages differ only in the import
// kinds they emit and the source-file extension their package paths map to.
type ReceiverConfig struct {
	// ImportTypes is the set of import_type values that introduce a usable
	// type binding. Java emits only "import"; Kotlin additionally emits
	// "alias" for `import a.b.C as D`.
	ImportTypes map[string]struct{}
	// SourceExtension is the file extension a dotted package source maps to,
	// e.g. ".java" or ".kt".
	SourceExtension string
	// MatchTypeFileName requires the imported file to be named after the type
	// (`pkg/Type.ext`). Java enforces one public class per file matching the
	// filename, so this disambiguates package directories. Kotlin allows a type
	// to live in any file, so Kotlin matches the package directory only and
	// trusts the prescan import map to point at the real declaring file.
	MatchTypeFileName bool
}

// ResolveReceiverCallee binds a receiver-typed call to the declaration of an
// imported type when that import resolves to exactly one repository path, then
// falls back to repository-scoped type-inference candidate names. It mirrors the
// closed resolution-provenance contract (ADR #2222): import-bound matches record
// import_binding and inference matches record type_inferred. Ambiguous bindings
// resolve to nothing rather than inventing an edge.
func ResolveReceiverCallee(
	ctx shared.ResolveContext,
	config ReceiverConfig,
) (string, string, codeprovenance.Method) {
	if entityID, attempted := resolveImportedReceiverCallee(ctx, config); entityID != "" {
		return entityID, ctx.Index.EntityFileByID(entityID), codeprovenance.MethodImportBinding
	} else if attempted {
		return "", "", ""
	}
	for _, candidateName := range receiverCandidateNames(ctx.Call, "") {
		entityID := resolveReceiverCandidate(ctx, candidateName)
		if entityID == "" {
			continue
		}
		return entityID, ctx.Index.EntityFileByID(entityID), codeprovenance.MethodTypeInferred
	}
	return "", "", ""
}

// receiverCandidateNames returns the ordered "Type.method" candidate names
// for a receiver-typed call, widened by argument-type and arity signatures when
// the parser recorded them. typeOverride replaces the call's inferred receiver
// type when non-empty, so import-bound resolution can look up the declared type
// (e.g. `Service`) rather than a local alias (e.g. `Svc`) that the prescan index
// and callee declarations are not keyed by.
func receiverCandidateNames(call map[string]any, typeOverride string) []string {
	receiverType := strings.TrimSpace(typeOverride)
	if receiverType == "" {
		receiverType = strings.TrimSpace(payloadcore.AnyToString(call["inferred_obj_type"]))
	}
	callName := strings.TrimSpace(payloadcore.AnyToString(call["name"]))
	if receiverType == "" || callName == "" {
		return nil
	}
	candidates := []string{receiverType + "." + callName}
	if argumentTypes := shared.MetadataStringSlice(call, "argument_types"); len(argumentTypes) > 0 {
		candidates = shared.AppendTypedSignatureNames(candidates, argumentTypes)
	}
	if arity, ok := shared.MetadataInt(call, "argument_count"); ok {
		candidates = shared.AppendArityNames(candidates, arity)
	}
	return candidates
}

func resolveReceiverCandidate(ctx shared.ResolveContext, candidateName string) string {
	candidateName = strings.TrimSpace(candidateName)
	if candidateName == "" || ctx.RepositoryID == "" {
		return ""
	}
	callerDir := shared.DirectoryKey(shared.PreferredPath(ctx.RawPath, ctx.RelativePath))
	if callerDir != "" {
		if entityID := ctx.Index.UniqueNameByRepoDir(ctx.RepositoryID, callerDir, candidateName); entityID != "" {
			return entityID
		}
	}
	return ctx.Index.UniqueNameByRepo(ctx.RepositoryID, candidateName)
}

// resolveImportedReceiverCallee resolves a receiver-typed call to the unique
// declaration reachable through the file's imports. The boolean reports whether
// an import binding was attempted, which the dispatch uses to block the broad
// repo-unique-name fallback when an explicit import exists.
func resolveImportedReceiverCallee(
	ctx shared.ResolveContext,
	config ReceiverConfig,
) (string, bool) {
	if ctx.RepositoryID == "" {
		return "", false
	}
	paths, declaredType, attempted := importedReceiverPaths(ctx, config)
	if len(paths) == 0 {
		return "", attempted
	}
	var resolvedEntityID string
	for _, candidateName := range receiverCandidateNames(ctx.Call, declaredType) {
		for _, path := range paths {
			entityID := ctx.Index.UniqueNameByPath(path, candidateName)
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

// ImportedReceiverBlocksRepoFallback reports whether the file imported the
// receiver type, so the dispatch must not fall back to an ambiguous
// repo-unique-name guess after the resolver declines.
func ImportedReceiverBlocksRepoFallback(
	ctx shared.ResolveContext,
	config ReceiverConfig,
) bool {
	_, _, attempted := importedReceiverPaths(ctx, config)
	return attempted
}

// importedReceiverPaths resolves the imported declaration files for a
// receiver-typed call. It returns the unique candidate paths, the declared type
// the import binds the receiver to, and whether an explicit import binding was
// attempted. The declared type may differ from the receiver's inferred type when
// an alias is in play (`import a.b.Service as Svc`): the receiver reads `Svc` but
// the prescan import map and callee declarations are keyed by `Service`, so the
// resolver keys RepositoryImports and the candidate names by the declared type.
func importedReceiverPaths(
	ctx shared.ResolveContext,
	config ReceiverConfig,
) ([]string, string, bool) {
	receiverType := strings.TrimSpace(payloadcore.AnyToString(ctx.Call["inferred_obj_type"]))
	if receiverType == "" {
		return nil, "", false
	}
	qualifiedReceiverType := strings.TrimSpace(payloadcore.AnyToString(ctx.Call["inferred_obj_qualified_type"]))
	importEntries := payloadcore.MapSlice(ctx.FileData["imports"])
	if len(importEntries) == 0 {
		return nil, "", false
	}

	var paths []string
	resolvedDeclaredType := ""
	attempted := false
	appendPath := func(path string) {
		path = shared.NormalizePath(path)
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
		declaredType := importEntryDeclaredType(entry, receiverType, config)
		if declaredType == "" {
			continue
		}
		pathsByDeclaredType := ctx.RepositoryImports[declaredType]
		if len(pathsByDeclaredType) == 0 {
			continue
		}
		for _, source := range shared.ImportEntrySources(entry) {
			if !importSourceMatchesQualifiedReceiver(source, declaredType, qualifiedReceiverType) {
				continue
			}
			attempted = true
			resolvedDeclaredType = declaredType
			for _, path := range pathsByDeclaredType {
				if importSourceMatchesPath(source, declaredType, path, config) {
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

// importEntryDeclaredType returns the declared type name an import entry binds
// the receiver to, or "" when the entry does not introduce the receiver. The
// declared type is the type's simple name as declared at its source (the trailing
// segment of the import `name`/`source`), which differs from the local receiver
// name under aliasing. It is the key the prescan import map and callee
// class_context declarations use.
func importEntryDeclaredType(
	entry map[string]any,
	receiverType string,
	config ReceiverConfig,
) string {
	receiverType = strings.TrimSpace(receiverType)
	if receiverType == "" {
		return ""
	}
	if _, ok := config.ImportTypes[strings.TrimSpace(payloadcore.AnyToString(entry["import_type"]))]; !ok {
		return ""
	}
	declaredFromName := shared.TrailingName(payloadcore.AnyToString(entry["name"]))
	declaredFromSource := shared.TrailingName(payloadcore.AnyToString(entry["source"]))
	// The local name the receiver uses is the alias when present, otherwise the
	// trailing name of the imported path.
	if alias := strings.TrimSpace(payloadcore.AnyToString(entry["alias"])); alias == receiverType {
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
	if strings.HasSuffix(strings.TrimSpace(payloadcore.AnyToString(entry["source"])), ".*") {
		return receiverType
	}
	return ""
}

func importSourceMatchesPath(
	source string,
	declaredType string,
	path string,
	config ReceiverConfig,
) bool {
	source = strings.TrimSpace(source)
	declaredType = strings.TrimSpace(declaredType)
	path = shared.NormalizePath(path)
	if source == "" || declaredType == "" || path == "" {
		return false
	}
	if strings.HasSuffix(source, ".*") {
		source = strings.TrimSuffix(source, ".*") + "." + declaredType
	}
	if config.MatchTypeFileName {
		sourcePath := strings.ReplaceAll(source, ".", "/") + config.SourceExtension
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
	return strings.HasSuffix(shared.DirectoryKey(path), packageDir)
}

func importSourceMatchesQualifiedReceiver(source string, receiverType string, qualifiedReceiverType string) bool {
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
