package kotlin

import "strings"

func kotlinAnnotations(line string) []string {
	matches := kotlinAnnotationPattern.FindAllStringSubmatch(line, -1)
	if len(matches) == 0 {
		return nil
	}
	annotations := make([]string, 0, len(matches))
	for _, match := range matches {
		if len(match) != 2 {
			continue
		}
		annotation := strings.TrimSpace(match[1])
		if annotation == "" {
			continue
		}
		annotations = append(annotations, kotlinShortName(annotation))
	}
	return annotations
}

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

func kotlinTypeDeadCodeRootKinds(trimmed string, annotations []string, kind string) []string {
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

func kotlinTypeItem(name string, lineNumber int, annotations []string, kind string, trimmed string) map[string]any {
	item := map[string]any{
		"name":        name,
		"line_number": lineNumber,
		"end_line":    lineNumber,
		"lang":        "kotlin",
	}
	if rootKinds := kotlinTypeDeadCodeRootKinds(trimmed, annotations, kind); len(rootKinds) > 0 {
		item["dead_code_root_kinds"] = rootKinds
	}
	return item
}

func kotlinFunctionDeadCodeRootKinds(
	trimmed string,
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
	if strings.Contains(trimmed, "override fun ") {
		rootKinds = appendKotlinRootKind(rootKinds, "kotlin.override_method")
	}
	if kotlinClassImplementsMethod(classContext, name, interfaces, classInterfaces) {
		rootKinds = appendKotlinRootKind(rootKinds, "kotlin.interface_implementation_method")
	}
	if kotlinIsGradlePluginApply(trimmed, name) {
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
	if kotlinIsGradleTaskSetter(trimmed, name, classContext, classInterfaces) {
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

func kotlinImplementedTypes(line string) []string {
	colonIndex := strings.Index(line, ":")
	if colonIndex < 0 {
		return nil
	}
	tail := line[colonIndex+1:]
	if braceIndex := strings.Index(tail, "{"); braceIndex >= 0 {
		tail = tail[:braceIndex]
	}
	parts := strings.Split(tail, ",")
	types := make([]string, 0, len(parts))
	for _, part := range parts {
		name := kotlinShortName(strings.TrimSpace(part))
		name = strings.TrimSuffix(strings.TrimSuffix(name, "()"), "?")
		if name == "" {
			continue
		}
		if genericIndex := strings.Index(name, "<"); genericIndex >= 0 {
			name = name[:genericIndex]
		}
		types = append(types, name)
	}
	return types
}

func kotlinIsGradlePluginApply(line string, name string) bool {
	return name == "apply" && strings.Contains(line, "override fun apply")
}

func kotlinIsGradleTaskSetter(
	line string,
	name string,
	classContext string,
	classInterfaces map[string]map[string]struct{},
) bool {
	if !strings.HasPrefix(name, "set") || classContext == "" {
		return false
	}
	if !kotlinHasOneParameter(line) {
		return false
	}
	for implementedType := range classInterfaces[classContext] {
		if implementedType == "DefaultTask" || implementedType == "Task" {
			return true
		}
	}
	return false
}

func kotlinHasOneParameter(line string) bool {
	openIndex := strings.Index(line, "(")
	if openIndex < 0 {
		return false
	}
	closeIndex := kotlinMatchingParenIndex(line, openIndex)
	if closeIndex <= openIndex {
		return false
	}
	parameters := strings.TrimSpace(line[openIndex+1 : closeIndex])
	return parameters != "" && !strings.Contains(parameters, ",")
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
