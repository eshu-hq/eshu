// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestCountInheritanceFactInputs locks the rc-12 diagnostic counters: only
// content_entity facts are counted, and only inheritable entity types
// (Class/Interface/Struct/Trait/Protocol/Enum) count toward inheritable —
// Function and non-content_entity facts are excluded. These counters drive the
// inheritance handler's completion log so an intermittent rc-12 (INHERITS) gate
// flake is root-causable from logs (#3873).
func TestCountInheritanceFactInputs(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{FactKind: factKindRepository, Payload: map[string]any{"repo_id": "repo-1"}},
		{FactKind: "content_entity", Payload: map[string]any{"entity_type": "Class"}},
		{FactKind: "content_entity", Payload: map[string]any{"entity_type": "Interface"}},
		{FactKind: "content_entity", Payload: map[string]any{"entity_type": "Struct"}},
		{FactKind: "content_entity", Payload: map[string]any{"entity_type": "Function"}}, // not inheritable
		{FactKind: "file", Payload: map[string]any{"relative_path": "x.go"}},             // not content_entity
	}

	contentEntities, inheritable := countInheritanceFactInputs(envelopes)
	if contentEntities != 4 {
		t.Fatalf("content_entity facts = %d, want 4 (Class, Interface, Struct, Function)", contentEntities)
	}
	if inheritable != 3 {
		t.Fatalf("inheritable entities = %d, want 3 (Class, Interface, Struct; Function excluded)", inheritable)
	}

	if ce, inh := countInheritanceFactInputs(nil); ce != 0 || inh != 0 {
		t.Fatalf("empty envelopes = (%d, %d), want (0, 0)", ce, inh)
	}
}
