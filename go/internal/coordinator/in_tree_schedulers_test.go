// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

const testComponentActivationConfig = `{
	"schema_version":"eshu.component.instance.v1",
	"component_id":"dev.eshu.collector.extracted",
	"component_version":"0.1.0",
	"publisher":"eshu-hq",
	"manifest_digest":"sha256:1234",
	"config_handle":"component-config:abcd",
	"runtime":{"sdk_protocol":"collector-sdk/v1alpha1","adapter":"process"}
}`

// componentOwnedConfigurations are configurations the generic component
// scheduler claims, including malformed component documents it must report.
var componentOwnedConfigurations = map[string]string{
	"valid component document": testComponentActivationConfig,
	"component document missing component_id": `{"schema_version":"eshu.component.instance.v1",` +
		`"component_version":"0.1.0"}`,
	"component id with wrong schema version": `{"schema_version":"other.v1","component_id":"dev.eshu.x"}`,
	"malformed json":                         `{not json`,
}

// inTreeOwnedConfigurations are configurations an in-tree kind scheduler keeps.
var inTreeOwnedConfigurations = map[string]string{
	"blank":                    "",
	"whitespace":               "  \n",
	"unrelated json object":    `{"foo":"bar"}`,
	"json null":                `null`,
	"empty json object":        `{}`,
	"unrelated schema version": `{"schema_version":"other.v1"}`,
	"in-tree targets":          `{"targets":[{"scope_id":"a"}]}`,
}

func selectorTestInstance(kind scope.CollectorKind, configuration string) workflow.CollectorInstance {
	now := time.Date(2026, time.October, 23, 18, 0, 0, 0, time.UTC)
	return workflow.CollectorInstance{
		InstanceID:     "selector-" + string(kind),
		CollectorKind:  kind,
		Mode:           workflow.CollectorModeScheduled,
		Enabled:        true,
		ClaimsEnabled:  true,
		Configuration:  configuration,
		LastObservedAt: now,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
}

// TestInTreeSelectorsLeaveComponentConfiguredInstancesToGenericScheduler is the
// #8919 ownership contract. Every selector the coordinator runs is held to it:
// an enabled, claims-enabled instance whose configuration is a component
// activation document (valid or malformed) is claimed by the generic component
// scheduler and by no in-tree kind scheduler, while blank and in-tree target
// configuration, and the enabled and claims gates, behave as before.
func TestInTreeSelectorsLeaveComponentConfiguredInstancesToGenericScheduler(t *testing.T) {
	t.Parallel()

	for _, selector := range allInstanceSelectors() {
		selector := selector
		t.Run(selector.name+"/"+string(selector.kind), func(t *testing.T) {
			t.Parallel()

			generic := selector.kind == ""
			kind := selector.kind
			if generic {
				kind = scope.CollectorPagerDuty // any kind: the generic selector ignores it
			}
			for name, configuration := range componentOwnedConfigurations {
				if got := selector.selects(selectorTestInstance(kind, configuration)); got != generic {
					t.Errorf("%s: selects(%s) = %v, want %v", selector.name, name, got, generic)
				}
			}
			for name, configuration := range inTreeOwnedConfigurations {
				if got := selector.selects(selectorTestInstance(kind, configuration)); got == generic {
					t.Errorf("%s: selects(%s) = %v, want %v", selector.name, name, got, !generic)
				}
			}
			for name, configuration := range map[string]string{
				"component": testComponentActivationConfig,
				"in-tree":   "",
			} {
				disabled := selectorTestInstance(kind, configuration)
				disabled.Enabled = false
				if selector.selects(disabled) {
					t.Errorf("%s: selects(disabled %s instance) = true, want false", selector.name, name)
				}
				unclaimed := selectorTestInstance(kind, configuration)
				unclaimed.ClaimsEnabled = false
				if selector.selects(unclaimed) {
					t.Errorf("%s: selects(claims-disabled %s instance) = true, want false", selector.name, name)
				}
			}
			if !generic && selector.selects(selectorTestInstance(scope.CollectorGit, "")) {
				t.Errorf("%s: selects(a %q instance) = true, want false", selector.name, scope.CollectorGit)
			}
		})
	}
}

// TestEveryScheduleSelectorIsRegistered fails when a shouldSchedule* function is
// declared in the package but missing from the registries the ownership test
// iterates, and when a registry entry names a selector that is not declared or
// is wired to a different function.
func TestEveryScheduleSelectorIsRegistered(t *testing.T) {
	t.Parallel()

	declared := map[string]bool{}
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package directory: %v", err)
	}
	fileSet := token.NewFileSet()
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fileSet, name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, decl := range file.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok && fn.Recv == nil && strings.HasPrefix(fn.Name.Name, "shouldSchedule") {
				declared[fn.Name.Name] = true
			}
		}
	}

	registered := map[string]bool{}
	for _, selector := range allInstanceSelectors() {
		registered[selector.name] = true
		resolved := runtime.FuncForPC(reflect.ValueOf(selector.selects).Pointer()).Name()
		if !strings.Contains(resolved, ".func") && !strings.HasSuffix(resolved, "."+selector.name) {
			t.Errorf("registry entry %q is wired to %q", selector.name, resolved)
		}
	}
	for name := range declared {
		if !registered[name] {
			t.Errorf("%s is declared but not registered in reconcileSchedulers or incidentFreshnessSelectors", name)
		}
	}
	for name := range registered {
		if !declared[name] {
			t.Errorf("registry names %s, which is not declared", name)
		}
	}
	if len(declared) == 0 {
		t.Fatal("found no shouldSchedule* declarations; the source scan is broken")
	}
}

func TestReconcileSchedulersCoverEveryReconcileKindOnce(t *testing.T) {
	t.Parallel()

	var names []string
	for _, scheduler := range reconcileSchedulers() {
		if scheduler.schedule == nil || scheduler.selects == nil {
			t.Fatalf("scheduler %q has no schedule or selects function", scheduler.name)
		}
		names = append(names, scheduler.name)
	}
	sort.Strings(names)
	for i := 1; i < len(names); i++ {
		if names[i] == names[i-1] {
			t.Fatalf("scheduler %q is registered twice", names[i])
		}
	}
}
