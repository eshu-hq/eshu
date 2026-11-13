// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// frozenCodeDeadCodeReadAdmission pins the exact method set behind the
// versioned #9669 admission contract for the dead-code content-fallback
// reads: the graph run behind the SQL-routine EXECUTES probes, the
// candidate/incoming/downgrade reads behind the investigation and scan,
// the entity hydration reads, the coverage and index-freshness reads,
// and the three cross-repo consumer-evidence reads. RunSingle is OUT by
// construction: the dead-code lane never issues a single-row graph
// read. The entries re-declare the GraphQuery/ContentReader signatures
// in this dead-code-scoped shape instead of embedding the shared ports,
// so no other family can cite this pin — a blanket or shared-port pin
// is forbidden, and the widened shape needs arbiter review before
// satellite consumption.
//
// Satellite consumer: the dead-code content-fallback lane, per the
// arbiter SUPPLEMENTAL-PIN-OWED ruling on eshu-hq/eshu-api#124.
var frozenCodeDeadCodeReadAdmission = []string{
	"CodeReachabilityIncomingEntityIDs func(context.Context, string, []string, []string) (map[string]code.DeadCodeIncomingEdge, error)",
	"CrossRepoDeadCodeConsumerCoverage func(context.Context, code.CrossRepoDeadCodeCoverageRequest) (code.CrossRepoDeadCodeCoverage, error)",
	"CrossRepoDeadCodeConsumerEvidence func(context.Context, string, []string, code.CrossRepoDeadCodeConsumerReads) (map[string][]deadcode.CrossRepoDeadCodeEvidence, code.CrossRepoDeadCodeHiddenConsumers, error)",
	"CrossRepoDeadCodeConsumerRootPaths func(context.Context, []string, []string) (map[string]string, error)",
	"DeadCodeCandidateRows func(context.Context, codeshaping.DeadCodeCandidateQuery) ([]map[string]interface {}, error)",
	"DeadCodeIncomingEntityIDs func(context.Context, string, []string) (map[string]code.DeadCodeIncomingEdge, error)",
	"DowngradedCodeRootKinds func(context.Context, string, []string) (map[string]map[string]struct {}, error)",
	"GetEntityContent func(context.Context, string) (*querycontract.EntityContent, error)",
	"GetEntityContents func(context.Context, []string) (map[string]*querycontract.EntityContent, error)",
	"RepositoryContextCoverage func(context.Context, string) (querycontract.RepositoryContentCoverage, error)",
	"RepositoryCoverage func(context.Context, string) (querycontract.RepositoryContentCoverage, error)",
	"RepositoryFilesLastIndexedAt func(context.Context, string) (time.Time, error)",
	"RepositoryRelationshipReadModel func(context.Context, string) (repository.RepositoryRelationshipReadModel, error)",
	"Run func(context.Context, string, map[string]interface {}) ([]map[string]interface {}, error)",
}

// codeDeadCodeReadAdmissionMethodSet reflects the live
// CodeDeadCodeReadAdmission method set into sorted "name signature"
// strings for comparison with the frozen list.
func codeDeadCodeReadAdmissionMethodSet(t *testing.T, iface any) []string {
	t.Helper()
	rt := reflect.TypeOf(iface).Elem()
	var got []string
	for i := range rt.NumMethod() {
		m := rt.Method(i)
		got = append(got, m.Name+" "+m.Type.String())
	}
	sort.Strings(got)
	return got
}

