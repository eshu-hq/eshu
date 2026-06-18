package pydataflow

import (
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

type pyFrameworkRequestEvidence struct {
	typeKinds      map[string]string
	namespaceMods  map[string]string
	frameworkTypes map[string]string
}

func newPyFrameworkRequestEvidence() pyFrameworkRequestEvidence {
	frameworkTypes := map[string]string{}
	for _, spec := range pySourceTypeSpecs {
		if len(spec.Modules) == 0 {
			continue
		}
		for _, module := range spec.Modules {
			frameworkTypes[pyImportKey(module, spec.TypeName)] = spec.Kind
		}
	}
	return pyFrameworkRequestEvidence{
		typeKinds:      map[string]string{},
		namespaceMods:  map[string]string{},
		frameworkTypes: frameworkTypes,
	}
}

func (e pyFrameworkRequestEvidence) TypeKind(typeName string) string {
	return e.typeKinds[strings.TrimSpace(typeName)]
}

func (e pyFrameworkRequestEvidence) NamespaceTypeKind(typeName string) (string, bool) {
	namespace, member, ok := strings.Cut(strings.TrimSpace(typeName), ".")
	if !ok || namespace == "" || member == "" {
		return "", false
	}
	module := e.namespaceMods[namespace]
	if module == "" {
		return "", false
	}
	kind := e.frameworkTypes[pyImportKey(module, member)]
	return kind, kind != ""
}

func pyFrameworkRequestImports(funcNode *tree_sitter.Node, source []byte) pyFrameworkRequestEvidence {
	evidence := newPyFrameworkRequestEvidence()
	root := pyTreeRoot(funcNode)
	if root == nil {
		return evidence
	}
	cursor := root.Walk()
	defer cursor.Close()
	for _, child := range root.NamedChildren(cursor) {
		child := child
		switch child.Kind() {
		case "import_statement", "import_from_statement":
			evidence.addImportStatement(&child, source)
		}
	}
	return evidence
}

func (e pyFrameworkRequestEvidence) addImportStatement(node *tree_sitter.Node, source []byte) {
	statement := strings.Join(strings.Fields(strings.TrimSpace(nodeText(node, source))), " ")
	switch {
	case strings.HasPrefix(statement, "from "):
		e.addFromImport(statement)
	case strings.HasPrefix(statement, "import "):
		e.addImport(statement)
	}
}

func (e pyFrameworkRequestEvidence) addFromImport(statement string) {
	rest := strings.TrimSpace(strings.TrimPrefix(statement, "from "))
	importIndex := strings.Index(rest, " import ")
	if importIndex == -1 {
		return
	}
	module := strings.TrimSpace(rest[:importIndex])
	if !e.hasFrameworkModule(module) {
		return
	}
	importClause := strings.TrimSpace(rest[importIndex+len(" import "):])
	importClause = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(importClause, "("), ")"))
	for _, clause := range pySplitImportClauses(importClause) {
		imported, alias := pySplitImportAlias(clause)
		if imported == "" {
			continue
		}
		if alias == "" {
			alias = imported
		}
		if kind := e.frameworkTypes[pyImportKey(module, imported)]; kind != "" {
			e.typeKinds[alias] = kind
		}
	}
}

func (e pyFrameworkRequestEvidence) addImport(statement string) {
	rest := strings.TrimSpace(strings.TrimPrefix(statement, "import "))
	for _, clause := range pySplitImportClauses(rest) {
		module, alias := pySplitImportAlias(clause)
		if module == "" || !e.hasFrameworkModule(module) {
			continue
		}
		if alias == "" {
			alias = pyImportLocalAlias(module)
		}
		if alias != "" {
			e.namespaceMods[alias] = module
		}
	}
}

func (e pyFrameworkRequestEvidence) hasFrameworkModule(module string) bool {
	for key := range e.frameworkTypes {
		if strings.HasPrefix(key, module+"\x00") {
			return true
		}
	}
	return false
}

func pySplitImportClauses(importClause string) []string {
	importClause = strings.TrimSpace(importClause)
	if importClause == "" {
		return nil
	}
	clauses := make([]string, 0)
	start := 0
	depth := 0
	for index, r := range importClause {
		switch r {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				if clause := strings.TrimSpace(importClause[start:index]); clause != "" {
					clauses = append(clauses, clause)
				}
				start = index + 1
			}
		}
	}
	if clause := strings.TrimSpace(importClause[start:]); clause != "" {
		clauses = append(clauses, clause)
	}
	return clauses
}

func pySplitImportAlias(clause string) (string, string) {
	clause = strings.TrimSpace(clause)
	if clause == "" {
		return "", ""
	}
	if left, right, ok := strings.Cut(clause, " as "); ok {
		return strings.TrimSpace(left), strings.TrimSpace(right)
	}
	return clause, ""
}

func pyImportLocalAlias(modulePath string) string {
	modulePath = strings.Trim(modulePath, ".")
	if modulePath == "" {
		return ""
	}
	if strings.Contains(modulePath, ".") {
		return strings.Split(modulePath, ".")[0]
	}
	return modulePath
}

func pyImportKey(module string, typeName string) string {
	return strings.TrimSpace(module) + "\x00" + strings.TrimSpace(typeName)
}

func pyTreeRoot(node *tree_sitter.Node) *tree_sitter.Node {
	for current := node; current != nil; current = current.Parent() {
		if current.Parent() == nil {
			return current
		}
	}
	return nil
}
