// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer/internetexposure"
)

// stubEC2InternetExposureNodeWriter is a minimal root-side fake satisfying
// internetexposure.EC2InternetExposureNodeWriter. This wiring test only proves
// the writer identity was threaded through to the handler, never exercising
// its methods, so it does not need the family's recording behavior -- and Go
// test files cannot share an unexported test type
// (recordingEC2InternetExposureNodeWriter) across the
// reducer/internetexposure package boundary (issue #6061).
// The id field is load-bearing: a zero-size struct compares equal to every
// other instance of itself, so an identity assertion over a struct{} value
// passes even when a DIFFERENT writer reaches the handler. A pointer to an
// empty struct is not sufficient either -- Go permits distinct zero-size
// allocations to share an address -- so the field is what makes the
// comparison sound rather than lucky.
type stubEC2InternetExposureNodeWriter struct{ id int }

func (stubEC2InternetExposureNodeWriter) WriteEC2InternetExposureNodes(context.Context, []map[string]any, string, string, string) error {
	return nil
}

func (stubEC2InternetExposureNodeWriter) RetractEC2InternetExposureNodes(context.Context, []string, string, string) error {
	return nil
}

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
	writer := &stubEC2InternetExposureNodeWriter{id: 1}
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
		handler, ok := def.Handler.(internetexposure.EC2InternetExposureMaterializationHandler)
		if !ok {
			t.Fatalf("ec2_internet_exposure_materialization handler type = %T, want internetexposure.EC2InternetExposureMaterializationHandler", def.Handler)
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
