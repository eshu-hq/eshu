// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import "testing"

// TestImplementedDefaultDomainDefinitionsOmitsIAMCanAssumeMaterializationWithoutEdgeWriter
// proves the additive registration gate: with a FactLoader but no CAN_ASSUME edge
// writer the trust-edge domain must stay unregistered, mirroring the
// aws_relationship_materialization gate, so intents are never silently dropped.
func TestImplementedDefaultDomainDefinitionsOmitsIAMCanAssumeMaterializationWithoutEdgeWriter(t *testing.T) {
	t.Parallel()

	definitions := implementedDefaultDomainDefinitions(DefaultHandlers{
		FactLoader: &stubFactLoader{},
	})
	for _, def := range definitions {
		if def.Domain == DomainIAMCanAssumeMaterialization {
			t.Fatalf("iam_can_assume_materialization registered without edge writer; want omitted to avoid silent intent drops")
		}
	}
}

// TestImplementedDefaultDomainDefinitionsIncludesIAMCanAssumeMaterializationWhenWired
// proves the CAN_ASSUME edge domain registers with the gated handler once both
// the FactLoader and the edge writer are wired, including the readiness lookup
// that holds edges until the canonical IAM CloudResource nodes commit.
func TestImplementedDefaultDomainDefinitionsIncludesIAMCanAssumeMaterializationWhenWired(t *testing.T) {
	t.Parallel()

	loader := &stubFactLoader{}
	writer := &recordingIAMCanAssumeEdgeWriter{}
	definitions := implementedDefaultDomainDefinitions(DefaultHandlers{
		FactLoader:             loader,
		IAMCanAssumeEdgeWriter: writer,
		ReadinessLookup:        readyLookup(true, true),
	})
	found := false
	for _, def := range definitions {
		if def.Domain != DomainIAMCanAssumeMaterialization {
			continue
		}
		found = true
		handler, ok := def.Handler.(IAMCanAssumeMaterializationHandler)
		if !ok {
			t.Fatalf("iam_can_assume_materialization handler type = %T, want IAMCanAssumeMaterializationHandler", def.Handler)
		}
		if handler.FactLoader != loader {
			t.Fatal("iam_can_assume_materialization handler FactLoader was not wired")
		}
		if handler.EdgeWriter != writer {
			t.Fatal("iam_can_assume_materialization handler EdgeWriter was not wired")
		}
		if handler.ReadinessLookup == nil {
			t.Fatal("iam_can_assume_materialization handler ReadinessLookup was not wired")
		}
		if !def.Ownership.CanonicalWrite {
			t.Fatal("iam_can_assume_materialization must declare CanonicalWrite ownership")
		}
	}
	if !found {
		t.Fatal("iam_can_assume_materialization not registered after wiring loader+edge writer")
	}
}
