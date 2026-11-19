// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"context"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/codequery"
	"github.com/eshu-hq/eshu/go/internal/query/impact/deployment"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// frozenImpactReadAdmission pins the exact method set behind the
// versioned #9669 W5 admission contract for the impact family (the 13
// W5 impact routes, per the supplemental arbiter ruling on
// eshu-hq/eshu-api#97). The two graph reads (Run, RunSingle), the
// eight ContentStore reads the family closure proves exercised
// (direct handler reads plus the service-not-found reference leg),
// the code-topic investigation the change-surface root adapter runs,
// the two live pod-template reads, and the ECS task read. The entries
// re-declare the port signatures in this impact-scoped shape instead
// of embedding the shared or family ports, so no other family can cite
// this pin — a blanket or shared-port pin is forbidden permanently. A
// renamed, added, removed, or re-signed method fails here until that
// list is deliberately updated; bump ImpactReadAdmissionContractVersion
// alongside (manually — the bump is convention, the frozen list is the
// enforcement) so dest consumers can pin the shape, keeping the
// eshu-api extraction boundary from drifting silently.
//
// W-wave consumer: W5c-e impact legs (the 13 W5 impact POSTs), with an
// explicit dual-pin cite for the deployment-trace enrichment leg:
// trace-deployment-chain and deployment-config-influence require
// EntityReadAdmission (extended Mint A) alongside this pin for their
// TraceContext leg, which pulls the F-A service-story closure through
// the root adapter. The satellite records this pin in a follow-up; a
// satellite PR consuming an unminted pin is invalid. Arbiter review of
// this pin precedes satellite termination of the impact legs.
var frozenImpactReadAdmission = []string{
	"GetEntityContent func(context.Context, string) (*querycontract.EntityContent, error)",
	"HasLiveIdentityMatch func(context.Context, deployment.KubernetesPodTemplateFilter) (bool, error)",
	"InvestigateCodeTopic func(context.Context, codequery.CodeTopicInvestigationRequest) ([]codequery.CodeTopicEvidenceRow, error)",
	"ListLiveIdentityMatches func(context.Context, deployment.KubernetesPodTemplateFilter) ([]deployment.LiveIdentityMatch, error)",
	"ListRepoEntities func(context.Context, string, int) ([]querycontract.EntityContent, error)",
	"ListRepoEntitiesByIDs func(context.Context, string, []string, int) ([]querycontract.EntityContent, error)",
	"ListRepoEntitiesByPaths func(context.Context, string, []string, int) ([]querycontract.EntityContent, error)",
	"ListRepoEntitiesByType func(context.Context, string, string, int) ([]querycontract.EntityContent, error)",
	"ListRepoK8sSelectCandidates func(context.Context, string, int) ([]querycontract.K8sSelectCandidate, error)",
	"ListRunningTasks func(context.Context, string, querycontract.RepositoryAccessFilter) (*deployment.ECSServiceTaskEvidence, error)",
	"Run func(context.Context, string, map[string]interface {}) ([]map[string]interface {}, error)",
	"RunSingle func(context.Context, string, map[string]interface {}) (map[string]interface {}, error)",
	"SearchEntitiesByExactNameAnyRepo func(context.Context, string, string, int) ([]querycontract.EntityContent, error)",
	"SearchEntitiesByName func(context.Context, string, string, string, int) ([]querycontract.EntityContent, error)",
}

// impactReadAdmissionGraphStub is the test-only graph satisfier behind
// the witness: it answers the two pinned graph reads.
type impactReadAdmissionGraphStub struct{}

func (impactReadAdmissionGraphStub) Run(context.Context, string, map[string]any) ([]map[string]any, error) {
	return nil, nil
}

func (impactReadAdmissionGraphStub) RunSingle(context.Context, string, map[string]any) (map[string]any, error) {
	return nil, nil
}

// impactReadAdmissionContentStub is the test-only content satisfier
// behind the witness: it answers the eight pinned ContentStore reads.
type impactReadAdmissionContentStub struct{}

func (impactReadAdmissionContentStub) GetEntityContent(context.Context, string) (*querycontract.EntityContent, error) {
	return nil, nil
}

func (impactReadAdmissionContentStub) SearchEntitiesByName(context.Context, string, string, string, int) ([]querycontract.EntityContent, error) {
	return nil, nil
}

