// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import "testing"

func TestImplementedDefaultDomainDefinitionsOmitsEC2BlockDeviceKMSPostureWithoutNodeWriter(t *testing.T) {
	t.Parallel()

	definitions := implementedDefaultDomainDefinitions(DefaultHandlers{
		FactLoader: &stubFactLoader{},
	})
	for _, def := range definitions {
		if def.Domain == DomainEC2BlockDeviceKMSPostureMaterialization {
			t.Fatalf("ec2_block_device_kms_posture_materialization registered without node writer; want omitted to avoid silent intent drops")
		}
	}
}

func TestImplementedDefaultDomainDefinitionsIncludesEC2BlockDeviceKMSPostureWhenWired(t *testing.T) {
	t.Parallel()

	loader := &stubFactLoader{}
	writer := &recordingEC2BlockDeviceKMSPostureNodeWriter{}
	definitions := implementedDefaultDomainDefinitions(DefaultHandlers{
		FactLoader:                         loader,
		EC2BlockDeviceKMSPostureNodeWriter: writer,
		ReadinessLookup:                    readyLookup(true, true),
	})
	found := false
	for _, def := range definitions {
		if def.Domain != DomainEC2BlockDeviceKMSPostureMaterialization {
			continue
		}
		found = true
		handler, ok := def.Handler.(EC2BlockDeviceKMSPostureMaterializationHandler)
		if !ok {
			t.Fatalf("ec2_block_device_kms_posture_materialization handler type = %T, want EC2BlockDeviceKMSPostureMaterializationHandler", def.Handler)
		}
		if handler.FactLoader != loader {
			t.Fatal("ec2_block_device_kms_posture_materialization handler FactLoader was not wired")
		}
		if handler.NodeWriter != writer {
			t.Fatal("ec2_block_device_kms_posture_materialization handler NodeWriter was not wired")
		}
		if handler.ReadinessLookup == nil {
			t.Fatal("ec2_block_device_kms_posture_materialization handler ReadinessLookup was not wired")
		}
		if !def.Ownership.CanonicalWrite {
			t.Fatal("ec2_block_device_kms_posture_materialization must declare CanonicalWrite ownership")
		}
	}
	if !found {
		t.Fatal("ec2_block_device_kms_posture_materialization not registered after wiring loader+node writer")
	}
}
