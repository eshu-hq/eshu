// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/redact"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	gcpv1 "github.com/eshu-hq/eshu/sdk/go/factschema/gcp/v1"
)

func testBoundary() Boundary {
	return Boundary{
		CollectorInstanceID: "gcp-instance-1",
		ParentScopeKind:     ParentScopeProject,
		ParentScopeID:       "my-project",
		AssetTypeFamily:     "compute",
		ContentFamily:       "resource",
		LocationBucket:      "us-central1-a",
		ScopeID:             "gcp:project:my-project:compute:resource:us-central1-a",
		GenerationID:        "gen-1",
		FencingToken:        7,
		ReadTime:            time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC),
		ObservedAt:          time.Date(2026, 6, 9, 12, 0, 5, 0, time.UTC),
	}
}

func testResourceObservation() ResourceObservation {
	return ResourceObservation{
		Name:        "//compute.googleapis.com/projects/my-project/zones/us-central1-a/instances/vm-1",
		AssetType:   "compute.googleapis.com/Instance",
		DisplayName: "vm-1",
		State:       "RUNNING",
		Location:    "us-central1-a",
		Ancestors:   []string{"projects/123456789", "folders/4455", "organizations/9988"},
		Labels:      map[string]string{"env": "prod"},
		UpdateTime:  time.Date(2026, 6, 9, 11, 59, 0, 0, time.UTC),
	}
}

func TestNewCloudResourceEnvelope(t *testing.T) {
	key := testRedactionKey(t)
	env, err := NewCloudResourceEnvelope(testBoundary(), testResourceObservation(), key)
	if err != nil {
		t.Fatalf("NewCloudResourceEnvelope: %v", err)
	}
	if env.FactKind != facts.GCPCloudResourceFactKind {
		t.Fatalf("FactKind = %q, want %q", env.FactKind, facts.GCPCloudResourceFactKind)
	}
	if env.SchemaVersion != facts.GCPCloudResourceSchemaVersion {
		t.Fatalf("SchemaVersion = %q", env.SchemaVersion)
	}
	if env.CollectorKind != CollectorKind {
		t.Fatalf("CollectorKind = %q, want %q", env.CollectorKind, CollectorKind)
	}
	if env.SourceConfidence != facts.SourceConfidenceReported {
		t.Fatalf("SourceConfidence = %q, want reported", env.SourceConfidence)
	}
	if env.FencingToken != 7 {
		t.Fatalf("FencingToken = %d, want 7", env.FencingToken)
	}
	payload := env.Payload
	if payload["full_resource_name"] != testResourceObservation().Name {
		t.Fatalf("full_resource_name = %v, want raw provider identity preserved", payload["full_resource_name"])
	}
	if payload["asset_type"] != "compute.googleapis.com/Instance" {
		t.Fatalf("asset_type = %v", payload["asset_type"])
	}
	if payload["project_id"] != "my-project" {
		t.Fatalf("project_id = %v, want my-project", payload["project_id"])
	}
	if payload["project_number"] != "123456789" {
		t.Fatalf("project_number = %v, want 123456789", payload["project_number"])
	}
	if payload["organization_number"] != "9988" {
		t.Fatalf("organization_number = %v", payload["organization_number"])
	}
	if payload["redaction_policy_version"] != RedactionPolicyVersion {
		t.Fatalf("redaction_policy_version = %v", payload["redaction_policy_version"])
	}
	ext, ok := payload["extension"].(map[string]any)
	if !ok {
		t.Fatalf("extension not a map: %T", payload["extension"])
	}
	if ext["schema_version"] == nil {
		t.Fatal("extension missing schema_version")
	}
}

func TestNewCloudResourceEnvelopeDeterministicStableKey(t *testing.T) {
	key := testRedactionKey(t)
	a, err := NewCloudResourceEnvelope(testBoundary(), testResourceObservation(), key)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	b, err := NewCloudResourceEnvelope(testBoundary(), testResourceObservation(), key)
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if a.StableFactKey != b.StableFactKey {
		t.Fatalf("stable fact key not deterministic: %q vs %q", a.StableFactKey, b.StableFactKey)
	}
	if a.FactID != b.FactID {
		t.Fatalf("fact id not deterministic: %q vs %q", a.FactID, b.FactID)
	}
}