func (impactReadAdmissionContentStub) SearchEntitiesByExactNameAnyRepo(context.Context, string, string, int) ([]querycontract.EntityContent, error) {
	return nil, nil
}

func (impactReadAdmissionContentStub) ListRepoEntities(context.Context, string, int) ([]querycontract.EntityContent, error) {
	return nil, nil
}

func (impactReadAdmissionContentStub) ListRepoEntitiesByType(context.Context, string, string, int) ([]querycontract.EntityContent, error) {
	return nil, nil
}

func (impactReadAdmissionContentStub) ListRepoEntitiesByPaths(context.Context, string, []string, int) ([]querycontract.EntityContent, error) {
	return nil, nil
}

func (impactReadAdmissionContentStub) ListRepoEntitiesByIDs(context.Context, string, []string, int) ([]querycontract.EntityContent, error) {
	return nil, nil
}

func (impactReadAdmissionContentStub) ListRepoK8sSelectCandidates(context.Context, string, int) ([]querycontract.K8sSelectCandidate, error) {
	return nil, nil
}

// impactReadAdmissionCodeTopicStub is the test-only code-topic
// satisfier behind the witness: it answers the pinned topic search
// the change-surface root adapter runs.
type impactReadAdmissionCodeTopicStub struct{}

func (impactReadAdmissionCodeTopicStub) InvestigateCodeTopic(context.Context, codequery.CodeTopicInvestigationRequest) ([]codequery.CodeTopicEvidenceRow, error) {
	return nil, nil
}

// impactReadAdmissionLiveStub is the test-only live-evidence satisfier
// behind the witness: it answers the two pinned pod-template reads.
type impactReadAdmissionLiveStub struct{}

func (impactReadAdmissionLiveStub) HasLiveIdentityMatch(context.Context, deployment.KubernetesPodTemplateFilter) (bool, error) {
	return false, nil
}

func (impactReadAdmissionLiveStub) ListLiveIdentityMatches(context.Context, deployment.KubernetesPodTemplateFilter) ([]deployment.LiveIdentityMatch, error) {
	return nil, nil
}

// impactReadAdmissionECSStub is the test-only ECS satisfier behind the
// witness: it answers the pinned running-task read.
type impactReadAdmissionECSStub struct{}

func (impactReadAdmissionECSStub) ListRunningTasks(context.Context, string, querycontract.RepositoryAccessFilter) (*deployment.ECSServiceTaskEvidence, error) {
	return nil, nil
}

// impactReadAdmissionWitness proves ImpactReadAdmission is satisfiable:
// the graph, content, topic, live, and ECS reads compose jointly, so
// no single store carries all fourteen methods. It composes test-only
// stubs rather than inventing a production satisfier — the seam stays
// unwired.
type impactReadAdmissionWitness struct {
	impactReadAdmissionGraphStub
	impactReadAdmissionContentStub
	impactReadAdmissionCodeTopicStub
	impactReadAdmissionLiveStub
	impactReadAdmissionECSStub
}

var _ ImpactReadAdmission = impactReadAdmissionWitness{}

// impactReadAdmissionResignedWitness carries a deliberately re-signed
// ListRepoEntitiesByPaths (an extra trailing parameter). It MUST NOT
// satisfy ImpactReadAdmission: the seeded rejection test below proves
// a signature drift on any pinned method breaks the pin instead of
// sliding through silently.
type impactReadAdmissionResignedWitness struct {
	impactReadAdmissionWitness
}

func (impactReadAdmissionResignedWitness) ListRepoEntitiesByPaths(context.Context, string, []string, int, int) ([]querycontract.EntityContent, error) {
	return nil, nil
}

