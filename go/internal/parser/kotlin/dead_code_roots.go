package kotlin

import "strings"

// kotlinShortName reduces a dotted, generic, or nullable type reference to its
// simple name (last path segment without type arguments or `?`).
func kotlinShortName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	value = strings.TrimSuffix(value, "?")
	if openIndex := strings.Index(value, "("); openIndex >= 0 {
		value = value[:openIndex]
	}
	if dotIndex := strings.LastIndex(value, "."); dotIndex >= 0 {
		value = value[dotIndex+1:]
	}
	return strings.TrimSpace(value)
}

// kotlinTypeDeadCodeRootKinds classifies a class/interface declaration into
// bounded dead-code root kinds from its kind and annotation set.
func kotlinTypeDeadCodeRootKinds(annotations []string, kind string) []string {
	rootKinds := make([]string, 0, 2)
	if kind == "interface" {
		rootKinds = appendKotlinRootKind(rootKinds, "kotlin.interface_type")
	}
	if kotlinHasAnyAnnotation(annotations, "Component", "Service", "Repository", "Controller",
		"RestController", "Configuration", "SpringBootApplication", "ControllerAdvice",
		"RestControllerAdvice") {
		rootKinds = appendKotlinRootKind(rootKinds, "kotlin.spring_component_class")
	}
	if kotlinHasAnnotation(annotations, "ConfigurationProperties") {
		rootKinds = appendKotlinRootKind(rootKinds, "kotlin.spring_configuration_properties_class")
	}
	return rootKinds
}

// kotlinFunctionDeadCodeRootKinds classifies a function declaration into
// bounded dead-code root kinds from AST-derived evidence: whether it is an
// override, whether it has exactly one parameter, its annotations, name, and
// enclosing type membership.
func kotlinFunctionDeadCodeRootKinds(
	isOverride bool,
	hasOneParameter bool,
	annotations []string,
	name string,
	classContext string,
	scopeKind string,
	interfaces map[string]map[string]struct{},
	classInterfaces map[string]map[string]struct{},
) []string {
	rootKinds := make([]string, 0, 4)
	if classContext == "" && name == "main" {
		rootKinds = appendKotlinRootKind(rootKinds, "kotlin.main_function")
	}
	if scopeKind == "interface" {
		rootKinds = appendKotlinRootKind(rootKinds, "kotlin.interface_method")
	}
	if isOverride {
		rootKinds = appendKotlinRootKind(rootKinds, "kotlin.override_method")
	}
	if kotlinClassImplementsMethod(classContext, name, interfaces, classInterfaces) {
		rootKinds = appendKotlinRootKind(rootKinds, "kotlin.interface_implementation_method")
	}
	if isOverride && name == "apply" {
		rootKinds = appendKotlinRootKind(rootKinds, "kotlin.gradle_plugin_apply")
	}
	if kotlinHasAnnotation(annotations, "TaskAction") {
		rootKinds = appendKotlinRootKind(rootKinds, "kotlin.gradle_task_action")
	}
	if kotlinHasAnyAnnotation(annotations, "Input", "InputFile", "InputFiles", "InputDirectory",
		"OutputFile", "OutputFiles", "OutputDirectory", "OutputDirectories", "Nested",
		"Classpath", "CompileClasspath", "Internal", "Option") {
		rootKinds = appendKotlinRootKind(rootKinds, "kotlin.gradle_task_property")
	}
	if kotlinIsGradleTaskSetter(hasOneParameter, name, classContext, classInterfaces) {
		rootKinds = appendKotlinRootKind(rootKinds, "kotlin.gradle_task_setter")
	}
	if kotlinHasAnyAnnotation(annotations, "RequestMapping", "GetMapping", "PostMapping",
		"PutMapping", "DeleteMapping", "PatchMapping") {
		rootKinds = appendKotlinRootKind(rootKinds, "kotlin.spring_request_mapping_method")
	}
	if kotlinHasAnnotation(annotations, "Bean") {
		rootKinds = appendKotlinRootKind(rootKinds, "kotlin.spring_bean_method")
	}
	if kotlinHasAnnotation(annotations, "EventListener") {
		rootKinds = appendKotlinRootKind(rootKinds, "kotlin.spring_event_listener_method")
	}
	if kotlinHasAnyAnnotation(annotations, "Scheduled", "Schedules") {
		rootKinds = appendKotlinRootKind(rootKinds, "kotlin.spring_scheduled_method")
	}
	if kotlinHasAnyAnnotation(annotations, "PostConstruct", "PreDestroy") {
		rootKinds = appendKotlinRootKind(rootKinds, "kotlin.lifecycle_callback_method")
	}
	if kotlinHasAnyAnnotation(annotations, "Test", "ParameterizedTest", "RepeatedTest",
		"TestFactory", "TestTemplate") {
		rootKinds = appendKotlinRootKind(rootKinds, "kotlin.junit_test_method")
	}
	if kotlinHasAnyAnnotation(annotations, "BeforeEach", "AfterEach", "BeforeAll", "AfterAll") {
		rootKinds = appendKotlinRootKind(rootKinds, "kotlin.junit_lifecycle_method")
	}
	return rootKinds
}

// kotlinConstructorDeadCodeRootKinds returns the root kind for a secondary
// constructor.
func kotlinConstructorDeadCodeRootKinds() []string {
	return []string{"kotlin.constructor"}
}

func kotlinClassImplementsMethod(
	classContext string,
	name string,
	interfaces map[string]map[string]struct{},
	classInterfaces map[string]map[string]struct{},
) bool {
	if classContext == "" || name == "" || len(classInterfaces[classContext]) == 0 {
		return false
	}
	for interfaceName := range classInterfaces[classContext] {
		if _, ok := interfaces[interfaceName][name]; ok {
			return true
		}
	}
	return false
}

// kotlinIsGradleTaskSetter reports whether a single-parameter `setX` method on
// a Gradle task class is a JavaBean-style task setter root.
func kotlinIsGradleTaskSetter(
	hasOneParameter bool,
	name string,
	classContext string,
	classInterfaces map[string]map[string]struct{},
) bool {
	if !strings.HasPrefix(name, "set") || classContext == "" || !hasOneParameter {
		return false
	}
	for implementedType := range classInterfaces[classContext] {
		if implementedType == "DefaultTask" || implementedType == "Task" {
			return true
		}
	}
	return false
}

func kotlinHasAnnotation(annotations []string, want string) bool {
	for _, annotation := range annotations {
		if annotation == want {
			return true
		}
	}
	return false
}

func kotlinHasAnyAnnotation(annotations []string, wants ...string) bool {
	for _, want := range wants {
		if kotlinHasAnnotation(annotations, want) {
			return true
		}
	}
	return false
}

func appendKotlinRootKind(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}
