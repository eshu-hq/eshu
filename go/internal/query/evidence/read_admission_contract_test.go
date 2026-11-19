// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package evidence

import (
	"context"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract/packet"
	"github.com/eshu-hq/eshu/go/internal/status"
)

// frozenEvidenceReadAdmission pins the exact method set behind the
// versioned #9669 W5 admission contract for the evidence reads (GET
// /api/v0/evidence/admission-decisions, bundle,
// relationships/{resolved_id},
// investigations/deployable-unit/packet, POST /api/v0/evidence/citations):
// the relationship drilldown, the two decision reads, the two citation
// reads, and the two bundle reads. The entries re-declare the store
// signatures in this evidence-scoped shape instead of embedding the
// shared stores, so no other family can cite this pin — embedding any
// of the stores here would be a blanket pin, which the arbiter ruling
// forbids. A renamed, added, removed, or re-signed method fails here
// until that list is deliberately updated; bump
// EvidenceReadAdmissionContractVersion alongside (manually — the bump
// is convention, the frozen list is the enforcement) so dest consumers
// can pin the shape, keeping the eshu-api extraction boundary from
// drifting silently.
//
// W-wave consumer: W5 evidence (all 5 oracle Evidence rows).
var frozenEvidenceReadAdmission = []string{
	"EvidenceCitationFiles func(context.Context, []evidence.EvidenceCitationFileLookup) (map[evidence.EvidenceCitationFileKey]querycontract.FileContent, error)",
	"GetFileContent func(context.Context, string, string) (*querycontract.FileContent, error)",
	"ListAdmissionDecisionEvidence func(context.Context, string, int) ([]packet.AdmissionDecisionEvidenceRow, error)",
	"ListAdmissionDecisions func(context.Context, evidence.AdmissionDecisionReadFilter) ([]evidence.AdmissionDecisionReadRow, error)",
	"ReadStatusSnapshotFiltered func(context.Context, time.Time, status.SnapshotSelection) (status.RawSnapshot, error)",
	"RelationshipEvidenceByResolvedID func(context.Context, string) (repository.RelationshipEvidenceReadModel, error)",
	"RunSingle func(context.Context, string, map[string]interface {}) (map[string]interface {}, error)",
}

// evidenceReadAdmissionWitness proves EvidenceReadAdmission is
// satisfiable with the minimal shape: one drilldown, two decision
// reads, two citation reads, and two bundle reads, no wider store
// surface.
type evidenceReadAdmissionWitness struct{}

func (evidenceReadAdmissionWitness) RelationshipEvidenceByResolvedID(context.Context, string) (RelationshipEvidenceReadModel, error) {
	return RelationshipEvidenceReadModel{}, nil
}

func (evidenceReadAdmissionWitness) ListAdmissionDecisions(context.Context, AdmissionDecisionReadFilter) ([]AdmissionDecisionReadRow, error) {
	return nil, nil
}

func (evidenceReadAdmissionWitness) ListAdmissionDecisionEvidence(context.Context, string, int) ([]packet.AdmissionDecisionEvidenceRow, error) {
	return nil, nil
}

func (evidenceReadAdmissionWitness) EvidenceCitationFiles(context.Context, []EvidenceCitationFileLookup) (map[EvidenceCitationFileKey]querycontract.FileContent, error) {
	return nil, nil
}

func (evidenceReadAdmissionWitness) GetFileContent(context.Context, string, string) (*querycontract.FileContent, error) {
	return nil, nil
}

func (evidenceReadAdmissionWitness) ReadStatusSnapshotFiltered(context.Context, time.Time, status.SnapshotSelection) (status.RawSnapshot, error) {
	return status.RawSnapshot{}, nil
}

func (evidenceReadAdmissionWitness) RunSingle(context.Context, string, map[string]any) (map[string]any, error) {
	return nil, nil
}

var _ EvidenceReadAdmission = evidenceReadAdmissionWitness{}

// evidenceReadAdmissionMethodSet reflects the live
// EvidenceReadAdmission method set into sorted "name signature"
// strings for comparison with the frozen list. The iface parameter
// forces every caller — including the seeded-drift RED proof below —
// through this same guard comparison, so a passing suite proves the
// guard itself can see drift rather than proving a private copy can.
func evidenceReadAdmissionMethodSet(t *testing.T, iface any) []string {
	t.Helper()
	rt := reflect.TypeOf(iface).Elem()
	var got []string
	for i := 0; i < rt.NumMethod(); i++ {
		m := rt.Method(i)
		got = append(got, m.Name+" "+m.Type.String())
	}
	sort.Strings(got)
	return got
}

