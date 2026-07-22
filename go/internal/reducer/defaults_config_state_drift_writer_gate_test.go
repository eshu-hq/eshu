// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"log/slog"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/relationships/tfstatebackend"
)

// This file holds the DomainConfigStateDrift durable-write registration gate
// tests added by issue #5442. Split out of defaults_test.go (already over the
// repository's 500-line file cap before this change) to avoid growing that
// file further.

func TestImplementedDefaultDomainDefinitionsIncludesConfigStateDriftWhenAdaptersPresent(t *testing.T) {
	t.Parallel()

	resolver := tfstatebackend.NewResolver(nil)
	loader := stubDriftEvidenceLoader{}
	logger := slog.New(slog.DiscardHandler)
	definitions := implementedDefaultDomainDefinitions(DefaultHandlers{
		DriftHandlers: DriftHandlers{
			TerraformBackendResolver: resolver,
			DriftEvidenceLoader:      loader,
			DriftWriter:              &stubDriftWriter{},
			DriftLogger:              logger,
		},
	})
	found := false
	for _, def := range definitions {
		if def.Domain == DomainConfigStateDrift {
			found = true
			if _, ok := def.Handler.(TerraformConfigStateDriftHandler); !ok {
				t.Fatalf("config_state_drift handler type = %T, want TerraformConfigStateDriftHandler", def.Handler)
			}
		}
	}
	if !found {
		t.Fatal("config_state_drift not registered after wiring resolver+loader+writer+logger")
	}
}

// TestImplementedDefaultDomainDefinitionsOmitsConfigStateDriftWithoutWriter
// proves the durable-write gate added by issue #5442: a resolver+loader+
// logger present but no DriftWriter must still omit registration, matching
// the "no consumer-less kind" bar the AWS/multi-cloud runtime drift adapters
// already hold. Without this gate the reducer could admit findings with no
// durable truth surface again, silently regressing this issue's fix.
func TestImplementedDefaultDomainDefinitionsOmitsConfigStateDriftWithoutWriter(t *testing.T) {
	t.Parallel()

	resolver := tfstatebackend.NewResolver(nil)
	loader := stubDriftEvidenceLoader{}
	logger := slog.New(slog.DiscardHandler)
	definitions := implementedDefaultDomainDefinitions(DefaultHandlers{
		DriftHandlers: DriftHandlers{
			TerraformBackendResolver: resolver,
			DriftEvidenceLoader:      loader,
			DriftLogger:              logger,
		},
	})
	for _, def := range definitions {
		if def.Domain == DomainConfigStateDrift {
			t.Fatal("config_state_drift registered without a DriftWriter; want omitted to avoid admitting findings with no durable truth surface")
		}
	}
}
