// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/evidencebundle"
	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
)

// TestEvidenceBundleTerraformOmissionTheory checks whether the complete live
// bundle is independent of only the Terraform-state serial and warning reads.
// This is a theory fixture; it does not alter a status-reader selection.
func TestEvidenceBundleTerraformOmissionTheory(t *testing.T) {
	t.Parallel()

	fixed := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	base := evidenceBundleFixtureSnapshot()
	base.TerraformStateLastSerials = []statuspkg.TerraformStateLocatorSerial{{
		SafeLocatorHash: "safe-hash", BackendKind: "s3", Serial: 42,
		GenerationID: "generation-42", ObservedAt: fixed,
	}}
	base.TerraformStateRecentWarnings = []statuspkg.TerraformStateLocatorWarning{{
		SafeLocatorHash: "safe-hash", BackendKind: "s3", WarningKind: "state_missing",
		Reason: "state_missing", GenerationID: "generation-42", ObservedAt: fixed,
	}}
	base.CollectorFactEvidence = []statuspkg.CollectorFactEvidence{{
		InstanceID: "git-1", CollectorKind: "git", EvidenceSource: "file",
		ObservationCount: 8, LastObservedAt: fixed, UpdatedAt: fixed,
	}}
	base.RegistryCollectors = []statuspkg.RegistryCollectorSnapshot{{
		CollectorKind: "package_registry", ConfiguredInstances: 1,
		RecentCompletedGenerations: 2, LastCompletedAt: fixed,
	}}
	base.SemanticExtraction.ProviderProfiles = []statuspkg.SemanticProviderProfileStatus{{
		ProfileID: "profile-a", ProviderKind: "local", State: "configured",
		Reason: "configured",
	}}
	// More than DomainLimit exercises both the bounded rows and truncation
	// marker. These domains are separate from Terraform-state evidence.
	for i := 0; i < statuspkg.DefaultOptions().DomainLimit+2; i++ {
		base.DomainBacklogs = append(base.DomainBacklogs, statuspkg.DomainBacklog{
			Domain:      "theory_domain_" + string(rune('a'+i)),
			Outstanding: i + 1,
			OldestAge:   time.Duration(i+1) * time.Second,
		})
	}

	cases := []struct {
		name       string
		raw        statuspkg.RawSnapshot
		wantHealth string
		nonempty   bool
	}{
		{name: "stalled", raw: base, wantHealth: "stalled", nonempty: true},
		{name: "degraded", raw: func() statuspkg.RawSnapshot {
			raw := base
			raw.Queue.OverdueClaims = 0
			return raw
		}(), wantHealth: "degraded", nonempty: true},
		{name: "healthy", raw: func() statuspkg.RawSnapshot {
			raw := base
			raw.Queue = statuspkg.QueueSnapshot{Succeeded: 10}
			raw.QueueBlockages = nil
			raw.DomainBacklogs = nil
			raw.StageCounts = []statuspkg.StageStatusCount{{Stage: "parse", Status: "succeeded", Count: 11}}
			return raw
		}(), wantHealth: "healthy", nonempty: true},
		{name: "empty", raw: statuspkg.RawSnapshot{
			AsOf:                         fixed,
			TerraformStateLastSerials:    base.TerraformStateLastSerials,
			TerraformStateRecentWarnings: base.TerraformStateRecentWarnings,
		}, wantHealth: "healthy"},
	}

	if len(base.CollectorFactEvidence) == 0 || len(base.RegistryCollectors) == 0 {
		t.Fatal("representative fixture lacks fact-backed collector or registry evidence")
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fullReport := statuspkg.BuildReport(tc.raw, statuspkg.DefaultOptions())
			if tc.nonempty && (len(fullReport.CollectorFactEvidence) == 0 || len(fullReport.RegistryCollectors) == 0) {
				t.Fatal("full report lost fact-backed collector or registry evidence")
			}
			if len(fullReport.TerraformState.LastSerials) == 0 || len(fullReport.TerraformState.RecentWarnings) == 0 {
				t.Fatal("fixture did not populate both Terraform-state report sections")
			}
			withoutTerraform := tc.raw
			withoutTerraform.TerraformStateLastSerials = nil
			withoutTerraform.TerraformStateRecentWarnings = nil
			trimmedReport := statuspkg.BuildReport(withoutTerraform, statuspkg.DefaultOptions())
			if len(trimmedReport.TerraformState.LastSerials) != 0 || len(trimmedReport.TerraformState.RecentWarnings) != 0 {
				t.Fatal("Terraform-state omission did not change the report")
			}
			if fullReport.Health.State != tc.wantHealth || trimmedReport.Health.State != tc.wantHealth {
				t.Fatalf("health states = %q, %q; want %q", fullReport.Health.State, trimmedReport.Health.State, tc.wantHealth)
			}

			options := evidencebundle.LiveBundleOptions{CreatedAt: fixed}
			full := evidencebundle.BuildLiveBundle(liveEvidenceSnapshotFromReport(fullReport, 5), options)
			trimmed := evidencebundle.BuildLiveBundle(liveEvidenceSnapshotFromReport(trimmedReport, 5), options)
			for label, bundle := range map[string]evidencebundle.Bundle{"full": full, "trimmed": trimmed} {
				if err := evidencebundle.Validate(bundle); err != nil {
					t.Fatalf("Validate(%s) = %v", label, err)
				}
			}
			full = evidencebundle.StampValidation(full)
			trimmed = evidencebundle.StampValidation(trimmed)
			if !reflect.DeepEqual(full, trimmed) {
				t.Fatalf("complete stamped bundles differ when only Terraform-state evidence is omitted:\nfull=%+v\ntrimmed=%+v", full, trimmed)
			}
			if tc.name == "stalled" {
				expected := loadFrozenEvidenceBundleTheory(t)
				if !reflect.DeepEqual(full, expected) || !reflect.DeepEqual(trimmed, expected) {
					t.Fatalf("full or trimmed bundle differs from independent frozen oracle:\nfull=%+v\ntrimmed=%+v\nexpected=%+v", full, trimmed, expected)
				}
				checkEvidenceBundleTheorySensitivity(t, tc.raw, fixed, expected)
			}
			if full.Validation.Status != "passed" || full.Bounds.MaxHandles == 0 || len(full.Redaction.Rules) == 0 {
				t.Fatalf("validation, bounds, or redaction missing: %+v", full)
			}
			if tc.nonempty {
				if full.Contents.PipelineState == nil || len(full.Contents.PipelineState.StageSummaries) == 0 ||
					len(full.Contents.PipelineState.Collectors) == 0 || full.Contents.SemanticProviderState == nil ||
					len(full.Contents.SemanticProviderState.ProviderProfiles) == 0 {
					t.Fatalf("pipeline, collector, or semantic fixture did not survive mapping: %+v", full.Contents)
				}
			}
			if tc.name == "stalled" && (!full.Bounds.Truncated || len(full.Contents.PipelineState.DomainBacklogs) != statuspkg.DefaultOptions().DomainLimit) {
				t.Fatalf("domain bound not exercised: %+v", full.Bounds)
			}

			// A field used by the mapper must change the full bundle. This
			// catches an empty or constant-output comparison.
			changed := withoutTerraform
			changed.Queue.Succeeded++
			changedBundle := evidencebundle.BuildLiveBundle(
				liveEvidenceSnapshotFromReport(statuspkg.BuildReport(changed, statuspkg.DefaultOptions()), 5), options)
			if err := evidencebundle.Validate(changedBundle); err != nil {
				t.Fatalf("Validate(changed) = %v", err)
			}
			changedBundle = evidencebundle.StampValidation(changedBundle)
			if reflect.DeepEqual(trimmed, changedBundle) || trimmed.BundleID == changedBundle.BundleID {
				t.Fatal("used queue field changed but stamped bundle did not")
			}
		})
	}
}