// TestEvidenceReadAdmissionContractIsVersioned pins the #9669 W5
// admission-contract version for the evidence reads. The frozen method
// set below is the enforcement: an added, removed, renamed, or
// re-signed method fails this test until the frozen list is
// deliberately updated. Bump EvidenceReadAdmissionContractVersion
// alongside (manually — the bump is convention, the frozen list is
// the enforcement) so dest consumers can pin the shape.
func TestEvidenceReadAdmissionContractIsVersioned(t *testing.T) {
	t.Parallel()

	if EvidenceReadAdmissionContractVersion != "v0" {
		t.Errorf("EvidenceReadAdmissionContractVersion = %q, want %q: bump it deliberately with the shape change", EvidenceReadAdmissionContractVersion, "v0")
	}
	got := evidenceReadAdmissionMethodSet(t, (*EvidenceReadAdmission)(nil))
	if len(got) != len(frozenEvidenceReadAdmission) {
		t.Fatalf("EvidenceReadAdmission method set = %q (len %d), want frozen %q (len %d): bump %s with the shape change", got, len(got), frozenEvidenceReadAdmission, len(frozenEvidenceReadAdmission), EvidenceReadAdmissionContractVersion)
	}
	for i := range got {
		if got[i] != frozenEvidenceReadAdmission[i] {
			t.Errorf("EvidenceReadAdmission method set differs at index %d: got %q, want %q: bump %s with the shape change", i, got[i], frozenEvidenceReadAdmission[i], EvidenceReadAdmissionContractVersion)
		}
	}
}

// TestEvidenceReadAdmissionWireShapesAreSealed proves the seal on the
// pin: exactly the seven re-declared evidence reads, with no packet
// composition, mutation, snapshot, or ledger surface riding along.
// The frozen method-set contract test pins the exact names; this test
// pins the exclusion the arbiter ruling demands.
func TestEvidenceReadAdmissionWireShapesAreSealed(t *testing.T) {
	t.Parallel()

	rt := reflect.TypeOf((*EvidenceReadAdmission)(nil)).Elem()
	if rt.NumMethod() != 7 {
		t.Fatalf("EvidenceReadAdmission has %d methods, want exactly 7: the seal forbids any wider surface", rt.NumMethod())
	}
	for i := range rt.NumMethod() {
		name := rt.Method(i).Name
		for _, banned := range []string{
			"Compose", "Respond", "Delete", "Create", "Write", "Purge", "Ledger", "Archive",
		} {
			if strings.Contains(name, banned) {
				t.Errorf("EvidenceReadAdmission.%s contains %q: that leg stays out of this pin", name, banned)
			}
		}
	}
}

// seededEvidenceDrift is a deliberately widened copy of the pin: the
// seven frozen evidence reads plus a packet-composition write the
// evidence lane never issues. It never ships; it proves the
// frozen-set comparison below can see a one-method widening.
type seededEvidenceDrift interface {
	EvidenceReadAdmission
	ComposeEvidencePacket(context.Context, string) error
}

// TestEvidenceReadAdmissionSeededDriftIsDetected is the RED half of
// the seeded-drift pair: the same reflect comparison the contract
// test runs must report a mismatch on the planted widening. It passes
// on a clean tree precisely because the guard fires on drift — a
// guard that cannot see the seeded method is no guard.
func TestEvidenceReadAdmissionSeededDriftIsDetected(t *testing.T) {
	t.Parallel()

	got := evidenceReadAdmissionMethodSet(t, (*seededEvidenceDrift)(nil))
	if reflect.DeepEqual(got, frozenEvidenceReadAdmission) {
		t.Fatal("seeded widening (ComposeEvidencePacket) matches the frozen set: the comparison cannot see drift")
	}
	if len(got) != len(frozenEvidenceReadAdmission)+1 {
		t.Fatalf("seeded drift method set len = %d, want frozen len + 1 (%d): the seed is malformed", len(got), len(frozenEvidenceReadAdmission)+1)
	}
}
