// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/secretsiam"
)

const instanceFullName = "//compute.googleapis.com/projects/demo-project/zones/us-central1-a/instances/web-1"

func instanceContext(data string) ExtractContext {
	return ExtractContext{
		FullResourceName: instanceFullName,
		AssetType:        assetTypeComputeInstance,
		ProjectID:        "demo-project",
		Data:             json.RawMessage(data),
	}
}

func TestInstanceExtractorIsRegistered(t *testing.T) {
	if _, ok := lookupAssetExtractor(assetTypeComputeInstance); !ok {
		t.Fatalf("expected %q extractor to self-register via init()", assetTypeComputeInstance)
	}
}

func TestExtractInstanceFullResource(t *testing.T) {
	const data = `{
		"name": "web-1",
		"machineType": "https://www.googleapis.com/compute/v1/projects/demo-project/zones/us-central1-a/machineTypes/e2-standard-4",
		"status": "RUNNING",
		"zone": "https://www.googleapis.com/compute/v1/projects/demo-project/zones/us-central1-a",
		"canIpForward": false,
		"deletionProtection": true,
		"creationTimestamp": "2024-06-01T00:00:00.000-07:00",
		"scheduling": {"preemptible": false, "automaticRestart": true, "onHostMaintenance": "MIGRATE", "provisioningModel": "STANDARD"},
		"shieldedInstanceConfig": {"enableSecureBoot": true, "enableVtpm": true, "enableIntegrityMonitoring": false},
		"serviceAccounts": [
			{"email": "runtime@demo-project.iam.gserviceaccount.com", "scopes": ["https://www.googleapis.com/auth/cloud-platform", "https://www.googleapis.com/auth/devstorage.read_only"]}
		],
		"networkInterfaces": [
			{
				"network": "https://www.googleapis.com/compute/v1/projects/demo-project/global/networks/prod-vpc",
				"subnetwork": "https://www.googleapis.com/compute/v1/projects/demo-project/regions/us-central1/subnetworks/prod-subnet",
				"networkIP": "10.0.0.5",
				"accessConfigs": [{"type": "ONE_TO_ONE_NAT", "name": "External NAT", "natIP": "203.0.113.7"}]
			}
		],
		"disks": [
			{"source": "https://www.googleapis.com/compute/v1/projects/demo-project/zones/us-central1-a/disks/boot-disk", "boot": true, "deviceName": "persistent-disk-0"},
			{"source": "https://www.googleapis.com/compute/v1/projects/demo-project/zones/us-central1-a/disks/data-disk", "boot": false, "deviceName": "persistent-disk-1"}
		],
		"metadata": {"items": [{"key": "startup-script", "value": "#!/bin/bash\necho secret"}, {"key": "enable-oslogin", "value": "TRUE"}], "fingerprint": "abc"},
		"tags": {"items": ["http-server", "https-server"], "fingerprint": "def"},
		"labels": {"team": "platform"}
	}`

	got, err := extractInstance(instanceContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantAttrs := map[string]any{
		"machine_type":                 "e2-standard-4",
		"status":                       "RUNNING",
		"zone":                         "us-central1-a",
		"can_ip_forward":               false,
		"deletion_protection":          true,
		"creation_time":                "2024-06-01T07:00:00Z",
		"preemptible":                  false,
		"automatic_restart":            true,
		"on_host_maintenance":          "MIGRATE",
		"provisioning_model":           "STANDARD",
		"enable_secure_boot":           true,
		"enable_vtpm":                  true,
		"enable_integrity_monitoring":  false,
		"service_account_count":        1,
		"service_account_scopes":       []string{"https://www.googleapis.com/auth/cloud-platform", "https://www.googleapis.com/auth/devstorage.read_only"},
		"network_interface_count":      1,
		"has_external_ip":              true,
		"external_access_config_count": 1,
		"disk_count":                   2,
		"boot_disk_present":            true,
		"metadata_keys":                []string{"startup-script", "enable-oslogin"},
		"network_tags":                 []string{"http-server", "https-server"},
	}
	if !reflect.DeepEqual(got.Attributes, wantAttrs) {
		t.Fatalf("attributes mismatch:\n got %#v\nwant %#v", got.Attributes, wantAttrs)
	}

	const (
		bootDisk   = "//compute.googleapis.com/projects/demo-project/zones/us-central1-a/disks/boot-disk"
		dataDisk   = "//compute.googleapis.com/projects/demo-project/zones/us-central1-a/disks/data-disk"
		network    = "//compute.googleapis.com/projects/demo-project/global/networks/prod-vpc"
		subnetwork = "//compute.googleapis.com/projects/demo-project/regions/us-central1/subnetworks/prod-subnet"
	)
	saDigest := secretsiam.GCPServiceAccountEmailDigest("runtime@demo-project.iam.gserviceaccount.com")

	wantAnchors := []string{bootDisk, dataDisk, network, subnetwork, saDigest}
	if !reflect.DeepEqual(got.CorrelationAnchors, wantAnchors) {
		t.Fatalf("anchors mismatch:\n got %#v\nwant %#v", got.CorrelationAnchors, wantAnchors)
	}

	if len(got.Relationships) != 4 {
		t.Fatalf("expected 4 edges (2 disks, network, subnetwork), got %d: %#v", len(got.Relationships), got.Relationships)
	}
	assertRelationship(t, got.Relationships, relationshipTypeInstanceUsesDisk, bootDisk, assetTypeComputeDisk)
	assertRelationship(t, got.Relationships, relationshipTypeInstanceUsesDisk, dataDisk, assetTypeComputeDisk)
	assertRelationship(t, got.Relationships, relationshipTypeInstanceInNetwork, network, assetTypeComputeNetwork)
	assertRelationship(t, got.Relationships, relationshipTypeInstanceInSubnetwork, subnetwork, assetTypeComputeSubnetwork)

	for _, rel := range got.Relationships {
		if rel.SourceFullResourceName != instanceFullName {
			t.Errorf("relationship source = %q, want instance full name", rel.SourceFullResourceName)
		}
		if rel.SourceAssetType != assetTypeComputeInstance {
			t.Errorf("relationship source asset type = %q, want %q", rel.SourceAssetType, assetTypeComputeInstance)
		}
		if rel.SupportState != RelationshipSupportSupported {
			t.Errorf("relationship support state = %q, want %q", rel.SupportState, RelationshipSupportSupported)
		}
	}
}

func TestExtractInstanceNoExternalIP(t *testing.T) {
	// A stopped instance with a private-only interface: no accessConfigs means no
	// external exposure, and posture fields present as explicit false survive.
	const data = `{
		"status": "TERMINATED",
		"networkInterfaces": [
			{"network": "projects/demo-project/global/networks/prod-vpc", "networkIP": "10.0.0.9"}
		]
	}`
	got, err := extractInstance(instanceContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Attributes["has_external_ip"] != false {
		t.Errorf("has_external_ip = %v, want false", got.Attributes["has_external_ip"])
	}
	if _, ok := got.Attributes["external_access_config_count"]; ok {
		t.Errorf("external_access_config_count must be omitted when zero: %#v", got.Attributes)
	}
	if got.Attributes["status"] != "TERMINATED" {
		t.Errorf("status = %v, want TERMINATED", got.Attributes["status"])
	}
	// Network edge still resolves from the partial reference.
	assertRelationship(t, got.Relationships, relationshipTypeInstanceInNetwork,
		"//compute.googleapis.com/projects/demo-project/global/networks/prod-vpc", assetTypeComputeNetwork)
}

func TestExtractInstanceIPv6OnlyExternalIsExposed(t *testing.T) {
	// An instance with only an external IPv6 access config (no IPv4 NAT) is still
	// externally reachable; the exposure signal must not report a false negative.
	const data = `{
		"networkInterfaces": [
			{
				"network": "projects/demo-project/global/networks/prod-vpc",
				"ipv6AccessConfigs": [{"type": "DIRECT_IPV6", "externalIpv6": "2600:1900::1"}]
			}
		]
	}`
	got, err := extractInstance(instanceContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Attributes["has_external_ip"] != true {
		t.Errorf("has_external_ip = %v, want true (external IPv6 present)", got.Attributes["has_external_ip"])
	}
	if got.Attributes["external_access_config_count"] != 1 {
		t.Errorf("external_access_config_count = %v, want 1", got.Attributes["external_access_config_count"])
	}
	// The IPv6 address value must never be persisted.
	blob, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal extraction: %v", err)
	}
	if containsString(string(blob), "2600:1900") {
		t.Fatalf("extraction leaked external IPv6 address: %s", blob)
	}
}

func TestExtractInstanceNeverLeaksIPsOrMetadataValues(t *testing.T) {
	const data = `{
		"networkInterfaces": [
			{"network": "projects/p/global/networks/n", "networkIP": "10.9.9.9", "accessConfigs": [{"type": "ONE_TO_ONE_NAT", "natIP": "198.51.100.4"}]}
		],
		"metadata": {"items": [{"key": "ssh-keys", "value": "user:ssh-rsa AAAAB3Nz secret-key-material"}]}
	}`
	got, err := extractInstance(instanceContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	blob, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal extraction: %v", err)
	}
	for _, forbidden := range []string{"10.9.9.9", "198.51.100.4", "ssh-rsa", "secret-key-material", "AAAAB3Nz"} {
		if containsString(string(blob), forbidden) {
			t.Fatalf("extraction leaked forbidden value %q: %s", forbidden, blob)
		}
	}
	// The metadata key name is safe to keep; the value is not.
	keys, _ := got.Attributes["metadata_keys"].([]string)
	if len(keys) != 1 || keys[0] != "ssh-keys" {
		t.Errorf("metadata_keys = %#v, want [ssh-keys]", got.Attributes["metadata_keys"])
	}
}

func TestExtractInstanceEmptyDataYieldsNothing(t *testing.T) {
	got, err := extractInstance(instanceContext(`{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Attributes) != 0 {
		t.Errorf("expected no attributes for empty data, got %#v", got.Attributes)
	}
	if len(got.Relationships) != 0 {
		t.Errorf("expected no relationships for empty data, got %#v", got.Relationships)
	}
	if len(got.CorrelationAnchors) != 0 {
		t.Errorf("expected no anchors for empty data, got %#v", got.CorrelationAnchors)
	}
}

func TestExtractInstanceMalformedDataErrors(t *testing.T) {
	if _, err := extractInstance(instanceContext(`{not json`)); err == nil {
		t.Fatalf("expected an error for malformed resource data")
	}
}

func TestInstanceMachineTypeName(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"https://www.googleapis.com/compute/v1/projects/p/zones/z/machineTypes/e2-standard-4", "e2-standard-4"},
		{"projects/p/zones/z/machineTypes/n1-standard-1", "n1-standard-1"},
		{"e2-micro", "e2-micro"},
		{"projects/p/zones/z", ""},
		{"", ""},
	}
	for _, tc := range cases {
		if got := computeMachineTypeName(tc.in); got != tc.want {
			t.Errorf("computeMachineTypeName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