// impactReadAdmissionMethodSet reflects the live ImpactReadAdmission
// method set into sorted "name signature" strings for comparison with
// the frozen list. The iface parameter forces every caller —
// including the seeded-drift RED proof below — through this same guard
// comparison, so a passing suite proves the guard itself can see drift
// rather than proving a private copy can.
func impactReadAdmissionMethodSet(t *testing.T, iface any) []string {
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

// TestImpactReadAdmissionContractIsVersioned pins the #9669 W5
// admission-contract version for the impact family reads. The frozen
// method set below is the enforcement: an added, removed, renamed, or
// re-signed method fails this test until the frozen list is
// deliberately updated. Bump ImpactReadAdmissionContractVersion
// alongside (manually — the bump is convention, the frozen list is the
// enforcement) so dest consumers can pin the shape.
func TestImpactReadAdmissionContractIsVersioned(t *testing.T) {
	t.Parallel()

	if ImpactReadAdmissionContractVersion != "v0" {
		t.Errorf("ImpactReadAdmissionContractVersion = %q, want %q: bump it deliberately with the shape change", ImpactReadAdmissionContractVersion, "v0")
	}
	got := impactReadAdmissionMethodSet(t, (*ImpactReadAdmission)(nil))
	if len(got) != len(frozenImpactReadAdmission) {
		t.Fatalf("ImpactReadAdmission method set = %q (len %d), want frozen %q (len %d): bump %s with the shape change", got, len(got), frozenImpactReadAdmission, len(frozenImpactReadAdmission), ImpactReadAdmissionContractVersion)
	}
	for i := range got {
		if got[i] != frozenImpactReadAdmission[i] {
			t.Errorf("ImpactReadAdmission method set differs at index %d: got %q, want %q: bump %s with the shape change", i, got[i], frozenImpactReadAdmission[i], ImpactReadAdmissionContractVersion)
		}
	}
}

// TestImpactReadAdmissionRejectsResignedMethod is the seeded RED half
// of the contract proof: a witness whose ListRepoEntitiesByPaths
// carries one extra parameter MUST NOT satisfy the pin. If a re-signed
// method ever slides through, this test fails and the frozen shape
// above can no longer be trusted to catch drift.
func TestImpactReadAdmissionRejectsResignedMethod(t *testing.T) {
	t.Parallel()

	pin := reflect.TypeOf((*ImpactReadAdmission)(nil)).Elem()
	if reflect.TypeOf(impactReadAdmissionResignedWitness{}).Implements(pin) {
		t.Errorf("impactReadAdmissionResignedWitness implements ImpactReadAdmission: a re-signed ListRepoEntitiesByPaths must break the pin")
	}
	if !reflect.TypeOf(impactReadAdmissionWitness{}).Implements(pin) {
		t.Errorf("impactReadAdmissionWitness does not implement ImpactReadAdmission: the minimal shape must satisfy the pin")
	}
}

// TestImpactReadAdmissionWireShapesAreSealed proves the seal on the
// pin: exactly the fourteen re-declared impact reads, with no
// mutation, snapshot, ledger, or other-family surface riding along.
// The frozen method-set contract test pins the exact names; this test
// pins the exclusion the arbiter ruling demands.
func TestImpactReadAdmissionWireShapesAreSealed(t *testing.T) {
	t.Parallel()

	rt := reflect.TypeOf((*ImpactReadAdmission)(nil)).Elem()
	if rt.NumMethod() != 14 {
		t.Fatalf("ImpactReadAdmission has %d methods, want exactly 14: the seal forbids any wider surface", rt.NumMethod())
	}
	for i := range rt.NumMethod() {
		name := rt.Method(i).Name
		for _, banned := range []string{
			"Delete", "Create", "Update", "Revoke", "Upsert", "Grant", "Snapshot", "Ledger",
		} {
			if strings.Contains(name, banned) {
				t.Errorf("ImpactReadAdmission.%s contains %q: that leg stays out of this pin", name, banned)
			}
		}
	}
}

// seededImpactDrift is a deliberately widened copy of the pin: the
// fourteen frozen impact reads plus a deployment-trace write the
// impact lane never issues. It never ships; it proves the frozen-set
// comparison below can see a one-method widening.
type seededImpactDrift interface {
	ImpactReadAdmission
	RecordImpactSnapshot(context.Context, string) error
}

// TestImpactReadAdmissionSeededDriftIsDetected is the RED half of the
// seeded-drift pair: the same reflect comparison the contract test
// runs must report a mismatch on the planted widening. It passes on
// a clean tree precisely because the guard fires on drift — a guard
// that cannot see the seeded method is no guard.
func TestImpactReadAdmissionSeededDriftIsDetected(t *testing.T) {
	t.Parallel()

	got := impactReadAdmissionMethodSet(t, (*seededImpactDrift)(nil))
	if reflect.DeepEqual(got, frozenImpactReadAdmission) {
		t.Fatal("seeded widening (RecordImpactSnapshot) matches the frozen set: the comparison cannot see drift")
	}
	if len(got) != len(frozenImpactReadAdmission)+1 {
		t.Fatalf("seeded drift method set len = %d, want frozen len + 1 (%d): the seed is malformed", len(got), len(frozenImpactReadAdmission)+1)
	}
}
