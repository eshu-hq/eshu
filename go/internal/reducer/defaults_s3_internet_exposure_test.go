// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import "testing"

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
	writer := &recordingS3InternetExposureNodeWriter{}
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
		handler, ok := def.Handler.(S3InternetExposureMaterializationHandler)
		if !ok {
			t.Fatalf("s3_internet_exposure_materialization handler type = %T, want S3InternetExposureMaterializationHandler", def.Handler)
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
