package scala

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func scalaTypeDeadCodeRootKinds(kind string, text string) []string {
	rootKinds := make([]string, 0, 2)
	switch kind {
	case "trait_definition":
		rootKinds = appendScalaRootKind(rootKinds, "scala.trait_type")
	case "object_definition":
		if scalaExtendsAny(text, "App") {
			rootKinds = appendScalaRootKind(rootKinds, "scala.app_object")
		}
	}
	if scalaExtendsAny(text, "AnyFunSuite", "AnyFlatSpec", "AnyWordSpec", "AnyFreeSpec",
		"AnyFeatureSpec", "FunSuite", "FlatSpec", "WordSpec", "FreeSpec", "Specification") {
		rootKinds = appendScalaRootKind(rootKinds, "scala.scalatest_suite_class")
	}
	return rootKinds
}

func scalaFunctionDeadCodeRootKinds(
	text string,
	name string,
	typeContext string,
	contextKind string,
	traitMethods map[string]map[string]struct{},
	typeTraits map[string]map[string]struct{},
) []string {
	rootKinds := make([]string, 0, 3)
	if name == "main" && (typeContext == "" || contextKind == "object_definition") {
		rootKinds = appendScalaRootKind(rootKinds, "scala.main_method")
	}
	if contextKind == "trait_definition" {
		rootKinds = appendScalaRootKind(rootKinds, "scala.trait_method")
	}
	if strings.Contains(text, "override def ") {
		rootKinds = appendScalaRootKind(rootKinds, "scala.override_method")
	}
	if scalaTypeImplementsMethod(typeContext, name, traitMethods, typeTraits) {
		rootKinds = appendScalaRootKind(rootKinds, "scala.trait_implementation_method")
	}
	if scalaIsPlayControllerAction(text, typeTraits[typeContext]) {
		rootKinds = appendScalaRootKind(rootKinds, "scala.play_controller_action")
	}
	if name == "receive" && scalaExtendsType(typeTraits[typeContext], "Actor", "AbstractActor") {
		rootKinds = appendScalaRootKind(rootKinds, "scala.akka_actor_receive")
	}
	if scalaHasAnnotation(text, "PostConstruct", "PreDestroy") {
		rootKinds = appendScalaRootKind(rootKinds, "scala.lifecycle_callback_method")
	}
	if scalaHasAnnotation(text, "Test", "ParameterizedTest", "RepeatedTest", "TestFactory", "TestTemplate") {
		rootKinds = appendScalaRootKind(rootKinds, "scala.junit_test_method")
	}
	return rootKinds
}

func scalaIsPlayControllerAction(text string, implemented map[string]struct{}) bool {
	if !scalaExtendsType(implemented, "InjectedController", "BaseController", "AbstractController", "Controller") {
		return false
	}
	return strings.Contains(text, "Action") || strings.Contains(text, "EssentialAction")
}

func scalaCollectTypeContracts(root *tree_sitter.Node, source []byte) (
	map[string]map[string]struct{},
	map[string]map[string]struct{},
) {
	traitMethods := make(map[string]map[string]struct{})
	typeTraits := make(map[string]map[string]struct{})
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		switch node.Kind() {
		case "class_definition", "object_definition":
			name := scalaNodeName(node, source)
			if name == "" {
				return
			}
			typeTraits[name] = scalaImplementedTypeSet(shared.NodeText(node, source))
		case "trait_definition":
			name := scalaNodeName(node, source)
			if name == "" {
				return
			}
			typeTraits[name] = scalaImplementedTypeSet(shared.NodeText(node, source))
		case "function_definition", "function_declaration":
			if nearestAncestorKind(node, "trait_definition") != "trait_definition" {
				return
			}
			traitName := nearestNamedAncestor(node, source, "trait_definition")
			functionName := scalaNodeName(node, source)
			if traitName == "" || functionName == "" {
				return
			}
			if traitMethods[traitName] == nil {
				traitMethods[traitName] = make(map[string]struct{})
			}
			traitMethods[traitName][functionName] = struct{}{}
		}
	})
	return traitMethods, typeTraits
}

func scalaImplementedTypeSet(text string) map[string]struct{} {
	names := scalaImplementedTypes(text)
	if len(names) == 0 {
		return nil
	}
	values := make(map[string]struct{}, len(names))
	for _, name := range names {
		values[name] = struct{}{}
	}
	return values
}

func scalaImplementedTypes(text string) []string {
	extendsIndex := strings.Index(text, "extends")
	if extendsIndex < 0 {
		return nil
	}
	tail := text[extendsIndex+len("extends"):]
	if braceIndex := strings.Index(tail, "{"); braceIndex >= 0 {
		tail = tail[:braceIndex]
	}
	tail = strings.ReplaceAll(tail, "with", ",")
	parts := strings.Split(tail, ",")
	types := make([]string, 0, len(parts))
	for _, part := range parts {
		name := scalaShortTypeName(part)
		if name == "" {
			continue
		}
		types = append(types, name)
	}
	return types
}

func scalaShortTypeName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if openIndex := strings.Index(value, "("); openIndex >= 0 {
		value = value[:openIndex]
	}
	if genericIndex := strings.Index(value, "["); genericIndex >= 0 {
		value = value[:genericIndex]
	}
	fields := strings.Fields(value)
	if len(fields) > 0 {
		value = fields[0]
	}
	if dotIndex := strings.LastIndex(value, "."); dotIndex >= 0 {
		value = value[dotIndex+1:]
	}
	return strings.TrimSpace(value)
}

func scalaExtendsAny(text string, names ...string) bool {
	implemented := scalaImplementedTypeSet(text)
	return scalaExtendsType(implemented, names...)
}

func scalaExtendsType(implemented map[string]struct{}, names ...string) bool {
	for _, name := range names {
		if _, ok := implemented[name]; ok {
			return true
		}
	}
	return false
}

func scalaTypeImplementsMethod(
	typeContext string,
	name string,
	traitMethods map[string]map[string]struct{},
	typeTraits map[string]map[string]struct{},
) bool {
	if typeContext == "" || name == "" || len(typeTraits[typeContext]) == 0 {
		return false
	}
	for traitName := range typeTraits[typeContext] {
		if _, ok := traitMethods[traitName][name]; ok {
			return true
		}
	}
	return false
}

func scalaHasAnnotation(text string, names ...string) bool {
	for _, name := range names {
		if strings.Contains(text, "@"+name) || strings.Contains(text, "@"+name+"(") ||
			strings.Contains(text, "@org.junit."+name) || strings.Contains(text, "@jakarta.annotation."+name) {
			return true
		}
	}
	return false
}

func appendScalaRootKind(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}
