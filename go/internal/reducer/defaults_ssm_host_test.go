// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts/cloud"
	"github.com/eshu-hq/eshu/go/internal/reducer/ssmhost"
)

// stubSSMHostGraphWriter is a minimal root-side fake satisfying
// ssmhost.HostGraphWriter. This wiring test only proves the writer and bounds
// identities were threaded through to the handler, never exercising their
// methods. The id field is load-bearing: a zero-size struct compares equal
// to every other instance of itself, so the field is what makes the identity
// comparison sound rather than lucky.
type stubSSMHostGraphWriter struct{ id int }

func (stubSSMHostGraphWriter) ReadCommittedHosts(context.Context, string, []cloud.SSMHostTuple) (map[string]ssmhost.CommittedHost, error) {
	return map[string]ssmhost.CommittedHost{}, nil
}

func (stubSSMHostGraphWriter) WriteHostNodes(context.Context, ssmhost.HostGraphWrite) error {
	return nil
}

func (stubSSMHostGraphWriter) RetractStaleHostNodes(context.Context, ssmhost.HostGraphRetract) (int, error) {
	return 0, nil
}

func ssmTestBounds() ssmhost.Bounds {
	return ssmhost.Bounds{
		MaxCaptureAge:             2 * time.Hour,
		MaxCrossTypeSkew:          10 * time.Minute,
		MaxPlatformObservationAge: 24 * time.Hour,
	}
}

func TestImplementedDefaultDomainDefinitionsOmitsSSMHostWithoutWriter(t *testing.T) {
	t.Parallel()

	definitions := implementedDefaultDomainDefinitions(DefaultHandlers{
		FactLoader:    &stubFactLoader{},
		SSMHostBounds: ssmTestBounds(),
	})
	for _, def := range definitions {
		if def.Domain == DomainSSMHostInventoryMaterialization {
			t.Fatalf("ssm_host_inventory_materialization registered without graph writer; want omitted to avoid silent intent drops")
		}
	}
}

func TestImplementedDefaultDomainDefinitionsOmitsSSMHostWithoutBounds(t *testing.T) {
	t.Parallel()

	definitions := implementedDefaultDomainDefinitions(DefaultHandlers{
		FactLoader:         &stubFactLoader{},
		SSMHostGraphWriter: &stubSSMHostGraphWriter{id: 1},
	})
	for _, def := range definitions {
		if def.Domain == DomainSSMHostInventoryMaterialization {
			t.Fatalf("ssm_host_inventory_materialization registered without freshness bounds; want omitted to avoid unbounded supersession")
		}
	}
}

func TestImplementedDefaultDomainDefinitionsIncludesSSMHostWhenWired(t *testing.T) {
	t.Parallel()

	loader := &stubFactLoader{}
	writer := &stubSSMHostGraphWriter{id: 1}
	definitions := implementedDefaultDomainDefinitions(DefaultHandlers{
		FactLoader:         loader,
		SSMHostGraphWriter: writer,
		SSMHostBounds:      ssmTestBounds(),
	})
	found := false
	for _, def := range definitions {
		if def.Domain != DomainSSMHostInventoryMaterialization {
			continue
		}
		found = true
		handler, ok := def.Handler.(ssmhost.MaterializationHandler)
		if !ok {
			t.Fatalf("ssm_host_inventory_materialization handler type = %T, want ssmhost.MaterializationHandler", def.Handler)
		}
		if handler.GraphWriter != writer {
			t.Fatal("ssm_host_inventory_materialization handler GraphWriter was not wired")
		}
		if err := handler.Bounds.Validate(); err != nil {
			t.Fatalf("ssm_host_inventory_materialization handler Bounds invalid: %v", err)
		}
	}
	if !found {
		t.Fatal("ssm_host_inventory_materialization not registered when fully wired")
	}
}