// loadFrozenEvidenceBundleTheory loads a hand-authored complete stalled bundle.
// Only its content hash is calculated here, from the frozen expected body;
// no expected field is copied from either candidate bundle.
func loadFrozenEvidenceBundleTheory(t *testing.T) evidencebundle.Bundle {
	t.Helper()
	raw, err := os.ReadFile("testdata/evidence_bundle_selection_expected.json")
	if err != nil {
		t.Fatal(err)
	}
	var expected evidencebundle.Bundle
	if err := json.Unmarshal(raw, &expected); err != nil {
		t.Fatal(err)
	}
	if expected.BundleID != "" {
		t.Fatal("frozen fixture bundle_id must be blank before independent hash")
	}
	canonical, err := json.Marshal(expected)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(canonical)
	expected.BundleID = "evidence-bundle:" + hex.EncodeToString(sum[:16])
	return expected
}

// checkEvidenceBundleTheorySensitivity ensures the equality proof can detect
// changes in every major non-Terraform input family the live mapper consumes.
func checkEvidenceBundleTheorySensitivity(t *testing.T, raw statuspkg.RawSnapshot, fixed time.Time, baseline evidencebundle.Bundle) {
	t.Helper()
	cases := []struct {
		name    string
		edit    func(*statuspkg.RawSnapshot)
		changed func(evidencebundle.Bundle, evidencebundle.Bundle) bool
	}{
		{
			name: "collector readiness from fact evidence",
			edit: func(snapshot *statuspkg.RawSnapshot) { snapshot.CollectorFactEvidence = nil },
			changed: func(before, after evidencebundle.Bundle) bool {
				return !reflect.DeepEqual(before.Contents.PipelineState.Collectors, after.Contents.PipelineState.Collectors)
			},
		},
		{
			name: "health and reasons",
			edit: func(snapshot *statuspkg.RawSnapshot) { snapshot.Queue.OverdueClaims = 0 },
			changed: func(before, after evidencebundle.Bundle) bool {
				return before.Contents.PipelineState.HealthState != after.Contents.PipelineState.HealthState &&
					!reflect.DeepEqual(before.Contents.PipelineState.HealthReasons, after.Contents.PipelineState.HealthReasons)
			},
		},
		{
			name: "stage and scope evidence",
			edit: func(snapshot *statuspkg.RawSnapshot) {
				snapshot.StageCounts = append([]statuspkg.StageStatusCount(nil), snapshot.StageCounts...)
				snapshot.StageCounts[0].Count++
				snapshot.ScopeActivity.Active++
				snapshot.ScopeActivity.Unchanged++
			},
			changed: func(before, after evidencebundle.Bundle) bool {
				return !reflect.DeepEqual(before.Contents.PipelineState.StageSummaries, after.Contents.PipelineState.StageSummaries) &&
					!reflect.DeepEqual(before.Contents.PipelineState.ScopeActivity, after.Contents.PipelineState.ScopeActivity)
			},
		},
		{
			name: "domain rows and truncation marker",
			edit: func(snapshot *statuspkg.RawSnapshot) { snapshot.DomainBacklogs = snapshot.DomainBacklogs[:1] },
			changed: func(before, after evidencebundle.Bundle) bool {
				return !reflect.DeepEqual(before.Contents.PipelineState.DomainBacklogs, after.Contents.PipelineState.DomainBacklogs) &&
					before.Bounds.Truncated && !after.Bounds.Truncated
			},
		},
		{
			name: "semantic profile",
			edit: func(snapshot *statuspkg.RawSnapshot) {
				snapshot.SemanticExtraction.ProviderProfiles = append([]statuspkg.SemanticProviderProfileStatus(nil), snapshot.SemanticExtraction.ProviderProfiles...)
				snapshot.SemanticExtraction.ProviderProfiles[0].Reason = "updated"
			},
			changed: func(before, after evidencebundle.Bundle) bool {
				return !reflect.DeepEqual(before.Contents.SemanticProviderState, after.Contents.SemanticProviderState)
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			changed := raw
			tc.edit(&changed)
			result := statuspkg.BuildReport(changed, statuspkg.DefaultOptions())
			bundle := evidencebundle.BuildLiveBundle(liveEvidenceSnapshotFromReport(result, 5), evidencebundle.LiveBundleOptions{CreatedAt: fixed})
			if err := evidencebundle.Validate(bundle); err != nil {
				t.Fatalf("Validate(changed) = %v", err)
			}
			bundle = evidencebundle.StampValidation(bundle)
			if !tc.changed(baseline, bundle) || reflect.DeepEqual(baseline, bundle) || baseline.BundleID == bundle.BundleID {
				t.Fatal("used input mutation did not change its expected output fields and complete bundle")
			}
		})
	}
}
