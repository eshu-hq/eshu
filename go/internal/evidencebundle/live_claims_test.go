// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package evidencebundle

import (
	"encoding/json"
	"testing"
)

// Tests here cover what an exported bundle CLAIMS about itself -- its redaction
// rule names, its validation stamp, and whether an artifact stays valid away
// from the binary that wrote it. live_redaction_test.go covers the screening
// rules themselves.

// TestValidateKeepsRealDomainNamesUsable is the false-positive guard for the
// credential rule. It matched the bare word "secret", and
// "secrets_iam_trust_chain" and "secrets_iam_graph_projection" are real
// materialization domains -- so a stack with backlog in either one could not
// export a live bundle at all. A screen that rejects honest bundles fails the
// feature just as badly as one that passes a leak.
func TestValidateKeepsRealDomainNamesUsable(t *testing.T) {
	for _, domain := range []string{
		"secrets_iam_trust_chain",
		"secrets_iam_graph_projection",
		"appflow_connector_profile_uses_secret",
		"aws_appsync_api_key",
	} {
		t.Run(domain, func(t *testing.T) {
			snapshot := realisticLiveSnapshot()
			snapshot.DomainBacklogs = []LiveDomainBacklogSnapshot{{Domain: domain, Outstanding: 2}}
			snapshot.StageSummaries = []LiveStageSummarySnapshot{{Stage: domain, Pending: 1}}
			bundle := BuildLiveBundle(snapshot, LiveBundleOptions{ScopeID: "live:x", CreatedAt: fixedLiveCreatedAt})
			if err := Validate(bundle); err != nil {
				t.Fatalf("Validate rejected the real domain name %q: %v", domain, err)
			}
		})
	}
}

// TestValidateStillRejectsAssignedCredentials is the counterweight: narrowing
// the credential rule to an assignment shape must not stop it catching a value
// actually being handed over.
func TestValidateStillRejectsAssignedCredentials(t *testing.T) {
	for _, reason := range []string{
		`password=hunter2`,
		`"api_key": "abcdef"`,
		`secret: swordfish`,
		`api-key=abcdef`,
		`Authorization: Bearer abcdef`,
	} {
		t.Run(reason, func(t *testing.T) {
			snapshot := realisticLiveSnapshot()
			snapshot.HealthReasons = []string{reason}
			bundle := BuildLiveBundle(snapshot, LiveBundleOptions{ScopeID: "live:x", CreatedAt: fixedLiveCreatedAt})
			if err := Validate(bundle); err == nil {
				t.Fatalf("Validate accepted an assigned credential in %q", reason)
			}
		})
	}
}

// TestValidateAcceptsAnArtifactFromAnOlderBinary pins why Validate does not
// rehash the content and compare it to bundle_id. That check looks like free
// integrity and instead breaks the artifact's purpose: bundle_id hashes the
// struct as it exists in the binary doing the check, so a bundle exported
// before any field was added within evidence_bundle.v1 rehashes differently and
// would be rejected by a newer `eshu evidence bundle validate --from`, despite
// being valid when written. It would not establish provenance anyway -- whoever
// edits a body can recompute the hash.
func TestValidateAcceptsAnArtifactFromAnOlderBinary(t *testing.T) {
	snapshot := realisticLiveSnapshot()
	// The removed keys must carry non-zero values, or deleting them leaves the
	// content identical after decode and this proves nothing.
	snapshot.Queue.Total = 18
	snapshot.Queue.Outstanding = 7
	snapshot.Queue.OverdueClaims = 3
	snapshot.Queue.OldestOutstandingAgeS = 12.5
	bundle := StampValidation(BuildLiveBundle(snapshot, LiveBundleOptions{
		ScopeID: "live:x", CreatedAt: fixedLiveCreatedAt,
	}))
	raw, err := RenderJSON(bundle)
	if err != nil {
		t.Fatalf("RenderJSON: %v", err)
	}

	var decoded Bundle
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if err := Validate(decoded); err != nil {
		t.Fatalf("Validate rejected its own rendered artifact: %v", err)
	}

	// The same artifact as an older binary wrote it: identical bytes minus the
	// queue keys a later version added.
	var generic map[string]any
	if err := json.Unmarshal(raw, &generic); err != nil {
		t.Fatalf("Unmarshal generic: %v", err)
	}
	queue := generic["contents"].(map[string]any)["pipeline_state"].(map[string]any)["queue"].(map[string]any)
	for _, key := range []string{"total", "outstanding", "overdue_claims", "oldest_outstanding_age_seconds"} {
		delete(queue, key)
	}
	older, err := json.Marshal(generic)
	if err != nil {
		t.Fatalf("Marshal older: %v", err)
	}
	var olderBundle Bundle
	if err := json.Unmarshal(older, &olderBundle); err != nil {
		t.Fatalf("Unmarshal older: %v", err)
	}
	if err := Validate(olderBundle); err != nil {
		t.Fatalf("Validate rejected an artifact exported by an older binary: %v", err)
	}
}

// TestRedactionRulesNameScreensNotGuarantees pins the honesty fix. The rules
// are advertised in every exported artifact, and screening by denylist cannot
// support a categorical "no private endpoints" claim -- naming the screen is
// what the code actually does.
func TestRedactionRulesNameScreensNotGuarantees(t *testing.T) {
	bundles := map[string]Bundle{
		"demo": BuildDemoBundle(DemoBundleOptions{ScopeID: "repo:demo/service"}),
		"live": BuildLiveBundle(realisticLiveSnapshot(), LiveBundleOptions{ScopeID: "live:x", CreatedAt: fixedLiveCreatedAt}),
	}
	for name, bundle := range bundles {
		for _, rule := range bundle.Redaction.Rules {
			// An allowlist, not a "no_" denylist: "guarantees_no_private_endpoints"
			// would pass a prefix check while asserting exactly what a screen
			// cannot deliver.
			switch rule {
			case "handles_only", "screened_private_endpoints", "screened_credentials", "screened_model_inputs_or_outputs":
			default:
				t.Errorf("%s bundle advertises unrecognised rule %q; a rule must name a screen that runs, not an outcome", name, rule)
			}
		}
	}
}