func TestCloudResourceStableKeyChangesWithUpdateTime(t *testing.T) {
	key := testRedactionKey(t)
	a, _ := NewCloudResourceEnvelope(testBoundary(), testResourceObservation(), key)
	obs := testResourceObservation()
	obs.UpdateTime = obs.UpdateTime.Add(time.Hour)
	b, _ := NewCloudResourceEnvelope(testBoundary(), obs, key)
	if a.StableFactKey == b.StableFactKey {
		t.Fatal("stable fact key should change when provider update time changes")
	}
}

func TestNewCloudResourceEnvelopeValidation(t *testing.T) {
	key := testRedactionKey(t)
	t.Run("missing full resource name", func(t *testing.T) {
		obs := testResourceObservation()
		obs.Name = ""
		if _, err := NewCloudResourceEnvelope(testBoundary(), obs, key); err == nil {
			t.Fatal("want error for missing full_resource_name")
		}
	})
	t.Run("missing asset type", func(t *testing.T) {
		obs := testResourceObservation()
		obs.AssetType = ""
		if _, err := NewCloudResourceEnvelope(testBoundary(), obs, key); err == nil {
			t.Fatal("want error for missing asset_type")
		}
	})
	t.Run("invalid parent scope kind", func(t *testing.T) {
		b := testBoundary()
		b.ParentScopeKind = "galaxy"
		if _, err := NewCloudResourceEnvelope(b, testResourceObservation(), key); err == nil {
			t.Fatal("want error for invalid parent scope kind")
		}
	})
	t.Run("zero fencing token", func(t *testing.T) {
		b := testBoundary()
		b.FencingToken = 0
		if _, err := NewCloudResourceEnvelope(b, testResourceObservation(), key); err == nil {
			t.Fatal("want error for non-positive fencing token")
		}
	})
}

func TestNewCloudResourceEnvelopeRedactsLabels(t *testing.T) {
	key := testRedactionKey(t)
	obs := testResourceObservation()
	obs.Labels = map[string]string{"env": "prod", "owner": "alice@example.com"}
	obs.LabelFingerprint = map[string]string{"owner": ""}
	env, err := NewCloudResourceEnvelope(testBoundary(), obs, key)
	if err != nil {
		t.Fatalf("NewCloudResourceEnvelope: %v", err)
	}
	labels := env.Payload["labels"].(map[string]string)
	if labels["env"] != "prod" {
		t.Fatalf("env label not preserved: %q", labels["env"])
	}
	if labels["owner"] == "alice@example.com" {
		t.Fatal("owner label value not fingerprinted")
	}
}

func TestGCPEnvelopePayloadsMatchFactschemaDirectMapJSON(t *testing.T) {
	key := testRedactionKey(t)
	boundary := testBoundary()
	resourceObs := testResourceObservation()

	resourceEnv, err := NewCloudResourceEnvelope(boundary, resourceObs, key)
	if err != nil {
		t.Fatalf("NewCloudResourceEnvelope: %v", err)
	}
	wantResource, err := expectedResourcePayload(boundary, resourceObs, key)
	if err != nil {
		t.Fatalf("expected resource payload: %v", err)
	}
	assertSameJSON(t, resourceEnv.Payload, wantResource)

	warningObs := WarningObservation{
		Boundary:    boundary,
		WarningKind: WarningKindPartialPermission,
		Outcome:     OutcomePartial,
		Reason:      "missing roles/cloudasset.viewer on folder",
		Retryable:   false,
		HiddenCount: 3,
	}
	warningEnv, err := NewCollectionWarningEnvelope(warningObs)
	if err != nil {
		t.Fatalf("NewCollectionWarningEnvelope: %v", err)
	}
	wantWarning, err := expectedWarningPayload(warningObs)
	if err != nil {
		t.Fatalf("expected warning payload: %v", err)
	}
	assertSameJSON(t, warningEnv.Payload, wantWarning)
}

