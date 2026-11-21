// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"fmt"
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/securityalert"
	"github.com/eshu-hq/eshu/go/internal/reducer/servicecatalog"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
)

// waitingLocalityViolations lists every deferral row whose provider
// locality breaks the #11114 contract: a narrowed producer-terminal row
// must declare a valid locality, and every other row must leave it empty
// (the cross-scope leg never reads it there, so a value would mislead).
func waitingLocalityViolations(entries []WaitingDeferral) []string {
	var out []string
	for _, entry := range entries {
		narrowed := entry.WaitsForKind == WaitingKindProducerTerminal && entry.ProducerDomain != ""
		switch {
		case narrowed && !entry.ProviderLocality.Valid():
			out = append(out, fmt.Sprintf("%s: narrowed producer-terminal row declares locality %q", entry.FailureClass, entry.ProviderLocality))
		case !narrowed && entry.ProviderLocality != "":
			out = append(out, fmt.Sprintf("%s: non-narrowed row declares locality %q", entry.FailureClass, entry.ProviderLocality))
		}
	}
	return out
}

// TestWaitingDeferralMapDeclaresProviderLocality is the #11114 completeness
// guard: the shipped map has no locality violation, and each seeded
// violation (an undeclared narrowed row, an invalid value, a stray value on
// a non-narrowed row) is caught.
func TestWaitingDeferralMapDeclaresProviderLocality(t *testing.T) {
	t.Parallel()
	shipped := WaitingDeferralMap()
	if got := waitingLocalityViolations(shipped); len(got) != 0 {
		t.Fatalf("shipped deferral map violations:\n%v", got)
	}
	seed := func(class string, locality WaitingProviderLocality) []WaitingDeferral {
		out := slices.Clone(shipped)
		for i := range out {
			if out[i].FailureClass == class {
				out[i].ProviderLocality = locality
				return out
			}
		}
		t.Fatalf("class %s not in the deferral map", class)
		return nil
	}
	cases := []struct {
		name    string
		entries []WaitingDeferral
	}{
		{"narrowed row with no locality", seed(securityalert.ImpactNotReadyFailureClass, "")},
		{"narrowed row with an invalid locality", seed(servicecatalog.RuntimeNotReadyFailureClass, "elsewhere")},
		{"non-narrowed row with a locality", seed(reducercontract.GenerationActivationNotReadyFailureClass, WaitingProviderCrossScope)},
	}
	for _, tc := range cases {
		if got := waitingLocalityViolations(tc.entries); len(got) != 1 {
			t.Fatalf("%s: violations = %v, want exactly one", tc.name, got)
		}
	}
}

// TestProducerTerminalWakePlanCrossScopeClasses pins the plan the shipped
// map yields: only workload_materialization carries a cross-scope class
// (the catalog runtime wait); the impact and attachment domains are
// narrowed but same-scope, so their Acks issue no cross-scope statement.
// An undeclared narrowed row plans as cross-scope (over-wake, never a lost
// wake).
func TestProducerTerminalWakePlanCrossScopeClasses(t *testing.T) {
	t.Parallel()
	plan := newProducerTerminalWakePlan(WaitingDeferralMap())
	want := map[string][]string{
		string(reducer.DomainWorkloadMaterialization): {servicecatalog.RuntimeNotReadyFailureClass},
	}
	if len(plan.crossScopeClasses) != len(want) {
		t.Fatalf("cross-scope classes = %v, want %v", plan.crossScopeClasses, want)
	}
	for domain, classes := range want {
		if !slices.Equal(plan.crossScopeClasses[domain], classes) {
			t.Fatalf("cross-scope classes[%s] = %v, want %v", domain, plan.crossScopeClasses[domain], classes)
		}
	}
	for _, domain := range []string{
		string(reducer.DomainWorkloadMaterialization),
		string(reducer.DomainSupplyChainImpact),
		string(reducercontract.DomainSBOMAttestationAttachment),
	} {
		if !plan.narrowed[domain] {
			t.Fatalf("%s is not narrowed: its Ack would issue no keyed wake", domain)
		}
	}
	undeclared := slices.Clone(WaitingDeferralMap())
	for i := range undeclared {
		if undeclared[i].FailureClass == supplychaincore.SBOMAttachmentNotReadyFailureClass {
			undeclared[i].ProviderLocality = ""
		}
	}
	got := newProducerTerminalWakePlan(undeclared).crossScopeClasses[string(reducercontract.DomainSBOMAttestationAttachment)]
	if !slices.Equal(got, []string{supplychaincore.SBOMAttachmentNotReadyFailureClass}) {
		t.Fatalf("undeclared attachment row planned cross-scope classes %v, want it kept on the cross-scope leg", got)
	}
}
