// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"context"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// frozenCodeCypherReadAdmission pins the exact method set behind the
// versioned #9669 admission contract for the W6d 2 caller-program reads:
// POST /api/v0/code/cypher and POST /api/v0/code/visualize admit their
// bounded graph traversal through Run and nothing else. The entry
// re-declares the staying GraphQuery signature in this cypher-scoped
// shape instead of embedding the port, so no other family can cite this
// pin — embedding or aliasing a shared port here would be a blanket pin,
// which the arbiter ruling forbids. A renamed, added, removed, or
// re-signed method fails here until that list is deliberately updated;
// bump CodeCypherReadAdmissionContractVersion alongside (manually — the
// bump is convention, the frozen list is the enforcement) so dest
// consumers can pin the shape, keeping the eshu-api extraction boundary
// from drifting silently.
//
// W-wave consumer: W6d cypher/visualize ONLY (per the arbiter
// supplemental ruling on eshu-hq/eshu-api#98, §2D). Termination stays
// gated on the shared-key-only wave gate: shared-key (or all-scope)
// terminates, every scoped or foreign class relays, and scoped callers
// on these two routes are rejected outright — a caller-supplied program
// can never be grant-intersected.
var frozenCodeCypherReadAdmission = []string{
	"Run func(context.Context, string, map[string]interface {}) ([]map[string]interface {}, error)",
}

// codeCypherReadAdmissionWitness proves CodeCypherReadAdmission is
// satisfiable with the minimal shape: the one graph traversal, no wider
// store surface.
type codeCypherReadAdmissionWitness struct{}

func (codeCypherReadAdmissionWitness) Run(context.Context, string, map[string]any) ([]map[string]any, error) {
	return nil, nil
}

var _ CodeCypherReadAdmission = codeCypherReadAdmissionWitness{}

// codeCypherReadAdmissionResignedWitness carries a deliberately
// re-signed Run (an extra trailing parameter). It MUST NOT satisfy
// CodeCypherReadAdmission: the seeded rejection test below proves a
// signature drift on the pinned method breaks the pin instead of sliding
// through silently.
type codeCypherReadAdmissionResignedWitness struct{}

func (codeCypherReadAdmissionResignedWitness) Run(context.Context, string, map[string]any, int) ([]map[string]any, error) {
	return nil, nil
}

// codeCypherReadAdmissionMethodSet reflects the live
// CodeCypherReadAdmission method set into sorted "name signature"
// strings for comparison with the frozen list.
func codeCypherReadAdmissionMethodSet(t *testing.T) []string {
	t.Helper()
	rt := reflect.TypeOf((*CodeCypherReadAdmission)(nil)).Elem()
	var got []string
	for i := 0; i < rt.NumMethod(); i++ {
		m := rt.Method(i)
		got = append(got, m.Name+" "+m.Type.String())
	}
	sort.Strings(got)
	return got
}

// TestCodeCypherReadAdmissionContractIsVersioned pins the #9669
// admission-contract version for the W6d 2 caller-program reads. The
// frozen method set below is the enforcement: an added, removed,
// renamed, or re-signed method fails this test until the frozen list is
// deliberately updated. Bump CodeCypherReadAdmissionContractVersion
// alongside (manually — the bump is convention, the frozen list is the
// enforcement) so dest consumers can pin the shape.
func TestCodeCypherReadAdmissionContractIsVersioned(t *testing.T) {
	t.Parallel()

	if CodeCypherReadAdmissionContractVersion != "v0" {
		t.Errorf("CodeCypherReadAdmissionContractVersion = %q, want %q: bump it deliberately with the shape change", CodeCypherReadAdmissionContractVersion, "v0")
	}
	got := codeCypherReadAdmissionMethodSet(t)
	if len(got) != len(frozenCodeCypherReadAdmission) {
		t.Fatalf("CodeCypherReadAdmission method set = %q (len %d), want frozen %q (len %d): bump %s with the shape change", got, len(got), frozenCodeCypherReadAdmission, len(frozenCodeCypherReadAdmission), CodeCypherReadAdmissionContractVersion)
	}
	for i := range got {
		if got[i] != frozenCodeCypherReadAdmission[i] {
			t.Errorf("CodeCypherReadAdmission method set differs at index %d: got %q, want %q: bump %s with the shape change", i, got[i], frozenCodeCypherReadAdmission[i], CodeCypherReadAdmissionContractVersion)
		}
	}
}

// TestCodeCypherReadAdmissionRejectsResignedMethod is the seeded RED half
// of the contract proof: a witness whose Run carries one extra parameter
// MUST NOT satisfy the pin. If a re-signed method ever slides through,
// this test fails and the frozen shape above can no longer be trusted to
// catch drift.
func TestCodeCypherReadAdmissionRejectsResignedMethod(t *testing.T) {
	t.Parallel()

	pin := reflect.TypeOf((*CodeCypherReadAdmission)(nil)).Elem()
	if reflect.TypeOf(codeCypherReadAdmissionResignedWitness{}).Implements(pin) {
		t.Errorf("codeCypherReadAdmissionResignedWitness implements CodeCypherReadAdmission: a re-signed Run must break the pin")
	}
	if !reflect.TypeOf(codeCypherReadAdmissionWitness{}).Implements(pin) {
		t.Errorf("codeCypherReadAdmissionWitness does not implement CodeCypherReadAdmission: the minimal shape must satisfy the pin")
	}
}

// TestCodeCypherReadAdmissionWireShapesAreSealed proves the seal on the
// pin: exactly the one re-declared graph read, with RunSingle and every
// search, list, entity, file, catalog, topic, secret, or snapshot
// surface riding along refused. The frozen method-set contract test pins
// the exact name; this test pins the exclusions the arbiter ruling
// demands. The mutation-rejection, LIMIT-injection, and timeout behavior
// stay handler behavior OUT: this pin carries the read shape only.
func TestCodeCypherReadAdmissionWireShapesAreSealed(t *testing.T) {
	t.Parallel()

	rt := reflect.TypeOf((*CodeCypherReadAdmission)(nil)).Elem()
	if rt.NumMethod() != 1 {
		t.Fatalf("CodeCypherReadAdmission has %d methods, want exactly 1: the seal forbids any wider surface", rt.NumMethod())
	}
	for i := range rt.NumMethod() {
		name := rt.Method(i).Name
		for _, banned := range []string{
			"RunSingle", "Search", "List", "Get", "Inspect",
			"Investigate", "Count", "Topic", "Secret", "Snapshot",
		} {
			if strings.Contains(name, banned) {
				t.Errorf("CodeCypherReadAdmission.%s contains %q: that leg stays out of this pin", name, banned)
			}
		}
	}
}