func expectedResourcePayload(boundary Boundary, obs ResourceObservation, key redact.Key) (map[string]any, error) {
	fullName := obs.Name
	assetType := obs.AssetType
	ancestry := NormalizeAncestry(obs.Ancestors)
	projectID := ProjectIDFromFullName(fullName)
	updateTime := obs.UpdateTime.UTC()
	labels := FingerprintLabelValues(obs.Labels, fingerprintKeys(obs.LabelFingerprint), key)
	location := obs.Location
	displayName := obs.DisplayName
	state := obs.State
	assetTypeFamily := boundary.AssetTypeFamily

	return factschema.EncodeGCPCloudResource(gcpv1.Resource{
		FullResourceName:   fullName,
		AssetType:          assetType,
		ProjectID:          &projectID,
		Location:           &location,
		DisplayName:        &displayName,
		State:              &state,
		AssetTypeFamily:    &assetTypeFamily,
		CorrelationAnchors: cloneStrings(obs.CorrelationAnchors),
		Attributes: map[string]any{
			"collector_instance_id":    boundary.CollectorInstanceID,
			"parent_scope_kind":        string(boundary.ParentScopeKind),
			"parent_scope_id":          boundary.ParentScopeID,
			"asset_type_family":        boundary.AssetTypeFamily,
			"content_family":           boundary.ContentFamily,
			"location_bucket":          boundary.LocationBucket,
			"full_resource_name":       fullName,
			"asset_type":               assetType,
			"display_name":             obs.DisplayName,
			"state":                    obs.State,
			"location":                 obs.Location,
			"project_id":               projectID,
			"project_number":           ancestry.ProjectNumber,
			"folder_numbers":           ancestry.FolderNumbers,
			"organization_number":      ancestry.OrganizationNumber,
			"ancestors":                ancestry.Chain,
			"labels":                   labels,
			"read_time":                timeOrNil(boundary.ReadTime),
			"update_time":              timeOrNil(updateTime),
			"update_time_present":      !updateTime.IsZero(),
			"redaction_policy_version": RedactionPolicyVersion,
			"extension":                extensionObject(obs),
			"attributes":               cloneAnyMap(obs.Attributes),
			"correlation_anchors":      cloneStrings(obs.CorrelationAnchors),
		},
	})
}

func expectedWarningPayload(obs WarningObservation) (map[string]any, error) {
	reason := obs.Reason
	hiddenCount := int64(obs.HiddenCount)
	payload, err := factschema.EncodeGCPCollectionWarning(gcpv1.CollectionWarning{
		WarningKind: obs.WarningKind,
		Outcome:     obs.Outcome,
		Reason:      &reason,
		Retryable:   &obs.Retryable,
		HiddenCount: &hiddenCount,
	})
	if err != nil {
		return nil, err
	}
	addGCPBoundaryPayload(payload, obs.Boundary)
	payload["read_time"] = timeOrNil(obs.Boundary.ReadTime)
	payload["hidden_count"] = obs.HiddenCount
	return payload, nil
}

func assertSameJSON(t *testing.T, got, want map[string]any) {
	t.Helper()
	gotBytes, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal got: %v", err)
	}
	wantBytes, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("marshal want: %v", err)
	}
	if !bytes.Equal(gotBytes, wantBytes) {
		t.Fatalf("payload JSON mismatch\ngot:  %s\nwant: %s", gotBytes, wantBytes)
	}
}

func TestNewCollectionWarningEnvelope(t *testing.T) {
	obs := WarningObservation{
		Boundary:    testBoundary(),
		WarningKind: WarningKindPartialPermission,
		Outcome:     OutcomePartial,
		Reason:      "missing roles/cloudasset.viewer on folder",
		Retryable:   false,
		HiddenCount: 3,
	}
	env, err := NewCollectionWarningEnvelope(obs)
	if err != nil {
		t.Fatalf("NewCollectionWarningEnvelope: %v", err)
	}
	if env.FactKind != facts.GCPCollectionWarningFactKind {
		t.Fatalf("FactKind = %q", env.FactKind)
	}
	if env.Payload["warning_kind"] != WarningKindPartialPermission {
		t.Fatalf("warning_kind = %v", env.Payload["warning_kind"])
	}
	if env.Payload["outcome"] != OutcomePartial {
		t.Fatalf("outcome = %v", env.Payload["outcome"])
	}
	if env.Payload["hidden_count"] != 3 {
		t.Fatalf("hidden_count = %v, want 3", env.Payload["hidden_count"])
	}
}

func TestCollectionWarningValidation(t *testing.T) {
	t.Run("invalid warning kind", func(t *testing.T) {
		obs := WarningObservation{Boundary: testBoundary(), WarningKind: "nope", Outcome: OutcomePartial}
		if _, err := NewCollectionWarningEnvelope(obs); err == nil {
			t.Fatal("want error for invalid warning kind")
		}
	})
	t.Run("invalid outcome", func(t *testing.T) {
		obs := WarningObservation{Boundary: testBoundary(), WarningKind: WarningKindQuota, Outcome: "nope"}
		if _, err := NewCollectionWarningEnvelope(obs); err == nil {
			t.Fatal("want error for invalid outcome")
		}
	})
}
