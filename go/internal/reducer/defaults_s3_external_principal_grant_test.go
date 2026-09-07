// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer/s3grant"
)

// stubS3ExternalPrincipalGrantWriter is a minimal root-side fake satisfying
// s3grant.S3ExternalPrincipalGrantWriter. This wiring test only proves the writer
// identity was threaded through to the handler, never exercising its
// methods, so it does not need the family's recording behavior -- and Go
// test files cannot share an unexported test type
// (recordingS3ExternalPrincipalGrantWriter) across the reducer/s3grant package
// boundary (issue #6061).
// The id field is load-bearing: a zero-size struct compares equal to every
// other instance of itself, so an identity assertion over a struct{} value
// passes even when a DIFFERENT writer reaches the handler. A pointer to an
// empty struct is not sufficient either -- Go permits distinct zero-size
// allocations to share an address -- so the field is what makes the
// comparison sound rather than lucky.
type stubS3ExternalPrincipalGrantWriter struct{ id int }

func (stubS3ExternalPrincipalGrantWriter) WriteS3ExternalPrincipalGrants(context.Context, []map[string]any, string, string, string) error {
	return nil
}

func (stubS3ExternalPrincipalGrantWriter) RetractS3ExternalPrincipalGrants(context.Context, []string, string, string) error {
	return nil
}

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
	writer := &stubS3ExternalPrincipalGrantWriter{id: 1}
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
		handler, ok := def.Handler.(s3grant.S3ExternalPrincipalGrantMaterializationHandler)
		if !ok {
			t.Fatalf("s3_external_principal_grant_materialization handler type = %T, want s3grant.S3ExternalPrincipalGrantMaterializationHandler", def.Handler)
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
