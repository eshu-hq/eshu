// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer/ec2blockkms"
)

// stubEC2BlockDeviceKMSPostureNodeWriter is a minimal root-side fake satisfying
// ec2blockkms.EC2BlockDeviceKMSPostureNodeWriter. This wiring test only proves
// the writer identity was threaded through to the handler, never exercising its
// methods, so it does not need the family's recording behavior -- and Go test
// files cannot share an unexported test type (recordingEC2BlockDeviceKMSPostureNodeWriter)
// across the reducer/ec2blockkms package boundary (issue #6061).
// The id field is load-bearing: a zero-size struct compares equal to every
// other instance of itself, so an identity assertion over a struct{} value
// passes even when a DIFFERENT writer reaches the handler. A pointer to an
// empty struct is not sufficient either -- Go permits distinct zero-size
// allocations to share an address -- so the field is what makes the
// comparison sound rather than lucky.
type stubEC2BlockDeviceKMSPostureNodeWriter struct{ id int }

func (stubEC2BlockDeviceKMSPostureNodeWriter) WriteEC2BlockDeviceKMSPostureNodes(context.Context, []map[string]any, string, string, string) error {
	return nil
}

func (stubEC2BlockDeviceKMSPostureNodeWriter) RetractEC2BlockDeviceKMSPostureNodes(context.Context, []string, string, string) error {
	return nil
}

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
	writer := &stubEC2BlockDeviceKMSPostureNodeWriter{id: 1}
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
		handler, ok := def.Handler.(ec2blockkms.EC2BlockDeviceKMSPostureMaterializationHandler)
		if !ok {
			t.Fatalf("ec2_block_device_kms_posture_materialization handler type = %T, want ec2blockkms.EC2BlockDeviceKMSPostureMaterializationHandler", def.Handler)
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
