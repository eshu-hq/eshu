// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"reflect"
	"strings"
	"testing"
)

// TestAdapterInterfaceForbidsExperimentRunAndMutation is the metadata-only
// acceptance gate for FIS: the SDK adapter must never start, stop, or mutate an
// experiment and must never read experiment run results or resolved-target
// inventories. We reflect over the adapter's read interface and confirm no
// experiment-run read, mutation, or start/stop method is reachable. This test
// fails the build if a future edit ever adds one of these to the adapter
// surface.
func TestAdapterInterfaceForbidsExperimentRunAndMutation(t *testing.T) {
	forbiddenSubstrings := []string{
		// experiment run reads / resolved-target inventories are data-plane.
		"Experiment", "ResolvedTargets", "TargetAccountConfiguration", "SafetyLever",
	}
	forbiddenPrefixes := []string{
		"Create", "Delete", "Update", "Put", "Start", "Stop",
		"Add", "Register", "Deregister", "Associate", "Disassociate",
		"Tag", "Untag",
	}
	// GetExperimentTemplate and ListExperimentTemplates legitimately contain
	// "Experiment" — they read template metadata, not experiment runs. They are
	// allowed by exact name so the substring guard can still ban experiment-run
	// reads like GetExperiment or ListExperiments.
	allowed := map[string]struct{}{
		"ListExperimentTemplates": {},
		"GetExperimentTemplate":   {},
		"ListTagsForResource":     {},
	}

	ifaceType := reflect.TypeOf((*apiClient)(nil)).Elem()
	if ifaceType.NumMethod() == 0 {
		t.Fatalf("apiClient has no methods; expected the FIS read surface")
	}
	for i := 0; i < ifaceType.NumMethod(); i++ {
		name := ifaceType.Method(i).Name
		if _, ok := allowed[name]; ok {
			continue
		}
		for _, banned := range forbiddenSubstrings {
			if strings.Contains(name, banned) {
				t.Fatalf("apiClient exposes forbidden experiment-run/mutation method %q; the FIS adapter is metadata-only", name)
			}
		}
		for _, prefix := range forbiddenPrefixes {
			if strings.HasPrefix(name, prefix) {
				t.Fatalf("apiClient exposes mutation method %q (prefix %q); the FIS adapter is metadata-only", name, prefix)
			}
		}
	}
}

// TestAdapterMethodsAreTemplateReads asserts every method on the adapter
// interface is one of the three allowed template-metadata reads so the read
// surface stays explicit and auditable. The scanner reads experiment-template
// metadata and resource tags only; it never reads experiment runs or resolved
// targets.
func TestAdapterMethodsAreTemplateReads(t *testing.T) {
	allowed := map[string]struct{}{
		"ListExperimentTemplates": {},
		"GetExperimentTemplate":   {},
		"ListTagsForResource":     {},
	}
	ifaceType := reflect.TypeOf((*apiClient)(nil)).Elem()
	for i := 0; i < ifaceType.NumMethod(); i++ {
		name := ifaceType.Method(i).Name
		if _, ok := allowed[name]; !ok {
			t.Fatalf("apiClient method %q is not an allowed FIS template read", name)
		}
	}
}
