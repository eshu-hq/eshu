// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestComposeReplatformingPlanOrdersWavesAndBlastRadius proves the HTTP route
// returns deterministic migration waves and blast-radius groups, assigns each
// item its wave_id/blast_radius_group, orders gated items into the final wave,
// and exposes bounded per-wave and per-group summaries. It exercises independent,
// shared (many dependents), missing-evidence, and safety-gated findings together.
func TestComposeReplatformingPlanOrdersWavesAndBlastRadius(t *testing.T) {
	t.Parallel()

	rows := []IaCManagementFindingRow{
		{
			// Independent, import-ready, no dependents -> early wave, none blast.
			ID:               "fact:safe-independent",
			Provider:         "aws",
			ResourceType:     "s3",
			ResourceID:       "bucket-independent",
			ARN:              "arn:aws:s3:::bucket-independent",
			FindingKind:      findingKindOrphanedCloudResource,
			ManagementStatus: managementStatusCloudOnly,
			ScopeID:          "aws:123456789012:us-east-1:s3",
			SafetyGate:       readySafetyGate(),
		},
		{
			// Shared resource with many dependency paths -> larger blast radius,
			// review wave (high blast radius, even though import is ready).
			ID:               "fact:shared-hub",
			Provider:         "aws",
			ResourceType:     "s3",
			ResourceID:       "bucket-shared",
			ARN:              "arn:aws:s3:::bucket-shared",
			FindingKind:      findingKindOrphanedCloudResource,
			ManagementStatus: managementStatusCloudOnly,
			ScopeID:          "aws:123456789012:us-east-1:s3",
			DependencyPaths:  []string{"svc-a->bucket", "svc-b->bucket", "svc-c->bucket", "svc-d->bucket", "svc-e->bucket", "svc-f->bucket"},
			SafetyGate:       readySafetyGate(),
		},
		{
			// Shared resource that is import-ready with a small dependency footprint
			// -> review wave (refused-free but low-medium blast radius keeps it out
			// of the earliest wave once we cross the low threshold).
			ID:               "fact:shared-small",
			Provider:         "aws",
			ResourceType:     "s3",
			ResourceID:       "bucket-shared-small",
			ARN:              "arn:aws:s3:::bucket-shared-small",
			FindingKind:      findingKindOrphanedCloudResource,
			ManagementStatus: managementStatusCloudOnly,
			ScopeID:          "aws:123456789012:us-east-1:s3",
			DependencyPaths:  []string{"svc-a->bucket", "svc-b->bucket", "svc-c->bucket"},
			SafetyGate:       readySafetyGate(),
		},
		{
			// Unknown status with missing deployment evidence -> review-gated by the
			// shared safety normalizer, so it lands in the blocked wave. This proves
			// missing deployment evidence never silently joins an early wave.
			ID:               "fact:missing-evidence",
			Provider:         "aws",
			ResourceType:     "s3",
			ResourceID:       "bucket-missing",
			ARN:              "arn:aws:s3:::bucket-missing",
			FindingKind:      findingKindUnknownCloudResource,
			ManagementStatus: managementStatusUnknown,
			ScopeID:          "aws:123456789012:us-east-1:s3",
			MissingEvidence:  []string{"no_deployment_chain", "no_terraform_state"},
			SafetyGate:       readySafetyGate(),
		},
		{
			// Safety-gated security resource -> blocked wave, ordered last.
			ID:               "fact:gated",
			Provider:         "aws",
			ResourceType:     "ec2",
			ResourceID:       "security-group/sg-9",
			ARN:              "arn:aws:ec2:us-east-1:123456789012:security-group/sg-9",
			FindingKind:      findingKindOrphanedCloudResource,
			ManagementStatus: managementStatusCloudOnly,
			ScopeID:          "aws:123456789012:us-east-1:ec2",
			WarningFlags:     []string{"security_sensitive_resource"},
			SafetyGate: IaCManagementSafetyGate{
				Outcome:        "security_review_required",
				ReadOnly:       true,
				ReviewRequired: true,
				RefusedActions: []string{"terraform_import_plan"},
				Warnings:       []string{"security_sensitive_resource"},
			},
		},
	}
	handler := &IaCHandler{
		Profile:    ProfileLocalAuthoritative,
		Management: fakeIaCManagementStore{rows: rows},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/replatforming/plans", bytes.NewBufferString(`{
		"scope_kind": "account",
		"account_id": "123456789012",
		"region": "us-east-1",
		"limit": 50
	}`))
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	_, data := decodeReplatformingPlanResponse(t, w)
	plan := data["plan"].(map[string]any)

	waves, ok := plan["waves"].([]any)
	if !ok || len(waves) == 0 {
		t.Fatalf("plan waves missing or empty: %#v", plan["waves"])
	}
	// Waves must be in strictly increasing order starting at 1.
	for i, raw := range waves {
		wave := raw.(map[string]any)
		if got, want := wave["order"], float64(i+1); got != want {
			t.Fatalf("wave[%d].order = %#v, want %#v", i, got, want)
		}
	}
	// The last wave is the blocked wave: it owns the safety-gated item and the
	// review-gated missing-evidence item, sorted, and never an early-safe item.
	lastWave := waves[len(waves)-1].(map[string]any)
	if got, want := lastWave["id"], replatformingWaveBlocked; got != want {
		t.Fatalf("last wave id = %q, want %q", got, want)
	}
	lastIDs := lastWave["item_ids"].([]any)
	if len(lastIDs) != 2 || lastIDs[0] != "fact:gated" || lastIDs[1] != "fact:missing-evidence" {
		t.Fatalf("last wave item_ids = %#v, want [fact:gated fact:missing-evidence]", lastIDs)
	}

	groups, ok := plan["blast_radius_groups"].([]any)
	if !ok || len(groups) == 0 {
		t.Fatalf("plan blast_radius_groups missing or empty: %#v", plan["blast_radius_groups"])
	}

	// Every item must carry both wave_id and blast_radius_group membership.
	waveByItem := map[string]string{}
	groupByItem := map[string]string{}
	for _, raw := range plan["items"].([]any) {
		item := raw.(map[string]any)
		id := item["item_id"].(string)
		wid, _ := item["wave_id"].(string)
		gid, _ := item["blast_radius_group"].(string)
		if wid == "" {
			t.Fatalf("item %q missing wave_id", id)
		}
		if gid == "" {
			t.Fatalf("item %q missing blast_radius_group", id)
		}
		waveByItem[id] = wid
		groupByItem[id] = gid
	}
	if waveByItem["fact:safe-independent"] != replatformingWaveEarly {
		t.Fatalf("independent item wave = %q, want %q", waveByItem["fact:safe-independent"], replatformingWaveEarly)
	}
	if waveByItem["fact:gated"] != replatformingWaveBlocked {
		t.Fatalf("gated item wave = %q, want %q", waveByItem["fact:gated"], replatformingWaveBlocked)
	}
	if waveByItem["fact:missing-evidence"] != replatformingWaveBlocked {
		t.Fatalf("missing-evidence item wave = %q, want %q (review-gated)", waveByItem["fact:missing-evidence"], replatformingWaveBlocked)
	}
	if waveByItem["fact:shared-hub"] != replatformingWaveReview {
		t.Fatalf("shared-hub item wave = %q, want %q (high blast radius)", waveByItem["fact:shared-hub"], replatformingWaveReview)
	}
	if groupByItem["fact:safe-independent"] != replatformingBlastGroupNone {
		t.Fatalf("independent item blast group = %q, want %q", groupByItem["fact:safe-independent"], replatformingBlastGroupNone)
	}
	if groupByItem["fact:shared-hub"] != replatformingBlastGroupHigh {
		t.Fatalf("shared item blast group = %q, want %q", groupByItem["fact:shared-hub"], replatformingBlastGroupHigh)
	}
	if groupByItem["fact:gated"] != replatformingBlastGroupBlocked {
		t.Fatalf("gated item blast group = %q, want %q", groupByItem["fact:gated"], replatformingBlastGroupBlocked)
	}

	// Bounded summaries must be present and count-consistent.
	waveSummaries := data["wave_summaries"].([]any)
	if len(waveSummaries) != len(waves) {
		t.Fatalf("wave_summaries length = %d, want %d", len(waveSummaries), len(waves))
	}
	blastSummaries := data["blast_radius_summaries"].([]any)
	if len(blastSummaries) != len(groups) {
		t.Fatalf("blast_radius_summaries length = %d, want %d", len(blastSummaries), len(groups))
	}
}
