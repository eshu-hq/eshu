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
	"encoding/json"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/correlation/cloudinventory"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/cloud"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
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

// mirror8856Envelopes drains one live scan and partitions the secrets/IAM
// mirror envelopes by kind.
func mirror8856Envelopes(t *testing.T, source *Source) (principals, permissions, trusts []facts.Envelope) {
	t.Helper()
	for {
		collected, ok, err := source.Next(context.Background())
		if err != nil {
			t.Fatalf("Next() error = %v", err)
		}
		if !ok {
			break
		}
		for _, envelope := range drainFacts(t, collected) {
			switch envelope.FactKind {
			case "gcp_iam_principal":
				principals = append(principals, envelope)
			case "gcp_iam_permission_policy":
				permissions = append(permissions, envelope)
			case "gcp_iam_trust_policy":
				trusts = append(trusts, envelope)
			}
		}
	}
	return principals, permissions, trusts
}

// TestGCP8856MirrorEnvelopesJoin pins the restamped mirror half of the
// correlation gate (#8856 P1/P2). It drains the three mirror kinds through
// the production admission anchors — CollectorKind/SourceSystem stamping plus
// the payloadcore.PayloadString fingerprint keys the secretsiam trust-chain
// reducer indexes on — and asserts the restamped envelopes join:
//
//   - every mirror envelope stamps collector_kind "gcp" (fails if the
//     re-stamp is removed) with SourceSystem "secrets_iam_posture"
//     (the safety premise the reducer-output argument relies on);
//   - every permission grant's principal_fingerprint resolves to an emitted
//     principal (the exact-chain join; an orphan grant invents no identity);
//   - every trust carries well-formed join anchors (email digest plus target
//     and trusted fingerprints); the parity trust targets a principal with no
//     grant fact, which production records as a missing_gcp_principal gap,
//     so the test pins the anchors, not a chain that needs a k8s lane;
//   - mirror payloads carry no raw member identity (redaction posture) and
//     are byte-stable across duplicate scans.
//
// Payload identity is pinned structurally rather than by hash so the pin
// survives unrelated payload evolution while still failing on any stamp,
// SourceSystem, redaction, or join-key drift.
func TestGCP8856MirrorEnvelopesJoin(t *testing.T) {
	t.Parallel()

	principals, permissions, trusts := mirror8856Envelopes(t, parity8856LiveSource(t))
	if len(principals) == 0 || len(permissions) == 0 || len(trusts) == 0 {
		t.Fatalf("mirror scan = %d principals, %d permissions, %d trusts; want at least one of each",
			len(principals), len(permissions), len(trusts))
	}
	mirrors := append(append(append([]facts.Envelope{}, principals...), permissions...), trusts...)
	for _, env := range mirrors {
		if env.CollectorKind != "gcp" {
			t.Fatalf("mirror kind %q stamps collector_kind %q, want gcp", env.FactKind, env.CollectorKind)
		}
		if env.SourceRef.SourceSystem != "secrets_iam_posture" {
			t.Fatalf("mirror kind %q has SourceSystem %q, want secrets_iam_posture", env.FactKind, env.SourceRef.SourceSystem)
		}
		raw, err := json.Marshal(env.Payload)
		if err != nil {
			t.Fatalf("mirror kind %q payload does not marshal: %v", env.FactKind, err)
		}
		for _, leak := range []string{"ci@parity-proj", "deployer@parity-proj"} {
			if strings.Contains(string(raw), leak) {
				t.Fatalf("mirror kind %q payload leaks raw identity %q", env.FactKind, leak)
			}
		}
	}

	principalFPs := map[string]bool{}
	for _, env := range principals {
		fp := payloadcore.PayloadString(env.Payload, "principal_fingerprint")
		if fp == "" {
			t.Fatalf("principal envelope %q carries a blank join fingerprint", env.StableFactKey)
		}
		principalFPs[fp] = true
	}
	for _, env := range permissions {
		fp := payloadcore.PayloadString(env.Payload, "principal_fingerprint")
		if fp == "" {
			t.Fatalf("permission envelope %q carries a blank join fingerprint", env.StableFactKey)
		}
		if !principalFPs[fp] {
			t.Fatalf("permission %q joins no principal (fingerprint %q): orphan grants invent no identity", env.StableFactKey, fp)
		}
	}
	for _, env := range trusts {
		for _, key := range []string{"target_principal_fingerprint", "trusted_member_fingerprint", "target_service_account_email_digest"} {
			if payloadcore.PayloadString(env.Payload, key) == "" {
				t.Fatalf("trust envelope %q carries a blank join anchor %q", env.StableFactKey, key)
			}
		}
	}
}

// TestGCP8856MirrorPayloadsStableAcrossScans pins payload determinism: two
// duplicate live scans must emit byte-identical mirror payloads, so the
// re-stamp cannot smuggle a payload change past review.
func TestGCP8856MirrorPayloadsStableAcrossScans(t *testing.T) {
	t.Parallel()

	canonical := func(source *Source) map[string]string {
		principals, permissions, trusts := mirror8856Envelopes(t, source)
		out := map[string]string{}
		for _, env := range append(append(principals, permissions...), trusts...) {
			raw, err := json.Marshal(env.Payload)
			if err != nil {
				t.Fatalf("marshal payload: %v", err)
			}
			out[env.StableFactKey] = string(raw)
		}
		return out
	}
	first := canonical(parity8856LiveSource(t))
	second := canonical(parity8856LiveSource(t))
	if len(first) != len(second) {
		t.Fatalf("mirror envelope count drifted: %d vs %d", len(first), len(second))
	}
	for key, payload := range first {
		other, ok := second[key]
		if !ok {
			t.Fatalf("stable key %q missing from second scan", key)
		}
		if payload != other {
			t.Fatalf("payload drift for stable key %q", key)
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
