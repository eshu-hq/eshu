// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"errors"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/component"
)

// errComponentExtensionWireShapeProbe is the sentinel the failing canned
// store below answers the Readback with. A non-nil error (never a panic,
// never a confident empty result) is the fail-closed proof the satellite
// consumer needs: an unavailable registry must surface as an error the
// handler maps to 503, not as an empty inventory the satellite could mistake
// for "no extensions installed".
var errComponentExtensionWireShapeProbe = errors.New("component extension wire-shape probe: registry unavailable")

// componentExtensionAdmissionCannedStore is the pin-satisfying double behind
// the wire-shape proof: it implements exactly the one pinned read over fixed
// canned values and is compile-asserted against the pin, so the proof below
// runs through the admission shape, not the wider Registry surface. Both
// routes share this read — the inventory route projects every row and the
// diagnostics route selects one row by ID — so serving the pinned Readback is
// sufficient for the family.
type componentExtensionAdmissionCannedStore struct {
	rows []component.RegistryReadbackComponent
	err  error
}

var _ ComponentExtensionReadAdmission = (*componentExtensionAdmissionCannedStore)(nil)

func (s *componentExtensionAdmissionCannedStore) Readback(component.Policy) ([]component.RegistryReadbackComponent, error) {
	if s.err != nil {
		return nil, s.err
	}
	return append([]component.RegistryReadbackComponent(nil), s.rows...), nil
}

// TestComponentExtensionReadAdmissionWireShapesServeTheRoute proves the
// frozen shape is sufficient: the pinned Readback answers through the
// pin-typed canned store with the rows the inventory projection consumes,
// and fails closed (never a confident empty result with a nil error) when
// the registry is unavailable.
func TestComponentExtensionReadAdmissionWireShapesServeTheRoute(t *testing.T) {
	t.Parallel()

	var pin ComponentExtensionReadAdmission = &componentExtensionAdmissionCannedStore{
		rows: []component.RegistryReadbackComponent{{States: []string{component.RegistryStateInstalled}}},
	}
	rows, err := pin.Readback(component.Policy{})
	if err != nil {
		t.Fatalf("Readback through the pin failed: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("Readback through the pin returned %d rows, want 1", len(rows))
	}

	var failing ComponentExtensionReadAdmission = &componentExtensionAdmissionCannedStore{
		err: errComponentExtensionWireShapeProbe,
	}
	failed, err := failing.Readback(component.Policy{})
	if err == nil {
		t.Error("Readback over an unavailable registry = nil error, want a fail-closed error")
	}
	if len(failed) != 0 {
		t.Errorf("Readback over an unavailable registry returned %d rows, want none alongside the error", len(failed))
	}
}

// TestComponentExtensionReadAdmissionMirrorStaysHonest proves the production
// registry behind the pin answers the same read the handler runs today: the
// component.Registry value the pin mirrors satisfies both the
// component-extension-scoped pin and the handler's direct call shape
// (component.NewRegistry(home).Readback(policy)), so satellite consumption
// moves no read off the shape the routes already admit through.
func TestComponentExtensionReadAdmissionMirrorStaysHonest(t *testing.T) {
	t.Parallel()

	var _ ComponentExtensionReadAdmission = component.NewRegistry(t.TempDir())
}