// TestCodeDeadCodeReadAdmissionContractIsVersioned pins the #9669
// admission-contract version for the dead-code content-fallback reads.
// The version value plus the frozen method set below are the
// enforcement: a breaking change to the admission shape fails this test
// until the version bumps in the same diff.
func TestCodeDeadCodeReadAdmissionContractIsVersioned(t *testing.T) {
	t.Parallel()

	if CodeDeadCodeReadAdmissionContractVersion != "v0" {
		t.Errorf("CodeDeadCodeReadAdmissionContractVersion = %q, want %q: bump it deliberately with the shape change", CodeDeadCodeReadAdmissionContractVersion, "v0")
	}
	got := codeDeadCodeReadAdmissionMethodSet(t, (*CodeDeadCodeReadAdmission)(nil))
	if len(got) != len(frozenCodeDeadCodeReadAdmission) {
		t.Fatalf("CodeDeadCodeReadAdmission method set = %q (len %d), want frozen %q (len %d): bump %s with the shape change", got, len(got), frozenCodeDeadCodeReadAdmission, len(frozenCodeDeadCodeReadAdmission), CodeDeadCodeReadAdmissionContractVersion)
	}
	for i := range got {
		if got[i] != frozenCodeDeadCodeReadAdmission[i] {
			t.Errorf("CodeDeadCodeReadAdmission method set differs at index %d: got %q, want %q: bump %s with the shape change", i, got[i], frozenCodeDeadCodeReadAdmission[i], CodeDeadCodeReadAdmissionContractVersion)
		}
	}
}

// TestCodeDeadCodeReadAdmissionWireShapesAreSealed proves the seal on
// the pin: exactly the fourteen re-declared dead-code reads, with
// RunSingle and every search, list, file, ledger, stored-summary, or
// snapshot surface riding along refused. The frozen method-set contract
// test pins the exact names; this test pins the exclusions the arbiter
// ruling demands.
func TestCodeDeadCodeReadAdmissionWireShapesAreSealed(t *testing.T) {
	t.Parallel()

	rt := reflect.TypeOf((*CodeDeadCodeReadAdmission)(nil)).Elem()
	if rt.NumMethod() != 14 {
		t.Fatalf("CodeDeadCodeReadAdmission has %d methods, want exactly 14: the seal forbids any wider surface", rt.NumMethod())
	}
	for i := range rt.NumMethod() {
		name := rt.Method(i).Name
		for _, banned := range []string{
			"RunSingle", "Snapshot", "Ledger", "Summary", "StartupError",
			"Search", "List", "GetFile", "ExactCase", "FileReference",
			"ByExactName", "ReferencingComponent", "ByLanguageAndType",
			"NonBlank",
		} {
			if strings.Contains(name, banned) {
				t.Errorf("CodeDeadCodeReadAdmission.%s contains %q: that leg stays out of this pin", name, banned)
			}
		}
	}
}

// seededCodeDeadCodeDrift is a deliberately widened copy of the pin:
// the fourteen frozen reads plus RunSingle, which the dead-code lane
// keeps OUT. It never ships; it proves the frozen-set comparison below
// can see a one-method widening.
type seededCodeDeadCodeDrift interface {
	CodeDeadCodeReadAdmission
	RunSingle(context.Context, string, map[string]any) (map[string]any, error)
}

// TestCodeDeadCodeReadAdmissionSeededDriftIsDetected is the RED half of
// the seeded-drift pair: the same reflect comparison the contract test
// runs must report a mismatch on the planted widening. It passes on a
// clean tree precisely because the guard fires on drift — a guard that
// cannot see the seeded method is no guard.
func TestCodeDeadCodeReadAdmissionSeededDriftIsDetected(t *testing.T) {
	t.Parallel()

	got := codeDeadCodeReadAdmissionMethodSet(t, (*seededCodeDeadCodeDrift)(nil))
	if reflect.DeepEqual(got, frozenCodeDeadCodeReadAdmission) {
		t.Fatal("seeded widening (RunSingle) matches the frozen set: the comparison cannot see drift")
	}
	if len(got) != len(frozenCodeDeadCodeReadAdmission)+1 {
		t.Fatalf("seeded drift method set len = %d, want frozen len + 1 (%d): the seed is malformed", len(got), len(frozenCodeDeadCodeReadAdmission)+1)
	}
}
