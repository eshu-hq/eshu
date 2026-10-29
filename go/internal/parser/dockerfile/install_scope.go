// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package dockerfile

import "strings"

// installScope holds known source-level ARG and ENV values for one build stage.
// A child stage points to the completed parent stage; unrelated stages start
// empty. ENV wins over ARG even when the ARG is redeclared by a child.
type installScope struct {
	parent *installScope
	args   map[string]installValue
	envs   map[string]installValue
}

type installValue struct {
	text      string
	sensitive bool
}

func (scope *installScope) declareArg(decl argDecl, globals *baseRefResolver) {
	if scope == nil {
		return
	}
	if decl.hasDefault {
		if scope.args == nil {
			scope.args = make(map[string]installValue)
		}
		scope.args[decl.name] = installValue{decl.value, sensitiveInstallValue(decl.name, decl.value, scope)}
		return
	}
	if _, found := scope.arg(decl.name); found {
		return
	}
	value, state := globals.lookup(decl.name)
	if state != varKnown {
		return
	}
	if scope.args == nil {
		scope.args = make(map[string]installValue)
	}
	scope.args[decl.name] = installValue{value, sensitiveInstallValue(decl.name, value, scope)}
}

func (scope *installScope) declareEnv(name, value string) {
	if scope == nil {
		return
	}
	if scope.envs == nil {
		scope.envs = make(map[string]installValue)
	}
	scope.envs[name] = installValue{value, sensitiveInstallValue(name, value, scope)}
}

func (scope *installScope) arg(name string) (installValue, bool) {
	for current := scope; current != nil; current = current.parent {
		if value, ok := current.args[name]; ok {
			return value, true
		}
	}
	return installValue{}, false
}

func (scope *installScope) lookup(name string) (installValue, bool) {
	for current := scope; current != nil; current = current.parent {
		if value, ok := current.envs[name]; ok {
			return value, true
		}
	}
	return scope.arg(name)
}

func stageInstallScope(stage Stage, previous map[string]*installScope) *installScope {
	var parent *installScope
	if stage.BaseTag == "" {
		parent = previous[strings.ToLower(stage.BaseImage)]
	}
	scope := &installScope{parent: parent}
	if stage.Alias != "" {
		previous[strings.ToLower(stage.Alias)] = scope
	}
	return scope
}
