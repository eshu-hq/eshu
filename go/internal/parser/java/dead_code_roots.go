package java

import (
	"strings"
	"unicode"
	"unicode/utf8"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func javaDeadCodeRootKinds(
	node *tree_sitter.Node,
	source []byte,
	name string,
	methodReferences *javaMethodReferenceIndex,
) []string {
	rootKinds := make([]string, 0, 3)
	raw := nodeText(node, source)
	switch node.Kind() {
	case "constructor_declaration":
		rootKinds = appendUniqueString(rootKinds, "java.constructor")
	case "method_declaration":
		if javaIsMainMethod(raw, name) {
			rootKinds = appendUniqueString(rootKinds, "java.main_method")
		}
		if javaHasAnnotation(raw, "Override") {
			rootKinds = appendUniqueString(rootKinds, "java.override_method")
		}
		if javaIsAntTaskSetter(node, source, raw, name) {
			rootKinds = appendUniqueString(rootKinds, "java.ant_task_setter")
		}
		if javaIsGradleTaskSetter(node, source, raw, name) {
			rootKinds = appendUniqueString(rootKinds, "java.gradle_task_setter")
		}
		if javaIsGradleTaskInterfaceMethod(node, source) {
			rootKinds = appendUniqueString(rootKinds, "java.gradle_task_interface_method")
		}
		if javaIsGradlePluginApply(node, source, raw, name) {
			rootKinds = appendUniqueString(rootKinds, "java.gradle_plugin_apply")
		}
		if javaHasAnnotation(raw, "TaskAction") {
			rootKinds = appendUniqueString(rootKinds, "java.gradle_task_action")
		}
		if javaIsGradleTaskProperty(raw) {
			rootKinds = appendUniqueString(rootKinds, "java.gradle_task_property")
		}
		if javaIsGradleDSLPublicMethod(node, source, raw) {
			rootKinds = appendUniqueString(rootKinds, "java.gradle_dsl_public_method")
		}
		if javaIsMethodReferenceTarget(node, source, name, methodReferences) {
			rootKinds = appendUniqueString(rootKinds, "java.method_reference_target")
		}
		if javaIsSerializationHookMethod(node, source, name) {
			rootKinds = appendUniqueString(rootKinds, "java.serialization_hook_method")
		}
		if javaIsExternalizableHookMethod(node, source, name) {
			rootKinds = appendUniqueString(rootKinds, "java.externalizable_hook_method")
		}
		rootKinds = append(rootKinds, javaFrameworkMethodRootKinds(raw)...)
	}
	return rootKinds
}

func javaTypeDeadCodeRootKinds(node *tree_sitter.Node, source []byte) []string {
	name := strings.TrimSpace(nodeText(node.ChildByFieldName("name"), source))
	decorators := javaDecorators(node, source, name)
	rootKinds := make([]string, 0, 2)
	if javaDecoratorsHaveAnyAnnotation(decorators, []string{
		"Component",
		"Service",
		"Repository",
		"Controller",
		"RestController",
		"Configuration",
		"SpringBootApplication",
		"AutoConfiguration",
		"ControllerAdvice",
		"RestControllerAdvice",
	}) {
		rootKinds = appendUniqueString(rootKinds, "java.spring_component_class")
	}
	if javaDecoratorsHaveAnnotation(decorators, "ConfigurationProperties") {
		rootKinds = appendUniqueString(rootKinds, "java.spring_configuration_properties_class")
	}
	if javaDecoratorsHaveAnyAnnotation(decorators, []string{"Extension", "TestExtension"}) {
		rootKinds = appendUniqueString(rootKinds, "java.jenkins_extension_class")
	}
	if javaDecoratorsHaveAnnotation(decorators, "Symbol") {
		rootKinds = appendUniqueString(rootKinds, "java.jenkins_symbol_class")
	}
	return rootKinds
}

func javaFrameworkMethodRootKinds(raw string) []string {
	rootKinds := make([]string, 0, 3)
	if javaHasAnyAnnotation(raw, []string{
		"RequestMapping",
		"GetMapping",
		"PostMapping",
		"PutMapping",
		"DeleteMapping",
		"PatchMapping",
	}) {
		rootKinds = appendUniqueString(rootKinds, "java.spring_request_mapping_method")
	}
	if javaHasAnnotation(raw, "Bean") {
		rootKinds = appendUniqueString(rootKinds, "java.spring_bean_method")
	}
	if javaHasAnnotation(raw, "EventListener") {
		rootKinds = appendUniqueString(rootKinds, "java.spring_event_listener_method")
	}
	if javaHasAnyAnnotation(raw, []string{"Scheduled", "Schedules"}) {
		rootKinds = appendUniqueString(rootKinds, "java.spring_scheduled_method")
	}
	if javaHasAnyAnnotation(raw, []string{"PostConstruct", "PreDestroy"}) {
		rootKinds = appendUniqueString(rootKinds, "java.lifecycle_callback_method")
	}
	if javaHasAnyAnnotation(raw, []string{
		"Test",
		"ParameterizedTest",
		"RepeatedTest",
		"TestFactory",
		"TestTemplate",
	}) {
		rootKinds = appendUniqueString(rootKinds, "java.junit_test_method")
	}
	if javaHasAnyAnnotation(raw, []string{
		"BeforeEach",
		"AfterEach",
		"BeforeAll",
		"AfterAll",
	}) {
		rootKinds = appendUniqueString(rootKinds, "java.junit_lifecycle_method")
	}
	if javaHasAnnotation(raw, "Symbol") {
		rootKinds = appendUniqueString(rootKinds, "java.jenkins_symbol_method")
	}
	if javaHasAnnotation(raw, "Initializer") {
		rootKinds = appendUniqueString(rootKinds, "java.jenkins_initializer_method")
	}
	if javaHasAnnotation(raw, "DataBoundSetter") {
		rootKinds = appendUniqueString(rootKinds, "java.jenkins_databound_setter_method")
	}
	if javaHasAnyAnnotation(raw, []string{"RequirePOST", "POST", "GET", "WebMethod"}) {
		rootKinds = appendUniqueString(rootKinds, "java.stapler_web_method")
	}
	return rootKinds
}

func javaIsAntTaskSetter(node *tree_sitter.Node, source []byte, raw string, name string) bool {
	if !javaIsSetterName(name) {
		return false
	}
	normalized := javaNormalized(raw)
	if !strings.Contains(normalized, " public ") ||
		!strings.Contains(normalized, " void ") ||
		javaParameterCount(node) != 1 {
		return false
	}
	classHeader := javaNearestClassHeader(node, source)
	return strings.Contains(classHeader, " extends Task ") ||
		strings.Contains(classHeader, " extends org.apache.tools.ant.Task ")
}

func javaIsGradleTaskSetter(node *tree_sitter.Node, source []byte, raw string, name string) bool {
	if !javaIsSetterName(name) || javaParameterCount(node) != 1 {
		return false
	}
	normalized := javaNormalized(raw)
	if !strings.Contains(normalized, " public ") || !strings.Contains(normalized, " void ") {
		return false
	}
	classHeader := javaNearestClassHeader(node, source)
	return javaClassExtendsGradleTask(classHeader)
}

func javaIsGradleTaskInterfaceMethod(node *tree_sitter.Node, source []byte) bool {
	interfaceNode := javaNearestInterfaceDeclaration(node)
	if interfaceNode == nil {
		return false
	}
	header := javaClassDeclarationHeader(interfaceNode, source)
	return strings.Contains(header, " extends Task ") ||
		strings.Contains(header, " extends org.gradle.api.Task ")
}

func javaIsMethodReferenceTarget(
	node *tree_sitter.Node,
	source []byte,
	name string,
	methodReferences *javaMethodReferenceIndex,
) bool {
	classContext := nearestNamedAncestor(node, source, "class_declaration", "record_declaration")
	return methodReferences.hasTarget(classContext, strings.TrimSpace(name))
}

func javaIsGradlePluginApply(node *tree_sitter.Node, source []byte, raw string, name string) bool {
	if strings.TrimSpace(name) != "apply" || javaParameterCount(node) != 1 {
		return false
	}
	normalized := javaNormalized(raw)
	if !strings.Contains(normalized, " public ") || !strings.Contains(normalized, " void ") {
		return false
	}
	classHeader := javaNearestClassHeader(node, source)
	return strings.Contains(classHeader, " implements Plugin<") ||
		strings.Contains(classHeader, " implements org.gradle.api.Plugin<")
}

func javaIsGradleTaskProperty(raw string) bool {
	for _, annotation := range []string{
		"Input",
		"InputFile",
		"InputFiles",
		"InputDirectory",
		"OutputFile",
		"OutputFiles",
		"OutputDirectory",
		"OutputDirectories",
		"Nested",
		"Classpath",
		"CompileClasspath",
		"Internal",
		"Option",
	} {
		if javaHasAnnotation(raw, annotation) {
			return true
		}
	}
	return false
}

func javaIsSerializationHookMethod(node *tree_sitter.Node, source []byte, name string) bool {
	name = strings.TrimSpace(name)
	switch name {
	case "readObject":
		return javaHasVoidReturn(node, source) && javaHasParameterTypes(node, source, []string{"ObjectInputStream"})
	case "writeObject":
		return javaHasVoidReturn(node, source) && javaHasParameterTypes(node, source, []string{"ObjectOutputStream"})
	case "readResolve", "writeReplace":
		return javaParameterCount(node) == 0 && javaMethodReturnType(node, source) == "Object"
	default:
		return false
	}
}

func javaIsExternalizableHookMethod(node *tree_sitter.Node, source []byte, name string) bool {
	name = strings.TrimSpace(name)
	switch name {
	case "readExternal":
		return javaHasVoidReturn(node, source) && javaHasParameterTypes(node, source, []string{"ObjectInput"})
	case "writeExternal":
		return javaHasVoidReturn(node, source) && javaHasParameterTypes(node, source, []string{"ObjectOutput"})
	default:
		return false
	}
}

func javaHasVoidReturn(node *tree_sitter.Node, source []byte) bool {
	return javaMethodReturnType(node, source) == "void"
}

func javaHasParameterTypes(node *tree_sitter.Node, source []byte, want []string) bool {
	got := javaParameterTypes(node, source)
	if len(got) != len(want) {
		return false
	}
	for i := range want {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func javaMethodReturnType(node *tree_sitter.Node, source []byte) string {
	if node == nil || node.Kind() != "method_declaration" {
		return ""
	}
	typeNode := node.ChildByFieldName("type")
	if typeNode == nil {
		return ""
	}
	return javaTypeLeafName(nodeText(typeNode, source))
}

func javaIsGradleDSLPublicMethod(node *tree_sitter.Node, source []byte, raw string) bool {
	if !strings.Contains(javaNormalized(raw), " public ") ||
		strings.Contains(javaNormalized(raw), " static ") {
		return false
	}
	classNode := javaNearestClassDeclaration(node)
	if classNode == nil {
		return false
	}
	if !strings.Contains(string(source), "org.gradle.") {
		return false
	}
	className := strings.TrimSpace(nodeText(classNode.ChildByFieldName("name"), source))
	classHeader := javaClassDeclarationHeader(classNode, source)
	return strings.HasSuffix(className, "Extension") ||
		strings.HasSuffix(className, "Spec") ||
		javaClassExtendsGradleTask(classHeader)
}

func javaClassExtendsGradleTask(classHeader string) bool {
	for _, token := range []string{
		" extends DefaultTask ",
		" extends org.gradle.api.DefaultTask ",
		" extends Jar ",
		" extends org.gradle.api.tasks.bundling.Jar ",
		" extends War ",
		" extends org.gradle.api.tasks.bundling.War ",
		" extends JavaExec ",
		" extends org.gradle.api.tasks.JavaExec ",
	} {
		if strings.Contains(classHeader, token) {
			return true
		}
	}
	return false
}

func javaNearestClassHeader(node *tree_sitter.Node, source []byte) string {
	if classNode := javaNearestClassDeclaration(node); classNode != nil {
		return javaClassDeclarationHeader(classNode, source)
	}
	return ""
}

func javaNearestClassDeclaration(node *tree_sitter.Node) *tree_sitter.Node {
	for current := node.Parent(); current != nil; current = current.Parent() {
		if current.Kind() == "class_declaration" || current.Kind() == "record_declaration" {
			return current
		}
	}
	return nil
}

func javaNearestInterfaceDeclaration(node *tree_sitter.Node) *tree_sitter.Node {
	for current := node.Parent(); current != nil; current = current.Parent() {
		if current.Kind() == "interface_declaration" {
			return current
		}
	}
	return nil
}

func javaIsSetterName(name string) bool {
	trimmed := strings.TrimSpace(name)
	if len(trimmed) <= len("set") || !strings.HasPrefix(trimmed, "set") {
		return false
	}
	firstPropertyRune, _ := utf8.DecodeRuneInString(trimmed[len("set"):])
	return unicode.IsUpper(firstPropertyRune)
}

func javaClassDeclarationHeader(node *tree_sitter.Node, source []byte) string {
	raw := nodeText(node, source)
	if open := strings.Index(raw, "{"); open >= 0 {
		raw = raw[:open]
	}
	return javaNormalized(raw)
}

func javaIsMainMethod(raw string, name string) bool {
	normalized := javaNormalized(raw)
	return strings.TrimSpace(name) == "main" &&
		strings.Contains(normalized, " public ") &&
		strings.Contains(normalized, " static ") &&
		strings.Contains(normalized, " void ") &&
		strings.Contains(raw, "String")
}

func javaHasAnnotation(raw string, name string) bool {
	for _, decorator := range strings.Split(raw, "\n") {
		decorator = strings.TrimSpace(decorator)
		if decorator == "@"+name ||
			strings.HasPrefix(decorator, "@"+name+"(") ||
			strings.HasSuffix(decorator, "."+name) ||
			strings.Contains(decorator, "."+name+"(") {
			return true
		}
	}
	return false
}

func javaHasAnyAnnotation(raw string, names []string) bool {
	for _, name := range names {
		if javaHasAnnotation(raw, name) {
			return true
		}
	}
	return false
}

func javaDecoratorsHaveAnnotation(decorators []string, name string) bool {
	return javaHasAnnotation(strings.Join(decorators, "\n"), name)
}

func javaDecoratorsHaveAnyAnnotation(decorators []string, names []string) bool {
	for _, name := range names {
		if javaDecoratorsHaveAnnotation(decorators, name) {
			return true
		}
	}
	return false
}

func javaNormalized(raw string) string {
	return " " + strings.Join(strings.Fields(raw), " ") + " "
}
