package javascript

import (
	"os"
	"regexp"
	"strings"
)

type javaScriptTypeScriptImportedBinding struct {
	importedName string
	source       string
}

var (
	javaScriptTypeScriptNamedImportRe       = regexp.MustCompile(`(?s)\bimport\s+(?:type\s+)?(?:[A-Za-z_$][A-Za-z0-9_$]*\s*,\s*)?\{([^}]*)\}\s+from\s+["']([^"']+)["']`)
	javaScriptTypeScriptLocalExportClauseRe = regexp.MustCompile(`(?s)\bexport\s+(?:type\s+)?\{([^}]*)\}`)
	javaScriptTypeScriptPublicDeclNameRe    = regexp.MustCompile(`\bexport\s+(?:abstract\s+class|interface|type|class|enum)\s+([A-Za-z_$][A-Za-z0-9_$]*)\b`)
)

func javaScriptTypeScriptImportedExportClauseReexportsFromSource(source string) []javaScriptTypeScriptSurfaceReexport {
	importsByLocalName := javaScriptTypeScriptNamedImportsByLocalName(source)
	if len(importsByLocalName) == 0 {
		return nil
	}

	reexports := make([]javaScriptTypeScriptSurfaceReexport, 0)
	matches := javaScriptTypeScriptLocalExportClauseRe.FindAllStringSubmatchIndex(source, -1)
	for _, match := range matches {
		if len(match) < 4 || javaScriptTypeScriptExportClauseHasFromSource(source, match[1]) {
			continue
		}
		for _, part := range strings.Split(source[match[2]:match[3]], ",") {
			localName, exportedName := javaScriptReExportSpecifierNames(part)
			if localName == "" || exportedName == "" {
				continue
			}
			binding, ok := importsByLocalName[localName]
			if !ok || binding.importedName == "" || binding.source == "" {
				continue
			}
			reexports = append(reexports, javaScriptTypeScriptSurfaceReexport{
				exportedName: exportedName,
				originalName: binding.importedName,
				source:       binding.source,
			})
		}
	}
	return reexports
}

func javaScriptTypeScriptNamedImportsByLocalName(source string) map[string]javaScriptTypeScriptImportedBinding {
	bindings := make(map[string]javaScriptTypeScriptImportedBinding)
	for _, match := range javaScriptTypeScriptNamedImportRe.FindAllStringSubmatch(source, -1) {
		if len(match) != 3 {
			continue
		}
		moduleSource := strings.TrimSpace(match[2])
		if moduleSource == "" {
			continue
		}
		for _, part := range strings.Split(match[1], ",") {
			importedName, localName := javaScriptReExportSpecifierNames(part)
			if importedName == "" || localName == "" {
				continue
			}
			if _, exists := bindings[localName]; exists {
				continue
			}
			bindings[localName] = javaScriptTypeScriptImportedBinding{
				importedName: importedName,
				source:       moduleSource,
			}
		}
	}
	return bindings
}

func javaScriptTypeScriptExportClauseHasFromSource(source string, matchEnd int) bool {
	remaining := strings.TrimSpace(source[matchEnd:])
	return strings.HasPrefix(remaining, "from ")
}

func javaScriptTypeScriptPublicImportedTypeReferenceNames(
	repoRoot string,
	publicPath string,
	targetPath string,
	exportedNames map[string]struct{},
) map[string]struct{} {
	const maxReferenceDepth = 8
	references := make(map[string]struct{})
	queue := []javaScriptTypeScriptSurfaceWalkItem{{path: publicPath, star: true}}
	visited := make(map[string]struct{})
	for len(queue) > 0 {
		item := queue[0]
		queue = queue[1:]
		if item.depth >= maxReferenceDepth {
			continue
		}
		visitKey := javaScriptTypeScriptSurfaceWalkKey(item)
		if _, ok := visited[visitKey]; ok {
			continue
		}
		visited[visitKey] = struct{}{}

		body, err := os.ReadFile(item.path)
		if err != nil {
			continue
		}
		source := string(body)
		targetBindings := javaScriptTypeScriptImportedBindingsForTarget(repoRoot, item.path, targetPath, source, exportedNames)
		for name := range javaScriptTypeScriptImportedTypeReferencesFromPublicDeclarations(source, item, targetBindings) {
			if binding, ok := targetBindings[name]; ok {
				references[binding.importedName] = struct{}{}
			}
		}
		for _, reexport := range javaScriptTypeScriptStaticReexportsFromSource(source) {
			nextNames, nextStar, ok := javaScriptTypeScriptPropagateReexport(item, reexport)
			if !ok {
				continue
			}
			for _, candidatePath := range javaScriptTypeScriptReexportSourceCandidates(repoRoot, item.path, reexport.source) {
				queue = append(queue, javaScriptTypeScriptSurfaceWalkItem{
					path:  candidatePath,
					names: nextNames,
					star:  nextStar,
					depth: item.depth + 1,
				})
			}
		}
	}
	return references
}

