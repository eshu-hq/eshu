// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package freshness

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/gcpcloud"
)

func temporalAssetPushEnvelope(t *testing.T, data string) []byte {
	t.Helper()
	envelope := struct {
		Message struct {
			Data        string `json:"data"`
			MessageID   string `json:"messageId"`
			PublishTime string `json:"publishTime"`
		} `json:"message"`
		Subscription string `json:"subscription"`
	}{}
	envelope.Message.Data = base64.StdEncoding.EncodeToString([]byte(data))
	envelope.Message.MessageID = "123456789012345"
	envelope.Message.PublishTime = "2026-06-01T12:00:00.000Z"
	envelope.Subscription = "projects/demo/subscriptions/gcp-freshness"
	encoded, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("marshal push envelope: %v", err)
	}
	return encoded
}

func TestNormalizePubSubPushAcceptsAssetChange(t *testing.T) {
	t.Parallel()

	temporalAsset := `{
		"asset": {
			"name": "//compute.googleapis.com/projects/123456789012/zones/us-central1-a/instances/vm-1",
			"assetType": "compute.googleapis.com/Instance",
			"ancestors": ["projects/123456789012", "folders/999", "organizations/1"],
			"updateTime": "2026-06-01T11:59:00Z",
			"resource": {
				"location": "us-central1-a",
				"data": {"status": "RUNNING", "networkInterfaces": [{"networkIP": "10.0.0.5"}]}
			}
		},
		"deleted": false,
		"window": {"startTime": "2026-06-01T11:59:00Z"}
	}`
	payload := temporalAssetPushEnvelope(t, temporalAsset)

	trigger, err := NormalizePubSubPush(payload)
	if err != nil {
		t.Fatalf("NormalizePubSubPush() error = %v, want nil", err)
	}
	if trigger.Kind != EventKindAssetChange {
		t.Fatalf("Kind = %q, want %q", trigger.Kind, EventKindAssetChange)
	}
	if trigger.ParentScopeKind != gcpcloud.ParentScopeProject {
		t.Fatalf("ParentScopeKind = %q, want %q", trigger.ParentScopeKind, gcpcloud.ParentScopeProject)
	}
	if trigger.ParentScopeID != "123456789012" {
		t.Fatalf("ParentScopeID = %q, want %q", trigger.ParentScopeID, "123456789012")
	}
	if trigger.AssetType != "compute.googleapis.com/Instance" {
		t.Fatalf("AssetType = %q, want %q", trigger.AssetType, "compute.googleapis.com/Instance")
	}
	if trigger.Location != "us-central1-a" {
		t.Fatalf("Location = %q, want %q", trigger.Location, "us-central1-a")
	}
	if trigger.EventID == "" {
		t.Fatal("EventID is empty, want messageId-derived event id")
	}
	if trigger.ObservedAt.IsZero() {
		t.Fatal("ObservedAt is zero, want asset update time")
	}
}

func TestNormalizePubSubPushAcceptsAssetDeleted(t *testing.T) {
	t.Parallel()

	temporalAsset := `{
		"priorAsset": {
			"name": "//compute.googleapis.com/projects/123456789012/zones/us-central1-a/instances/vm-1",
			"assetType": "compute.googleapis.com/Instance",
			"ancestors": ["projects/123456789012"],
			"updateTime": "2026-06-01T11:59:00Z",
			"resource": {"location": "us-central1-a"}
		},
		"deleted": true,
		"priorAssetState": "PRESENT",
		"window": {"startTime": "2026-06-01T12:01:00Z"}
	}`
	payload := temporalAssetPushEnvelope(t, temporalAsset)

	trigger, err := NormalizePubSubPush(payload)
	if err != nil {
		t.Fatalf("NormalizePubSubPush() error = %v, want nil", err)
	}
	if trigger.Kind != EventKindAssetDeleted {
		t.Fatalf("Kind = %q, want %q", trigger.Kind, EventKindAssetDeleted)
	}
	if trigger.ParentScopeID != "123456789012" {
		t.Fatalf("ParentScopeID = %q, want %q", trigger.ParentScopeID, "123456789012")
	}
}

func TestNormalizePubSubPushDerivesFolderScope(t *testing.T) {
	t.Parallel()

	temporalAsset := `{
		"asset": {
			"name": "//cloudresourcemanager.googleapis.com/folders/999",
			"assetType": "cloudresourcemanager.googleapis.com/Folder",
			"ancestors": ["folders/999", "organizations/1"],
			"updateTime": "2026-06-01T11:59:00Z",
			"resource": {}
		},
		"deleted": false
	}`
	payload := temporalAssetPushEnvelope(t, temporalAsset)

	trigger, err := NormalizePubSubPush(payload)
	if err != nil {
		t.Fatalf("NormalizePubSubPush() error = %v, want nil", err)
	}
	if trigger.ParentScopeKind != gcpcloud.ParentScopeFolder {
		t.Fatalf("ParentScopeKind = %q, want %q", trigger.ParentScopeKind, gcpcloud.ParentScopeFolder)
	}
	if trigger.ParentScopeID != "999" {
		t.Fatalf("ParentScopeID = %q, want %q", trigger.ParentScopeID, "999")
	}
	if trigger.Location != "" {
		t.Fatalf("Location = %q, want empty for a global resource", trigger.Location)
	}
}

