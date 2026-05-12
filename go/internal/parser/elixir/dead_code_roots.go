package elixir

import "strings"

type elixirDeadCodeFacts struct {
	moduleUses map[string]map[string]struct{}
}

func newElixirDeadCodeFacts() elixirDeadCodeFacts {
	return elixirDeadCodeFacts{
		moduleUses: map[string]map[string]struct{}{},
	}
}

func recordElixirModule(facts elixirDeadCodeFacts, name string) {
	if name == "" {
		return
	}
	if facts.moduleUses[name] == nil {
		facts.moduleUses[name] = map[string]struct{}{}
	}
}

func recordElixirUse(facts elixirDeadCodeFacts, moduleName string, trimmed string, keyword string, paths []string) {
	if moduleName == "" || keyword != "use" {
		return
	}
	recordElixirModule(facts, moduleName)
	for _, path := range paths {
		recordElixirUseKind(facts, moduleName, path)
	}
	if strings.Contains(trimmed, ":controller") {
		facts.moduleUses[moduleName]["phoenix_controller"] = struct{}{}
	}
	if strings.Contains(trimmed, ":live_view") {
		facts.moduleUses[moduleName]["phoenix_live_view"] = struct{}{}
	}
	if strings.Contains(trimmed, ":live_component") {
		facts.moduleUses[moduleName]["phoenix_live_component"] = struct{}{}
	}
}

func recordElixirUseKind(facts elixirDeadCodeFacts, moduleName string, path string) {
	switch elixirShortModuleName(path) {
	case "Application":
		facts.moduleUses[moduleName]["application"] = struct{}{}
	case "GenServer":
		facts.moduleUses[moduleName]["genserver"] = struct{}{}
	case "Supervisor", "DynamicSupervisor":
		facts.moduleUses[moduleName]["supervisor"] = struct{}{}
	case "LiveView":
		facts.moduleUses[moduleName]["phoenix_live_view"] = struct{}{}
	case "LiveComponent":
		facts.moduleUses[moduleName]["phoenix_live_component"] = struct{}{}
	case "Controller":
		facts.moduleUses[moduleName]["phoenix_controller"] = struct{}{}
	case "Task":
		if strings.HasSuffix(path, "Mix.Task") || path == "Mix.Task" {
			facts.moduleUses[moduleName]["mix_task"] = struct{}{}
		}
	}
}

func recordElixirBehaviour(facts elixirDeadCodeFacts, moduleName string, behaviour string) {
	if moduleName == "" {
		return
	}
	recordElixirModule(facts, moduleName)
	if elixirShortModuleName(behaviour) == "Application" {
		facts.moduleUses[moduleName]["application"] = struct{}{}
	}
}

func elixirFunctionDeadCodeRootKinds(
	keyword string,
	name string,
	args []string,
	moduleName string,
	moduleKind string,
	pendingImpl bool,
	facts elixirDeadCodeFacts,
) []string {
	rootKinds := make([]string, 0, 3)
	if elixirIsApplicationStart(moduleName, name, args, facts) {
		rootKinds = appendElixirRootKind(rootKinds, "elixir.application_start")
	}
	if keyword == "defmacro" {
		rootKinds = appendElixirRootKind(rootKinds, "elixir.public_macro")
	}
	if keyword == "defguard" {
		rootKinds = appendElixirRootKind(rootKinds, "elixir.public_guard")
	}
	switch moduleKind {
	case "protocol":
		rootKinds = appendElixirRootKind(rootKinds, "elixir.protocol_function")
	case "protocol_implementation":
		rootKinds = appendElixirRootKind(rootKinds, "elixir.protocol_implementation_function")
	}
	if pendingImpl {
		rootKinds = appendElixirRootKind(rootKinds, "elixir.behaviour_callback")
	}
	if elixirModuleHasUse(facts, moduleName, "genserver") && elixirIsGenServerCallback(name, args) {
		rootKinds = appendElixirRootKind(rootKinds, "elixir.genserver_callback")
	}
	if elixirModuleHasUse(facts, moduleName, "supervisor") && elixirIsSupervisorCallback(name, args) {
		rootKinds = appendElixirRootKind(rootKinds, "elixir.supervisor_callback")
	}
	if elixirIsMixTaskRun(moduleName, name, args, facts) {
		rootKinds = appendElixirRootKind(rootKinds, "elixir.mix_task_run")
	}
	if keyword == "def" && elixirIsPhoenixControllerAction(moduleName, args, facts) {
		rootKinds = appendElixirRootKind(rootKinds, "elixir.phoenix_controller_action")
	}
	if elixirIsLiveViewCallback(moduleName, name, args, facts) {
		rootKinds = appendElixirRootKind(rootKinds, "elixir.phoenix_liveview_callback")
	}
	return rootKinds
}

