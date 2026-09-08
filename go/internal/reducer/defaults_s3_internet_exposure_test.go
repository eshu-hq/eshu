// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer/internetexposure"
)

// stubS3InternetExposureNodeWriter is a minimal root-side fake satisfying
// internetexposure.S3InternetExposureNodeWriter. This wiring test only proves
// the writer identity was threaded through to the handler, never exercising
// its methods, so it does not need the family's recording behavior -- and Go
// test files cannot share an unexported test type
// (recordingS3InternetExposureNodeWriter) across the
// reducer/internetexposure package boundary (issue #6061).
// The id field is load-bearing: a zero-size struct compares equal to every
// other instance of itself, so an identity assertion over a struct{} value
// passes even when a DIFFERENT writer reaches the handler. A pointer to an
// empty struct is not sufficient either -- Go permits distinct zero-size
// allocations to share an address -- so the field is what makes the
// comparison sound rather than lucky.
type stubS3InternetExposureNodeWriter struct{ id int }

func (stubS3InternetExposureNodeWriter) WriteS3InternetExposureNodes(context.Context, []map[string]any, string, string, string) error {
	return nil
}

func (stubS3InternetExposureNodeWriter) RetractS3InternetExposureNodes(context.Context, []string, string, string) error {
	return nil
}

func TestImplementedDefaultDomainDefinitionsOmitsS3InternetExposureWithoutNodeWriter(t *testing.T) {
	t.Parallel()

	definitions := implementedDefaultDomainDefinitions(DefaultHandlers{
		FactLoader: &stubFactLoader{},
	})
	for _, def := range definitions {
		if def.Domain == DomainS3InternetExposureMaterialization {
			t.Fatalf("s3_internet_exposure_materialization registered without node writer; want omitted to avoid silent intent drops")
		}
	}
}

func TestImplementedDefaultDomainDefinitionsIncludesS3InternetExposureWhenWired(t *testing.T) {
	t.Parallel()

	loader := &stubFactLoader{}
	writer := &stubS3InternetExposureNodeWriter{id: 1}
	definitions := implementedDefaultDomainDefinitions(DefaultHandlers{
		FactLoader:                   loader,
		S3InternetExposureNodeWriter: writer,
		ReadinessLookup:              readyLookup(true, true),
	})
	found := false
	for _, def := range definitions {
		if def.Domain != DomainS3InternetExposureMaterialization {
			continue
		}
		found = true
		handler, ok := def.Handler.(internetexposure.S3InternetExposureMaterializationHandler)
		if !ok {
			t.Fatalf("s3_internet_exposure_materialization handler type = %T, want internetexposure.S3InternetExposureMaterializationHandler", def.Handler)
		}
		if handler.FactLoader != loader {
			t.Fatal("s3_internet_exposure_materialization handler FactLoader was not wired")
		}
		if handler.NodeWriter != writer {
			t.Fatal("s3_internet_exposure_materialization handler NodeWriter was not wired")
		}
		if handler.ReadinessLookup == nil {
			t.Fatal("s3_internet_exposure_materialization handler ReadinessLookup was not wired")
		}
		if !def.Ownership.CanonicalWrite {
			t.Fatal("s3_internet_exposure_materialization must declare CanonicalWrite ownership")
		}
	}
	if !found {
		t.Fatal("s3_internet_exposure_materialization not registered after wiring loader+node writer")
	}
}
