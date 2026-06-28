// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestCountInheritanceFactInputs locks the rc-12 diagnostic counters: every
// content_entity fact counts toward content_entity_facts, but only an inheritable
// entity type (Class/Interface/Struct/Trait/Protocol/Enum) that ALSO declares a
// parent (a base, implemented interface, or trait adaptation) counts toward
// entities_with_declared_parent. A Function (not inheritable), a parentless Class
// (genuinely empty inheritance input, not a resolution failure), and a non-
// content_entity fact must all be excluded from that second counter. These drive
// the handler's log so an intermittent rc-12 (INHERITS) gate flake is
// root-causable from logs (#3873).
func TestCountInheritanceFactInputs(t *testing.T) {
	t.Parallel()

	classWithBase := func(name, base string) facts.Envelope {
		return facts.Envelope{FactKind: "content_entity", Payload: map[string]any{
			"entity_type":     name,
			"entity_metadata": map[string]any{"bases": []any{base}},
		}}
	}

	envelopes := []facts.Envelope{
		{FactKind: factKindRepository, Payload: map[string]any{"repo_id": "repo-1"}},
		classWithBase("Class", "ParentClass"),   // inheritable + declares parent -> counted
		classWithBase("Interface", "BaseIface"), // inheritable + declares parent -> counted
		{FactKind: "content_entity", Payload: map[string]any{ // parentless Class -> NOT counted
			"entity_type": "Class", "entity_metadata": map[string]any{},
		}},
		classWithBase("Function", "Whatever"),                 // declares a base but not inheritable -> NOT counted
		{FactKind: "file", Payload: map[string]any{"x": "y"}}, // not content_entity
	}

	contentEntities, withParent := countInheritanceFactInputs(envelopes)
	if contentEntities != 4 {
		t.Fatalf("content_entity facts = %d, want 4 (two parented + one parentless Class + one Function)", contentEntities)
	}
	if withParent != 2 {
		t.Fatalf("entities_with_declared_parent = %d, want 2 (Class+Interface with bases; parentless Class + Function excluded)", withParent)
	}

	if ce, wp := countInheritanceFactInputs(nil); ce != 0 || wp != 0 {
		t.Fatalf("empty envelopes = (%d, %d), want (0, 0)", ce, wp)
	}
}
