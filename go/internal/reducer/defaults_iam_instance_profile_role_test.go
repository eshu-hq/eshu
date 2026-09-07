// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer/iaminstprofile"
)

// stubIAMInstanceProfileRoleEdgeWriter is a minimal root-side fake satisfying
// iaminstprofile.IAMInstanceProfileRoleEdgeWriter. This wiring test only proves
// the writer identity was threaded through to the handler, never exercising
// its methods, so it does not need the family's recording behavior -- and Go
// test files cannot share an unexported test type
// (recordingIAMInstanceProfileRoleEdgeWriter) across the reducer/iaminstprofile
// package boundary (issue #6061).
// The id field is load-bearing: a zero-size struct compares equal to every
// other instance of itself, so an identity assertion over a struct{} value
// passes even when a DIFFERENT writer reaches the handler. A pointer to an
// empty struct is not sufficient either -- Go permits distinct zero-size
// allocations to share an address -- so the field is what makes the
// comparison sound rather than lucky.
type stubIAMInstanceProfileRoleEdgeWriter struct{ id int }

func (stubIAMInstanceProfileRoleEdgeWriter) WriteIAMInstanceProfileRoleEdges(context.Context, []map[string]any, string, string, string) error {
	return nil
}

func (stubIAMInstanceProfileRoleEdgeWriter) RetractIAMInstanceProfileRoleEdges(context.Context, []string, string, string) error {
	return nil
}

func TestImplementedDefaultDomainDefinitionsIncludesIAMInstanceProfileRoleWhenWired(t *testing.T) {
	t.Parallel()

	loader := &stubFactLoader{}
	writer := &stubIAMInstanceProfileRoleEdgeWriter{id: 1}
	definitions := implementedDefaultDomainDefinitions(DefaultHandlers{
		FactLoader:                       loader,
		IAMInstanceProfileRoleEdgeWriter: writer,
		ReadinessLookup:                  readyLookup(true, true),
	})

	found := false
	for _, def := range definitions {
		if def.Domain != DomainIAMInstanceProfileRoleMaterialization {
			continue
		}
		found = true
		handler, ok := def.Handler.(iaminstprofile.IAMInstanceProfileRoleMaterializationHandler)
		if !ok {
			t.Fatalf("handler type = %T, want iaminstprofile.IAMInstanceProfileRoleMaterializationHandler", def.Handler)
		}
		if handler.FactLoader != loader {
			t.Fatal("iam_instance_profile_role_materialization handler FactLoader was not wired")
		}
		if handler.EdgeWriter != writer {
			t.Fatal("iam_instance_profile_role_materialization handler EdgeWriter was not wired")
		}
		if handler.ReadinessLookup == nil {
			t.Fatal("iam_instance_profile_role_materialization handler ReadinessLookup was not wired")
		}
	}
	if !found {
		t.Fatal("iam_instance_profile_role_materialization not registered after wiring loader+edge writer")
	}
}
