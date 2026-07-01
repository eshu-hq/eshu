// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/secretsiam"
)

const dataprocClusterFullName = "//dataproc.googleapis.com/projects/demo-project/regions/us-central1/clusters/analytics"

func dataprocClusterContext(data string) ExtractContext {
	return ExtractContext{
		FullResourceName: dataprocClusterFullName,
		AssetType:        assetTypeDataprocCluster,
		ProjectID:        "demo-project",
		Data:             json.RawMessage(data),
	}
}

func TestDataprocClusterExtractorIsRegistered(t *testing.T) {
	if _, ok := lookupAssetExtractor(assetTypeDataprocCluster); !ok {
		t.Fatalf("expected %q extractor to self-register via init()", assetTypeDataprocCluster)
	}
}

func TestExtractDataprocClusterFullResource(t *testing.T) {
	const data = `{
		"clusterName": "analytics",
		"projectId": "demo-project",
		"status": {"state": "RUNNING"},
		"labels": {"team": "data"},
		"config": {
			"configBucket": "dataproc-staging-demo",
			"gceClusterConfig": {
				"networkUri": "https://www.googleapis.com/compute/v1/projects/demo-project/global/networks/analytics-vpc",
				"subnetworkUri": "https://www.googleapis.com/compute/v1/projects/demo-project/regions/us-central1/subnetworks/analytics-subnet",
				"serviceAccount": "dataproc-runner@demo-project.iam.gserviceaccount.com",
				"internalIpOnly": true
			},
			"masterConfig": {"numInstances": 1, "machineTypeUri": "https://www.googleapis.com/compute/v1/projects/demo-project/zones/us-central1-a/machineTypes/n2-standard-4"},
			"workerConfig": {"numInstances": 4, "machineTypeUri": "https://www.googleapis.com/compute/v1/projects/demo-project/zones/us-central1-a/machineTypes/n2-standard-8"},
			"softwareConfig": {"imageVersion": "2.1-debian11"},
			"encryptionConfig": {"gcePdKmsKeyName": "projects/demo-project/locations/us-central1/keyRings/dataproc/cryptoKeys/pd"},
			"autoscalingConfig": {"policyUri": "projects/demo-project/regions/us-central1/autoscalingPolicies/analytics-ap"}
		}
	}`

	got, err := extractDataprocCluster(dataprocClusterContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantAttrs := map[string]any{
		"status_state":                "RUNNING",
		"internal_ip_only":            true,
		"master_machine_type":         "n2-standard-4",
		"master_num_instances":        1,
		"worker_machine_type":         "n2-standard-8",
		"worker_num_instances":        4,
		"image_version":               "2.1-debian11",
		"customer_managed_encryption": true,
		"autoscaling_enabled":         true,
		"service_account_fingerprint": secretsiam.GCPServiceAccountEmailDigest("dataproc-runner@demo-project.iam.gserviceaccount.com"),
	}
	if !reflect.DeepEqual(got.Attributes, wantAttrs) {
		t.Fatalf("attributes mismatch:\n got %#v\nwant %#v", got.Attributes, wantAttrs)
	}

	assertRelationship(t, got.Relationships, relationshipTypeClusterUsesNetwork,
		"//compute.googleapis.com/projects/demo-project/global/networks/analytics-vpc", assetTypeComputeNetwork)
	assertRelationship(t, got.Relationships, relationshipTypeClusterUsesSubnetwork,
		"//compute.googleapis.com/projects/demo-project/regions/us-central1/subnetworks/analytics-subnet", assetTypeComputeSubnetwork)
	assertRelationship(t, got.Relationships, relationshipTypeClusterEncryptedByKMSKey,
		"//cloudkms.googleapis.com/projects/demo-project/locations/us-central1/keyRings/dataproc/cryptoKeys/pd", assetTypeKMSCryptoKey)
	assertRelationship(t, got.Relationships, relationshipTypeClusterUsesStagingBucket,
		"//storage.googleapis.com/projects/_/buckets/dataproc-staging-demo", assetTypeStorageBucket)
	if len(got.Relationships) != 4 {
		t.Fatalf("expected network + subnet + kms + bucket edges, got %d: %#v", len(got.Relationships), got.Relationships)
	}

	// The raw service-account email must never leak — only its fingerprint.
	blob, _ := json.Marshal(got)
	if containsString(string(blob), "dataproc-runner@demo-project.iam.gserviceaccount.com") {
		t.Fatalf("dataproc extraction leaked raw service-account email: %s", blob)
	}
	rel := got.Relationships[0]
	if rel.SourceFullResourceName != dataprocClusterFullName || rel.SourceAssetType != assetTypeDataprocCluster {
		t.Errorf("relationship source = %q/%q, want dataproc identity", rel.SourceFullResourceName, rel.SourceAssetType)
	}
}

func TestExtractDataprocClusterMinimal(t *testing.T) {
	const data = `{"status": {"state": "CREATING"}, "config": {"gceClusterConfig": {"internalIpOnly": false}}}`
	got, err := extractDataprocCluster(dataprocClusterContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantAttrs := map[string]any{
		"status_state":     "CREATING",
		"internal_ip_only": false,
	}
	if !reflect.DeepEqual(got.Attributes, wantAttrs) {
		t.Fatalf("attributes mismatch:\n got %#v\nwant %#v", got.Attributes, wantAttrs)
	}
	if len(got.Relationships) != 0 {
		t.Errorf("expected no edges, got %#v", got.Relationships)
	}
}

func TestExtractDataprocClusterEmptyDataYieldsNothing(t *testing.T) {
	got, err := extractDataprocCluster(dataprocClusterContext(`{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Attributes) != 0 {
		t.Errorf("expected no attributes for empty data, got %#v", got.Attributes)
	}
	if len(got.Relationships) != 0 {
		t.Errorf("expected no relationships for empty data, got %#v", got.Relationships)
	}
}

func TestExtractDataprocClusterMalformedDataErrors(t *testing.T) {
	if _, err := extractDataprocCluster(dataprocClusterContext(`{bad`)); err == nil {
		t.Fatalf("expected an error for malformed resource data")
	}
	if _, err := extractDataprocCluster(dataprocClusterContext(``)); err == nil {
		t.Fatalf("expected an error for empty resource data")
	}
}

func TestDataprocKMSKeyFullName(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"relative key", "projects/p/locations/l/keyRings/r/cryptoKeys/k", "//cloudkms.googleapis.com/projects/p/locations/l/keyRings/r/cryptoKeys/k"},
		{"leading slash", "/projects/p/locations/l/keyRings/r/cryptoKeys/k", "//cloudkms.googleapis.com/projects/p/locations/l/keyRings/r/cryptoKeys/k"},
		{"already full name", "//cloudkms.googleapis.com/projects/p/locations/l/keyRings/r/cryptoKeys/k", "//cloudkms.googleapis.com/projects/p/locations/l/keyRings/r/cryptoKeys/k"},
		{"whitespace only", "   ", ""},
		{"blank", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := dataprocKMSKeyFullName(tc.in); got != tc.want {
				t.Errorf("dataprocKMSKeyFullName(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestExtractDataprocClusterShortNetworkAndSubnetNames(t *testing.T) {
	// Dataproc accepts bare short names for network and subnetwork; both must
	// still resolve to CAI full names and emit edges.
	const data = `{"config": {"gceClusterConfig": {"networkUri": "default", "subnetworkUri": "sub0"}}}`
	got, err := extractDataprocCluster(dataprocClusterContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertRelationship(t, got.Relationships, relationshipTypeClusterUsesNetwork,
		"//compute.googleapis.com/projects/demo-project/global/networks/default", assetTypeComputeNetwork)
	assertRelationship(t, got.Relationships, relationshipTypeClusterUsesSubnetwork,
		"//compute.googleapis.com/projects/demo-project/regions/us-central1/subnetworks/sub0", assetTypeComputeSubnetwork)
}

func TestDataprocSubnetworkFullNameBareNameNeedsRegion(t *testing.T) {
	// A bare subnet name with no known region cannot be resolved.
	if got := dataprocSubnetworkFullName("sub0", "demo-project", ""); got != "" {
		t.Errorf("bare subnet with no region = %q, want empty", got)
	}
	if got := dataprocRegionFromFullName(dataprocClusterFullName); got != "us-central1" {
		t.Errorf("region from full name = %q, want us-central1", got)
	}
}

func TestExtractDataprocClusterBlankServiceAccountOmitsFingerprint(t *testing.T) {
	const data = `{"config": {"gceClusterConfig": {"serviceAccount": ""}}}`
	got, err := extractDataprocCluster(dataprocClusterContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := got.Attributes["service_account_fingerprint"]; ok {
		t.Errorf("blank service account must not set a fingerprint: %#v", got.Attributes)
	}
}