func TestNormalizePubSubPushDerivesOrganizationScope(t *testing.T) {
	t.Parallel()

	temporalAsset := `{
		"asset": {
			"name": "//cloudresourcemanager.googleapis.com/organizations/1",
			"assetType": "cloudresourcemanager.googleapis.com/Organization",
			"ancestors": ["organizations/1"],
			"updateTime": "2026-06-01T11:59:00Z",
			"resource": {}
		},
		"deleted": false
	}`
	payload := temporalAssetPushEnvelope(t, temporalAsset)

	trigger, err := NormalizePubSubPush(payload)
	if err != nil {
		t.Fatalf("NormalizePubSubPush() error = %v, want nil", err)
	}
	if trigger.ParentScopeKind != gcpcloud.ParentScopeOrganization {
		t.Fatalf("ParentScopeKind = %q, want %q", trigger.ParentScopeKind, gcpcloud.ParentScopeOrganization)
	}
	if trigger.ParentScopeID != "1" {
		t.Fatalf("ParentScopeID = %q, want %q", trigger.ParentScopeID, "1")
	}
}

func TestNormalizePubSubPushIgnoresWelcomeMessage(t *testing.T) {
	t.Parallel()

	// The first delivery to a new CAI feed subscription is a bare JSON string
	// welcome message, not a TemporalAsset object. It must be treated as a
	// benign no-op, not a malformed-event error.
	payload := temporalAssetPushEnvelope(t, `"You have successfully subscribed to..."`)

	_, err := NormalizePubSubPush(payload)
	if err != ErrWelcomeMessage {
		t.Fatalf("NormalizePubSubPush() error = %v, want ErrWelcomeMessage", err)
	}
}

func TestNormalizePubSubPushRejectsMalformedEnvelope(t *testing.T) {
	t.Parallel()

	if _, err := NormalizePubSubPush([]byte(`not json`)); err == nil {
		t.Fatal("NormalizePubSubPush() error = nil, want error for malformed envelope")
	}
}

func TestNormalizePubSubPushRejectsMalformedBase64(t *testing.T) {
	t.Parallel()

	payload := []byte(`{"message":{"data":"not-valid-base64!!","messageId":"1"}}`)
	if _, err := NormalizePubSubPush(payload); err == nil {
		t.Fatal("NormalizePubSubPush() error = nil, want error for malformed base64")
	}
}

func TestNormalizePubSubPushRejectsMalformedTemporalAssetJSON(t *testing.T) {
	t.Parallel()

	payload := temporalAssetPushEnvelope(t, `{"asset": "not-an-object"`)
	if _, err := NormalizePubSubPush(payload); err == nil {
		t.Fatal("NormalizePubSubPush() error = nil, want error for malformed TemporalAsset JSON")
	}
}

func TestNormalizePubSubPushRejectsMissingAncestors(t *testing.T) {
	t.Parallel()

	temporalAsset := `{
		"asset": {
			"name": "//compute.googleapis.com/projects/123456789012/zones/us-central1-a/instances/vm-1",
			"assetType": "compute.googleapis.com/Instance",
			"ancestors": [],
			"updateTime": "2026-06-01T11:59:00Z",
			"resource": {"location": "us-central1-a"}
		},
		"deleted": false
	}`
	payload := temporalAssetPushEnvelope(t, temporalAsset)

	if _, err := NormalizePubSubPush(payload); err == nil {
		t.Fatal("NormalizePubSubPush() error = nil, want error when ancestors carry no parent scope")
	}
}

func TestNormalizePubSubPushDropsResourceDataBlob(t *testing.T) {
	t.Parallel()

	// The raw resource.data blob (network IPs, config) must never surface on
	// the normalized Trigger; only bounded scope identity survives.
	temporalAsset := `{
		"asset": {
			"name": "//compute.googleapis.com/projects/123456789012/zones/us-central1-a/instances/vm-1",
			"assetType": "compute.googleapis.com/Instance",
			"ancestors": ["projects/123456789012"],
			"updateTime": "2026-06-01T11:59:00Z",
			"resource": {
				"location": "us-central1-a",
				"data": {"networkInterfaces": [{"networkIP": "10.0.0.99", "natIP": "34.1.2.3"}]}
			}
		},
		"deleted": false
	}`
	payload := temporalAssetPushEnvelope(t, temporalAsset)

	trigger, err := NormalizePubSubPush(payload)
	if err != nil {
		t.Fatalf("NormalizePubSubPush() error = %v, want nil", err)
	}
	encoded, err := json.Marshal(trigger)
	if err != nil {
		t.Fatalf("marshal trigger: %v", err)
	}
	if strings.Contains(string(encoded), "10.0.0.99") || strings.Contains(string(encoded), "34.1.2.3") {
		t.Fatalf("Trigger leaked raw resource.data blob content: %s", encoded)
	}
}
