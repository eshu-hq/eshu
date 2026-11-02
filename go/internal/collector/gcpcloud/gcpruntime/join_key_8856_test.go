// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Correlation join-key proof for #8856 (AC4, in-tree side).
//
// The cutover gate (#4047) needs more than emission parity: it needs proof
// that the exact bytes the reducer admission keys on survive the emission
// path. The loader (postgres/cloud_inventory_evidence.go) keys admission on
// the SQL COALESCE raw identity (= payload full_resource_name) with provider
// "gcp", and the admission handler resolves it with
// cloudinventory.ResolveProviderIdentity. These tests wire the parity live
// source straight into that production resolver and pin the agreement:
//
//   - every gcp_cloud_resource envelope carries its CAI input full resource
//     name verbatim, and the resolver admits it;
//   - the admitted uid is stable across duplicate scans (idempotent join);
//   - the classifier still rejects blank (unresolved) and near-miss
//     (ambiguous) identities instead of minting uids.
//
// The extracted producer must reproduce this same agreement against the same
// fixture pack; the pack plus these assertions are the #4047 gate input for
// the join-key half of the correlation architecture gate.

package gcpruntime

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/correlation/cloudinventory"
	"github.com/eshu-hq/eshu/go/internal/facts/cloud"
)

// parity8856InputNames indexes every CAI full resource name the parity live
// source emits, keyed by name, so the proof below can demand verbatim
// survival through emission.
func parity8856InputNames() map[string]bool {
	names := map[string]bool{}
	for _, obs := range parity8856PageOne() {
		names[obs.Name] = true
	}
	for _, obs := range parity8856PageTwo() {
		names[obs.Name] = true
	}
	for _, obs := range parity8856ExpiredPage() {
		names[obs.Name] = true
	}
	return names
}

// drain8856EnvelopesByStableKey runs one full live scan and indexes the
// emitted envelopes by stable key.
func drain8856EnvelopesByStableKey(t *testing.T, source *Source) map[string]string {
	t.Helper()
	byKey := map[string]string{}
	for {
		collected, ok, err := source.Next(context.Background())
		if err != nil {
			t.Fatalf("Next() error = %v", err)
		}
		if !ok {
			break
		}
		for _, envelope := range drainFacts(t, collected) {
			if envelope.FactKind != cloud.GCPCloudResourceFactKind {
				continue
			}
			name, _ := envelope.Payload["full_resource_name"].(string)
			byKey[envelope.StableFactKey] = name
		}
	}
	return byKey
}

// TestGCP8856JoinKeysSurviveToCanonicalUID pins the correlation-gate join:
// emission output agrees byte-for-byte with the reducer admission input, so
// the extracted producer cannot silently re-key canonical truth.
func TestGCP8856JoinKeysSurviveToCanonicalUID(t *testing.T) {
	t.Parallel()

	wantNames := parity8856InputNames()
	first := drain8856EnvelopesByStableKey(t, parity8856LiveSource(t))
	if len(first) == 0 {
		t.Fatal("live scan emitted no gcp_cloud_resource envelopes")
	}
	for key, name := range first {
		if name == "" {
			t.Fatalf("stable key %q carries a blank full_resource_name", key)
		}
		if !wantNames[name] {
			t.Fatalf("emitted full_resource_name %q is not a verbatim CAI input name", name)
		}
		resolution := cloudinventory.ResolveProviderIdentity(cloudinventory.ProviderGCP, name)
		if resolution.Outcome != cloudinventory.ResolutionOutcomeAdmitted {
			t.Fatalf("join key %q resolves %q, want admitted", name, resolution.Outcome)
		}
		if resolution.CloudResourceUID == "" {
			t.Fatalf("join key %q admitted with an empty uid", name)
		}
	}

	// Duplicate delivery must join identically: the same stable keys resolve
	// to the same uids on a second scan.
	second := drain8856EnvelopesByStableKey(t, parity8856LiveSource(t))
	if len(second) != len(first) {
		t.Fatalf("second scan envelopes = %d, want %d", len(second), len(first))
	}
	for key, name := range first {
		other, ok := second[key]
		if !ok {
			t.Fatalf("stable key %q missing from second scan", key)
		}
		firstUID := cloudinventory.ResolveProviderIdentity(cloudinventory.ProviderGCP, name).CloudResourceUID
		secondUID := cloudinventory.ResolveProviderIdentity(cloudinventory.ProviderGCP, other).CloudResourceUID
		if firstUID != secondUID {
			t.Fatalf("uid drift for stable key %q: %q vs %q", key, firstUID, secondUID)
		}
	}
}

// TestGCP8856JoinClassifierRejectsNonKeyable pins the negative and ambiguous
// halves: blank identities stay unresolved and near-miss shapes stay
// ambiguous, so neither can mint canonical truth at admission.
func TestGCP8856JoinClassifierRejectsNonKeyable(t *testing.T) {
	t.Parallel()

	if got := cloudinventory.ResolveProviderIdentity(cloudinventory.ProviderGCP, ""); got.Outcome != cloudinventory.ResolutionOutcomeUnresolved || got.CloudResourceUID != "" {
		t.Fatalf("blank identity = (%q, %q), want (unresolved, empty uid)", got.Outcome, got.CloudResourceUID)
	}
	if got := cloudinventory.ResolveProviderIdentity(cloudinventory.ProviderGCP, "projects/parity-proj"); got.Outcome != cloudinventory.ResolutionOutcomeAmbiguous || got.CloudResourceUID != "" {
		t.Fatalf("near-miss identity = (%q, %q), want (ambiguous, empty uid)", got.Outcome, got.CloudResourceUID)
	}
}
