// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer/ec2instance"
)

// stubEC2InstanceIdentityNodeWriter is a minimal root-side fake satisfying
// ec2instance.EC2InstanceIdentityNodeWriter. This wiring test only proves the
// writer identity was threaded through to the handler, never exercising its
// methods, so it does not need the family's recording behavior -- and Go
// test files cannot share an unexported test type
// (recordingEC2InstanceIdentityNodeWriter) across the reducer/ec2instance
// package boundary (issue #6061).
// The id field is load-bearing: a zero-size struct compares equal to every
// other instance of itself, so an identity assertion over a struct{} value
// passes even when a DIFFERENT writer reaches the handler. A pointer to an
// empty struct is not sufficient either -- Go permits distinct zero-size
// allocations to share an address -- so the field is what makes the
// comparison sound rather than lucky.
type stubEC2InstanceIdentityNodeWriter struct{ id int }

func (stubEC2InstanceIdentityNodeWriter) WriteEC2InstanceIdentityNodes(context.Context, []map[string]any, string, string, string) error {
	return nil
}

func (stubEC2InstanceIdentityNodeWriter) RetractEC2InstanceIdentityNodes(context.Context, []string, string, string) error {
	return nil
}

func TestImplementedDefaultDomainDefinitionsOmitsEC2InstanceIdentityWithoutNodeWriter(t *testing.T) {
	t.Parallel()

	definitions := implementedDefaultDomainDefinitions(DefaultHandlers{
		FactLoader: &stubFactLoader{},
	})
	for _, def := range definitions {
		if def.Domain == DomainEC2InstanceIdentityMaterialization {
			t.Fatalf("ec2_instance_identity_materialization registered without node writer; want omitted to avoid silent intent drops")
		}
	}
}

func TestImplementedDefaultDomainDefinitionsIncludesEC2InstanceIdentityWhenWired(t *testing.T) {
	t.Parallel()

	loader := &stubFactLoader{}
	writer := &stubEC2InstanceIdentityNodeWriter{id: 1}
	definitions := implementedDefaultDomainDefinitions(DefaultHandlers{
		FactLoader:                    loader,
		EC2InstanceIdentityNodeWriter: writer,
		ReadinessLookup:               readyLookup(true, true),
	})
	found := false
	for _, def := range definitions {
		if def.Domain != DomainEC2InstanceIdentityMaterialization {
			continue
		}
		found = true
		handler, ok := def.Handler.(ec2instance.EC2InstanceIdentityMaterializationHandler)
		if !ok {
			t.Fatalf("ec2_instance_identity_materialization handler type = %T, want ec2instance.EC2InstanceIdentityMaterializationHandler", def.Handler)
		}
		if handler.FactLoader != loader {
			t.Fatal("ec2_instance_identity_materialization handler FactLoader was not wired")
		}
		if handler.NodeWriter != writer {
			t.Fatal("ec2_instance_identity_materialization handler NodeWriter was not wired")
		}
		if handler.ReadinessLookup == nil {
			t.Fatal("ec2_instance_identity_materialization handler ReadinessLookup was not wired")
		}
		if !def.Ownership.CanonicalWrite {
			t.Fatal("ec2_instance_identity_materialization must declare CanonicalWrite ownership")
		}
	}
	if !found {
		t.Fatal("ec2_instance_identity_materialization not registered after wiring loader+node writer")
	}
}
