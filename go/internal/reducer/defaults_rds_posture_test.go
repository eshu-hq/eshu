// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import "testing"

func TestImplementedDefaultDomainDefinitionsOmitsRDSPostureWithoutNodeWriter(t *testing.T) {
	t.Parallel()

	definitions := implementedDefaultDomainDefinitions(DefaultHandlers{
		FactLoader: &stubFactLoader{},
	})
	for _, def := range definitions {
		if def.Domain == DomainRDSPostureMaterialization {
			t.Fatalf("rds_posture_materialization registered without node writer; want omitted to avoid silent intent drops")
		}
	}
}

func TestImplementedDefaultDomainDefinitionsIncludesRDSPostureWhenWired(t *testing.T) {
	t.Parallel()

	loader := &stubFactLoader{}
	writer := &recordingRDSPostureNodeWriter{}
	definitions := implementedDefaultDomainDefinitions(DefaultHandlers{
		FactLoader:           loader,
		RDSPostureNodeWriter: writer,
		ReadinessLookup:      readyLookup(true, true),
	})
	found := false
	for _, def := range definitions {
		if def.Domain != DomainRDSPostureMaterialization {
			continue
		}
		found = true
		handler, ok := def.Handler.(RDSPostureMaterializationHandler)
		if !ok {
			t.Fatalf("rds_posture_materialization handler type = %T, want RDSPostureMaterializationHandler", def.Handler)
		}
		if handler.FactLoader != loader {
			t.Fatal("rds_posture_materialization handler FactLoader was not wired")
		}
		if handler.NodeWriter != writer {
			t.Fatal("rds_posture_materialization handler NodeWriter was not wired")
		}
		if handler.ReadinessLookup == nil {
			t.Fatal("rds_posture_materialization handler ReadinessLookup was not wired")
		}
		if !def.Ownership.CanonicalWrite {
			t.Fatal("rds_posture_materialization must declare CanonicalWrite ownership")
		}
	}
	if !found {
		t.Fatal("rds_posture_materialization not registered after wiring loader+node writer")
	}
}
