// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import "testing"

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
	writer := &recordingEC2InstanceIdentityNodeWriter{}
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
		handler, ok := def.Handler.(EC2InstanceIdentityMaterializationHandler)
		if !ok {
			t.Fatalf("ec2_instance_identity_materialization handler type = %T, want EC2InstanceIdentityMaterializationHandler", def.Handler)
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
