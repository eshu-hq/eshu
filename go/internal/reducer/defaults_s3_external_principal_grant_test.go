// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import "testing"

func TestImplementedDefaultDomainDefinitionsOmitsS3ExternalPrincipalGrantWithoutWriter(t *testing.T) {
	t.Parallel()

	definitions := implementedDefaultDomainDefinitions(DefaultHandlers{
		FactLoader: &stubFactLoader{},
	})
	for _, def := range definitions {
		if def.Domain == DomainS3ExternalPrincipalGrantMaterialization {
			t.Fatalf("s3_external_principal_grant_materialization registered without writer; want omitted to avoid silent intent drops")
		}
	}
}

func TestImplementedDefaultDomainDefinitionsIncludesS3ExternalPrincipalGrantWhenWired(t *testing.T) {
	t.Parallel()

	loader := &stubFactLoader{}
	writer := &recordingS3ExternalPrincipalGrantWriter{}
	definitions := implementedDefaultDomainDefinitions(DefaultHandlers{
		FactLoader:                     loader,
		S3ExternalPrincipalGrantWriter: writer,
		ReadinessLookup:                readyLookup(true, true),
	})
	found := false
	for _, def := range definitions {
		if def.Domain != DomainS3ExternalPrincipalGrantMaterialization {
			continue
		}
		found = true
		handler, ok := def.Handler.(S3ExternalPrincipalGrantMaterializationHandler)
		if !ok {
			t.Fatalf("s3_external_principal_grant_materialization handler type = %T, want S3ExternalPrincipalGrantMaterializationHandler", def.Handler)
		}
		if handler.FactLoader != loader {
			t.Fatal("s3_external_principal_grant_materialization handler FactLoader was not wired")
		}
		if handler.GrantWriter != writer {
			t.Fatal("s3_external_principal_grant_materialization handler GrantWriter was not wired")
		}
		if handler.ReadinessLookup == nil {
			t.Fatal("s3_external_principal_grant_materialization handler ReadinessLookup was not wired")
		}
		if !def.Ownership.CanonicalWrite {
			t.Fatal("s3_external_principal_grant_materialization must declare CanonicalWrite ownership")
		}
	}
	if !found {
		t.Fatal("s3_external_principal_grant_materialization not registered after wiring loader+writer")
	}
}
