// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import "testing"

func TestImplementedDefaultDomainDefinitionsOmitsEC2InternetExposureWithoutNodeWriter(t *testing.T) {
	t.Parallel()

	definitions := implementedDefaultDomainDefinitions(DefaultHandlers{
		FactLoader: &stubFactLoader{},
	})
	for _, def := range definitions {
		if def.Domain == DomainEC2InternetExposureMaterialization {
			t.Fatalf("ec2_internet_exposure_materialization registered without node writer; want omitted to avoid silent intent drops")
		}
	}
}

func TestImplementedDefaultDomainDefinitionsIncludesEC2InternetExposureWhenWired(t *testing.T) {
	t.Parallel()

	loader := &stubFactLoader{}
	writer := &recordingEC2InternetExposureNodeWriter{}
	definitions := implementedDefaultDomainDefinitions(DefaultHandlers{
		FactLoader:                    loader,
		EC2InternetExposureNodeWriter: writer,
		ReadinessLookup:               readyLookup(true, true),
	})
	found := false
	for _, def := range definitions {
		if def.Domain != DomainEC2InternetExposureMaterialization {
			continue
		}
		found = true
		handler, ok := def.Handler.(EC2InternetExposureMaterializationHandler)
		if !ok {
			t.Fatalf("ec2_internet_exposure_materialization handler type = %T, want EC2InternetExposureMaterializationHandler", def.Handler)
		}
		if handler.FactLoader != loader {
			t.Fatal("ec2_internet_exposure_materialization handler FactLoader was not wired")
		}
		if handler.NodeWriter != writer {
			t.Fatal("ec2_internet_exposure_materialization handler NodeWriter was not wired")
		}
		if handler.ReadinessLookup == nil {
			t.Fatal("ec2_internet_exposure_materialization handler ReadinessLookup was not wired")
		}
		if !def.Ownership.CanonicalWrite {
			t.Fatal("ec2_internet_exposure_materialization must declare CanonicalWrite ownership")
		}
	}
	if !found {
		t.Fatal("ec2_internet_exposure_materialization not registered after wiring loader+node writer")
	}
}