func javaScriptTypeScriptImportedBindingsForTarget(
	repoRoot string,
	fromPath string,
	targetPath string,
	source string,
	exportedNames map[string]struct{},
) map[string]javaScriptTypeScriptImportedBinding {
	bindings := javaScriptTypeScriptNamedImportsByLocalName(source)
	if len(bindings) == 0 {
		return nil
	}
	targetBindings := make(map[string]javaScriptTypeScriptImportedBinding)
	for localName, binding := range bindings {
		if _, ok := exportedNames[binding.importedName]; !ok {
			continue
		}
		for _, candidatePath := range javaScriptTypeScriptReexportSourceCandidates(repoRoot, fromPath, binding.source) {
			if sameJavaScriptPath(candidatePath, targetPath) {
				targetBindings[localName] = binding
				break
			}
		}
	}
	return targetBindings
}

func javaScriptTypeScriptImportedTypeReferencesFromPublicDeclarations(
	source string,
	item javaScriptTypeScriptSurfaceWalkItem,
	importsByLocalName map[string]javaScriptTypeScriptImportedBinding,
) map[string]struct{} {
	if len(importsByLocalName) == 0 {
		return nil
	}
	publicNames := javaScriptTypeScriptPublicDeclarationNames(source, item)
	if len(publicNames) == 0 {
		return nil
	}
	references := make(map[string]struct{})
	for name := range publicNames {
		declaration := javaScriptTypeScriptPublicDeclarationText(source, name)
		if declaration == "" {
			continue
		}
		for localName := range importsByLocalName {
			if javaScriptIdentifierMentioned(declaration, localName) {
				references[localName] = struct{}{}
			}
		}
	}
	return references
}

func javaScriptTypeScriptPublicDeclarationNames(
	source string,
	item javaScriptTypeScriptSurfaceWalkItem,
) map[string]struct{} {
	if !item.star {
		return cloneJavaScriptTypeScriptSurfaceNames(item.names)
	}
	names := make(map[string]struct{})
	for _, match := range javaScriptTypeScriptPublicDeclNameRe.FindAllStringSubmatch(source, -1) {
		if len(match) == 2 {
			names[strings.TrimSpace(match[1])] = struct{}{}
		}
	}
	return names
}

func javaScriptTypeScriptPublicDeclarationText(source string, name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	for _, prefix := range []string{"export interface ", "export type ", "export class ", "export abstract class ", "export enum "} {
		start := strings.Index(source, prefix+name)
		if start < 0 {
			continue
		}
		end := strings.Index(source[start+1:], "\nexport ")
		if end < 0 {
			return source[start:]
		}
		return source[start : start+1+end]
	}
	return ""
}

func javaScriptIdentifierMentioned(source string, name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	remaining := source
	for {
		offset := strings.Index(remaining, name)
		if offset < 0 {
			return false
		}
		if javaScriptIdentifierBoundaryBefore(remaining, offset) &&
			javaScriptIdentifierBoundaryAfter(remaining, offset+len(name)) {
			return true
		}
		nextStart := offset + len(name)
		if nextStart >= len(remaining) {
			return false
		}
		remaining = remaining[nextStart:]
	}
}

func javaScriptIdentifierBoundaryBefore(source string, offset int) bool {
	if offset <= 0 {
		return true
	}
	character := source[offset-1]
	return !javaScriptIdentifierCharacter(character)
}

func javaScriptIdentifierBoundaryAfter(source string, offset int) bool {
	if offset >= len(source) {
		return true
	}
	return !javaScriptIdentifierCharacter(source[offset])
}

func javaScriptIdentifierCharacter(character byte) bool {
	return (character >= 'A' && character <= 'Z') ||
		(character >= 'a' && character <= 'z') ||
		(character >= '0' && character <= '9') ||
		character == '_' ||
		character == '$'
}