func elixirCurrentModuleKind(scopes []scope) string {
	for index := len(scopes) - 1; index >= 0; index-- {
		scope := scopes[index]
		if scope.kind != "module" || scope.item == nil {
			continue
		}
		if moduleKind, _ := scope.item["module_kind"].(string); moduleKind != "" {
			return moduleKind
		}
	}
	return ""
}

func markElixirObservedExactnessBlockers(scopes []scope, line string) {
	if !strings.Contains(line, "apply(") {
		return
	}
	for index := len(scopes) - 1; index >= 0; index-- {
		scope := scopes[index]
		if scope.kind != "function" || scope.item == nil {
			continue
		}
		scope.item["exactness_blockers"] = appendElixirMetadataString(
			scope.item["exactness_blockers"],
			"dynamic_dispatch_unresolved",
		)
		return
	}
}

func markElixirObservedExactnessBlockersOnItem(item map[string]any, line string) {
	if item == nil || !strings.Contains(line, "apply(") {
		return
	}
	item["exactness_blockers"] = appendElixirMetadataString(
		item["exactness_blockers"],
		"dynamic_dispatch_unresolved",
	)
}

func elixirModuleHasUse(facts elixirDeadCodeFacts, moduleName string, useKind string) bool {
	if moduleName == "" || useKind == "" {
		return false
	}
	_, ok := facts.moduleUses[moduleName][useKind]
	return ok
}

func elixirIsApplicationStart(moduleName string, name string, args []string, facts elixirDeadCodeFacts) bool {
	return name == "start" && len(args) == 2 && elixirModuleHasUse(facts, moduleName, "application")
}

func appendElixirMetadataString(existing any, value string) []string {
	values, _ := existing.([]string)
	for _, got := range values {
		if got == value {
			return values
		}
	}
	return append(values, value)
}

func elixirIsGenServerCallback(name string, args []string) bool {
	switch name {
	case "init":
		return len(args) == 1
	case "handle_call":
		return len(args) == 3
	case "handle_cast", "handle_continue", "handle_info", "terminate", "format_status":
		return len(args) == 2
	case "code_change":
		return len(args) == 3
	default:
		return false
	}
}

func elixirIsSupervisorCallback(name string, args []string) bool {
	switch name {
	case "init":
		return len(args) == 1
	default:
		return false
	}
}

func elixirIsMixTaskRun(moduleName string, name string, args []string, facts elixirDeadCodeFacts) bool {
	if name != "run" || len(args) != 1 {
		return false
	}
	return strings.HasPrefix(moduleName, "Mix.Tasks.") || elixirModuleHasUse(facts, moduleName, "mix_task")
}

func elixirIsPhoenixControllerAction(moduleName string, args []string, facts elixirDeadCodeFacts) bool {
	return len(args) == 2 && elixirModuleHasUse(facts, moduleName, "phoenix_controller")
}

func elixirIsLiveViewCallback(moduleName string, name string, args []string, facts elixirDeadCodeFacts) bool {
	if !elixirModuleHasUse(facts, moduleName, "phoenix_live_view") &&
		!elixirModuleHasUse(facts, moduleName, "phoenix_live_component") {
		return false
	}
	switch name {
	case "mount", "handle_event", "handle_params":
		return len(args) == 3
	case "handle_info", "update":
		return len(args) == 2
	case "render":
		return len(args) == 1
	default:
		return false
	}
}

func elixirShortModuleName(path string) string {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return ""
	}
	if dotIndex := strings.LastIndex(trimmed, "."); dotIndex >= 0 {
		return trimmed[dotIndex+1:]
	}
	return trimmed
}

func appendElixirRootKind(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}
