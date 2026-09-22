// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package symbols

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// InterfaceTarget describes what one type-annotated position (a struct field,
// a function parameter, a return type) resolves to when it names an
// interface: either a package-local interface, or an imported interface type
// with a known or unknown method set. Callers outside this package construct
// and mutate InterfaceTarget values directly (see
// FunctionParamInterfaceTargets' callers merging imported-method evidence
// gathered from other files in the same package), so its fields are exported.
type InterfaceTarget struct {
	// LocalInterface is the lower-case name of the package-local interface
	// this target resolves to, or "" when the target is not a local
	// interface.
	LocalInterface string
	// Imported reports whether this target resolves to an interface type
	// from another package.
	Imported bool
	// ImportedMethods is the known lower-case method set for an imported
	// interface target. It is empty when Imported is true but the method
	// set is unknown (AllowExportedMethods then governs matching).
	ImportedMethods []string
	// AllowExportedMethods reports whether any exported method on a
	// candidate concrete type should be treated as satisfying this
	// imported target, because ImportedMethods could not be determined.
	AllowExportedMethods bool
}

// Modeled reports whether target names either a package-local interface or an
// imported interface this package has evidence for.
func (target InterfaceTarget) Modeled() bool {
	return target.LocalInterface != "" || target.Imported
}

// InterfaceMethodNames returns the lower-cased method names declared directly
// on one interface_type node.
func InterfaceMethodNames(node *tree_sitter.Node, source []byte) []string {
	names := make([]string, 0)
	shared.WalkNamed(node, func(child *tree_sitter.Node) {
		if child.Kind() != "method_elem" {
			return
		}
		name := strings.ToLower(strings.TrimSpace(shared.NodeText(child.ChildByFieldName("name"), source)))
		if name != "" {
			names = AppendUniqueImportAlias(names, name)
		}
	})
	return names
}

// AppendUniqueMethods appends the trimmed, lower-cased, non-empty members of
// methods to target, skipping any already present.
func AppendUniqueMethods(target []string, methods []string) []string {
	for _, method := range methods {
		if trimmed := strings.TrimSpace(method); trimmed != "" {
			target = AppendUniqueImportAlias(target, strings.ToLower(trimmed))
		}
	}
	return target
}

// KnownImportedInterfaceMethods returns the lower-cased method set this
// package can model for a small allowlist of well-known imported interface
// types (io.Closer, the internal cypher executor interfaces). An unknown type
// returns nil, which callers treat as "any method may satisfy this target".
func KnownImportedInterfaceMethods(typeName string) []string {
	switch strings.TrimSpace(typeName) {
	case "io.Closer":
		return []string{"close"}
	case "sourcecypher.Executor", "cypher.Executor":
		return []string{"execute", "executegroup"}
	case "sourcecypher.GroupExecutor", "cypher.GroupExecutor":
		return []string{"executegroup"}
	default:
		return nil
	}
}

// ReferencedLocalInterfaces returns the lower-cased package-local interface
// names referenced by a type_identifier anywhere under node, restricted to
// names present in interfaceMethods.
func ReferencedLocalInterfaces(
	node *tree_sitter.Node,
	source []byte,
	interfaceMethods map[string][]string,
) []string {
	if node == nil {
		return nil
	}
	refs := make([]string, 0)
	shared.WalkNamed(node, func(child *tree_sitter.Node) {
		if child.Kind() != "type_identifier" {
			return
		}
		name := strings.ToLower(strings.TrimSpace(shared.NodeText(child, source)))
		if _, ok := interfaceMethods[name]; ok {
			refs = AppendUniqueImportAlias(refs, name)
		}
	})
	return refs
}

// InterfaceTargetFromTypeNode classifies a type-annotated node as a
// package-local interface reference, an imported interface reference, or
// neither.
func InterfaceTargetFromTypeNode(
	node *tree_sitter.Node,
	source []byte,
	interfaceMethods map[string][]string,
) InterfaceTarget {
	if node == nil {
		return InterfaceTarget{}
	}
	localRefs := ReferencedLocalInterfaces(node, source, interfaceMethods)
	if len(localRefs) > 0 {
		return InterfaceTarget{LocalInterface: localRefs[0]}
	}
	text := strings.TrimSpace(shared.NodeText(node, source))
	if strings.Contains(text, ".") {
		methods := KnownImportedInterfaceMethods(text)
		return InterfaceTarget{Imported: true, ImportedMethods: methods, AllowExportedMethods: len(methods) == 0}
	}
	return InterfaceTarget{}
}

// FunctionParamInterfaceTargets maps each package-level function name to the
// interface target modeled for each of its parameter positions, for
// parameters whose declared type names a package-local or known imported
// interface.
func FunctionParamInterfaceTargets(
	root *tree_sitter.Node,
	source []byte,
	interfaceMethods map[string][]string,
) map[string]map[int]InterfaceTarget {
	targets := make(map[string]map[int]InterfaceTarget)
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "function_declaration" {
			return
		}
		name := strings.ToLower(strings.TrimSpace(shared.NodeText(node.ChildByFieldName("name"), source)))
		if name == "" {
			return
		}
		paramIndex := 0
		params := node.ChildByFieldName("parameters")
		WalkDirectNamed(params, func(param *tree_sitter.Node) {
			if param.Kind() != "parameter_declaration" {
				return
			}
			target := InterfaceTargetFromTypeNode(param.ChildByFieldName("type"), source, interfaceMethods)
			nameCount := len(IdentifierNames(param.ChildByFieldName("name"), source))
			if nameCount == 0 {
				nameCount = 1
			}
			for range nameCount {
				if target.Modeled() {
					if _, ok := targets[name]; !ok {
						targets[name] = make(map[int]InterfaceTarget)
					}
					targets[name][paramIndex] = target
				}
				paramIndex++
			}
		})
	})
	return targets
}

// FunctionParamImportedInterfaceMethods reduces FunctionParamInterfaceTargets
// to the subset of parameter positions whose target is an imported interface
// with a known method set, keyed the same way
// shared.GoImportedInterfaceParamMethods is: by lower-case function name and
// parameter index.
func FunctionParamImportedInterfaceMethods(
	root *tree_sitter.Node,
	source []byte,
) shared.GoImportedInterfaceParamMethods {
	targets := FunctionParamInterfaceTargets(root, source, nil)
	importedMethods := make(shared.GoImportedInterfaceParamMethods)
	for functionName, byIndex := range targets {
		for index, target := range byIndex {
			if !target.Imported || len(target.ImportedMethods) == 0 {
				continue
			}
			if _, ok := importedMethods[functionName]; !ok {
				importedMethods[functionName] = make(map[int][]string)
			}
			importedMethods[functionName][index] = AppendUniqueMethods(
				importedMethods[functionName][index],
				target.ImportedMethods,
			)
		}
	}
	return importedMethods
}

// TypeParameterConstraintCandidates extracts the lower-cased identifier-like
// fields from one type_parameter_declaration node's text, excluding the
// parameter name itself (the first field) and the built-in `any` and
// `comparable` constraints.
func TypeParameterConstraintCandidates(text string) []string {
	fields := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return r != '_' && r != '.' && r != '*' && r != '~' &&
			(r < '0' || r > '9') &&
			(r < 'a' || r > 'z')
	})
	if len(fields) < 2 {
		return nil
	}
	names := make([]string, 0, len(fields))
	for _, field := range fields[1:] {
		field = strings.Trim(field, "*~")
		if field == "" || field == "any" || field == "comparable" {
			continue
		}
		names = AppendUniqueImportAlias(names, field)
	}
	return names
}
