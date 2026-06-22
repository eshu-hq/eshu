package javascript

import (
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

type javaScriptTypeScriptImportedBinding struct {
	importedName string
	source       string
}

// javaScriptTypeScriptImportedExportClauseReexportsFromRoot finds re-exports
// expressed as a local export clause over previously imported names, the
// declare-namespace `export type { ... }` shape. It walks export_statement nodes
// that have no module source and maps each exported specifier back to its
// imported binding so the re-export resolves to the original module.
func javaScriptTypeScriptImportedExportClauseReexportsFromRoot(root *tree_sitter.Node, source []byte) []javaScriptTypeScriptSurfaceReexport {
	importsByLocalName := javaScriptTypeScriptNamedImportsByLocalName(root, source)
	if len(importsByLocalName) == 0 {
		return nil
	}

	reexports := make([]javaScriptTypeScriptSurfaceReexport, 0)
	walkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "export_statement" {
			return
		}
		if node.ChildByFieldName("source") != nil {
			return
		}
		for _, specifier := range javaScriptReExportSpecifiers(node, source) {
			binding, ok := importsByLocalName[specifier.originalName]
			if !ok || binding.importedName == "" || binding.source == "" {
				continue
			}
			reexports = append(reexports, javaScriptTypeScriptSurfaceReexport{
				exportedName: specifier.exportedName,
				originalName: binding.importedName,
				source:       binding.source,
			})
		}
	})
	return reexports
}

// javaScriptTypeScriptNamedImportsByLocalName maps each named-import local name
// to its imported name and module source. It walks import_statement nodes and
// reuses the shared import-entry extraction, keeping only named bindings (not
// default or namespace imports) to match the prior import-clause contract.
func javaScriptTypeScriptNamedImportsByLocalName(root *tree_sitter.Node, source []byte) map[string]javaScriptTypeScriptImportedBinding {
	bindings := make(map[string]javaScriptTypeScriptImportedBinding)
	if root == nil {
		return bindings
	}
	walkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "import_statement" {
			return
		}
		for _, item := range javaScriptImportEntries(node, source, "") {
			importedName, _ := item["name"].(string)
			importedName = strings.TrimSpace(importedName)
			if importedName == "" || importedName == "default" || importedName == "*" {
				continue
			}
			moduleSource, _ := item["source"].(string)
			moduleSource = strings.TrimSpace(moduleSource)
			if moduleSource == "" {
				continue
			}
			localName := strings.TrimSpace(stringOr(item["alias"]))
			if localName == "" {
				localName = importedName
			}
			if _, exists := bindings[localName]; exists {
				continue
			}
			bindings[localName] = javaScriptTypeScriptImportedBinding{
				importedName: importedName,
				source:       moduleSource,
			}
		}
	})
	return bindings
}

// stringOr returns the string held by value, or "" when it is absent or not a
// string.
func stringOr(value any) string {
	text, _ := value.(string)
	return text
}

func javaScriptTypeScriptPublicImportedTypeReferenceNames(
	repoRoot string,
	publicPath string,
	targetPath string,
	exportedNames map[string]struct{},
	siblingParser *javaScriptSiblingParser,
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

		root, source, ok := siblingParser.rootForFile(item.path)
		if !ok {
			continue
		}
		targetBindings := javaScriptTypeScriptImportedBindingsForTarget(repoRoot, item.path, targetPath, root, source, exportedNames)
		for name := range javaScriptTypeScriptImportedTypeReferencesFromPublicDeclarations(root, source, item, targetBindings) {
			if binding, ok := targetBindings[name]; ok {
				references[binding.importedName] = struct{}{}
			}
		}
		for _, reexport := range javaScriptTypeScriptStaticReexportsFromRoot(root, source) {
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
	root *tree_sitter.Node,
	source []byte,
	exportedNames map[string]struct{},
) map[string]javaScriptTypeScriptImportedBinding {
	bindings := javaScriptTypeScriptNamedImportsByLocalName(root, source)
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
	root *tree_sitter.Node,
	source []byte,
	item javaScriptTypeScriptSurfaceWalkItem,
	importsByLocalName map[string]javaScriptTypeScriptImportedBinding,
) map[string]struct{} {
	if len(importsByLocalName) == 0 {
		return nil
	}
	declarations := javaScriptTypeScriptPublicDeclarationNodes(root, source, item)
	if len(declarations) == 0 {
		return nil
	}
	references := make(map[string]struct{})
	for _, declaration := range declarations {
		mentioned := javaScriptTypeScriptDeclarationMentionedNames(declaration, source, importsByLocalName)
		for localName := range mentioned {
			references[localName] = struct{}{}
		}
	}
	return references
}

// javaScriptTypeScriptDeclarationMentionedNames returns the imported local names
// referenced as identifiers anywhere inside a public declaration node. It walks
// identifier and type-identifier nodes so a type reference is matched on the AST
// instead of a text identifier-boundary scan.
func javaScriptTypeScriptDeclarationMentionedNames(
	declaration *tree_sitter.Node,
	source []byte,
	importsByLocalName map[string]javaScriptTypeScriptImportedBinding,
) map[string]struct{} {
	mentioned := make(map[string]struct{})
	if declaration == nil {
		return mentioned
	}
	walkNamed(declaration, func(node *tree_sitter.Node) {
		switch node.Kind() {
		case "identifier", "type_identifier", "property_identifier",
			"shorthand_property_identifier", "nested_type_identifier",
			"scoped_type_identifier":
		default:
			return
		}
		name := strings.TrimSpace(nodeText(node, source))
		if _, ok := importsByLocalName[name]; ok {
			mentioned[name] = struct{}{}
		}
	})
	return mentioned
}

// javaScriptTypeScriptPublicDeclarationNodes returns the exported declaration
// nodes whose names are public for this walk item. A star item exposes every
// exported interface/type/class/enum declaration; a named item exposes only the
// declarations matching the carried names.
func javaScriptTypeScriptPublicDeclarationNodes(
	root *tree_sitter.Node,
	source []byte,
	item javaScriptTypeScriptSurfaceWalkItem,
) []*tree_sitter.Node {
	declarations := make([]*tree_sitter.Node, 0)
	if root == nil {
		return declarations
	}
	parents := buildJavaScriptParentLookup(root)
	walkNamed(root, func(node *tree_sitter.Node) {
		if !javaScriptIsExported(node, parents) {
			return
		}
		if !javaScriptTypeScriptIsPublicDeclarationKind(node) {
			return
		}
		name := javaScriptTypeScriptDeclarationName(node, source)
		if name == "" {
			return
		}
		if !item.star {
			if _, ok := item.names[name]; !ok {
				return
			}
		}
		declarations = append(declarations, node)
	})
	return declarations
}

// javaScriptTypeScriptIsPublicDeclarationKind reports whether node is an
// interface, type alias, class, or enum declaration, the declaration kinds the
// prior public-declaration scan recognized (functions and variables excluded).
func javaScriptTypeScriptIsPublicDeclarationKind(node *tree_sitter.Node) bool {
	if node == nil {
		return false
	}
	switch node.Kind() {
	case "interface_declaration", "type_alias_declaration",
		"class_declaration", "abstract_class_declaration", "enum_declaration":
		return true
	default:
		return false
	}
}
