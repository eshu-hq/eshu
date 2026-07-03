// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/secretsiam"
)

const dataflowJobFullName = "//dataflow.googleapis.com/projects/demo-project/locations/us-central1/jobs/analytics-job"

func dataflowJobContext(data string) ExtractContext {
	return ExtractContext{
		FullResourceName: dataflowJobFullName,
		AssetType:        assetTypeDataflowJob,
		ProjectID:        "demo-project",
		Data:             json.RawMessage(data),
	}
}

func TestDataflowJobExtractorIsRegistered(t *testing.T) {
	if _, ok := lookupAssetExtractor(assetTypeDataflowJob); !ok {
		t.Fatalf("expected %q extractor to self-register via init()", assetTypeDataflowJob)
	}
}

func TestExtractDataflowJobFullResource(t *testing.T) {
	const data = `{
		"type": "JOB_TYPE_STREAMING",
		"currentState": "JOB_STATE_RUNNING",
		"location": "us-central1",
		"createTime": "2026-06-01T12:00:00.000000Z",
		"startTime": "2026-06-01T12:05:00.000000Z",
		"jobMetadata": {"sdkVersion": {"version": "2.58.1", "sdkSupportStatus": "SUPPORTED"}},
		"environment": {
			"serviceAccountEmail": "dataflow-runner@demo-project.iam.gserviceaccount.com",
			"serviceKmsKeyName": "projects/demo-project/locations/us-central1/keyRings/df/cryptoKeys/state",
			"tempStoragePrefix": "gs://dataflow-staging-demo/temp",
			"workerPools": [
				{
					"network": "https://www.googleapis.com/compute/v1/projects/demo-project/global/networks/analytics-vpc",
					"subnetwork": "https://www.googleapis.com/compute/v1/projects/demo-project/regions/us-central1/subnetworks/analytics-subnet",
					"zone": "us-central1-a"
				}
			]
		}
	}`

	got, err := extractDataflowJob(dataflowJobContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantAttrs := map[string]any{
		"job_type":                    "JOB_TYPE_STREAMING",
		"current_state":               "JOB_STATE_RUNNING",
		"location":                    "us-central1",
		"creation_time":               "2026-06-01T12:00:00Z",
		"start_time":                  "2026-06-01T12:05:00Z",
		"sdk_version":                 "2.58.1",
		"sdk_support_status":          "SUPPORTED",
		"service_kms_key_name":        "projects/demo-project/locations/us-central1/keyRings/df/cryptoKeys/state",
		"service_account_fingerprint": secretsiam.GCPServiceAccountEmailDigest("dataflow-runner@demo-project.iam.gserviceaccount.com"),
	}
	if !reflect.DeepEqual(got.Attributes, wantAttrs) {
		t.Fatalf("attributes mismatch:\n got %#v\nwant %#v", got.Attributes, wantAttrs)
	}

	assertRelationship(t, got.Relationships, relationshipTypeDataflowJobEncryptedByKMSKey,
		"//cloudkms.googleapis.com/projects/demo-project/locations/us-central1/keyRings/df/cryptoKeys/state", assetTypeKMSCryptoKey)
	assertRelationship(t, got.Relationships, relationshipTypeDataflowJobUsesNetwork,
		"//compute.googleapis.com/projects/demo-project/global/networks/analytics-vpc", assetTypeComputeNetwork)
	assertRelationship(t, got.Relationships, relationshipTypeDataflowJobUsesSubnetwork,
		"//compute.googleapis.com/projects/demo-project/regions/us-central1/subnetworks/analytics-subnet", assetTypeComputeSubnetwork)
	assertRelationship(t, got.Relationships, relationshipTypeDataflowJobUsesStagingBucket,
		"//storage.googleapis.com/projects/_/buckets/dataflow-staging-demo", assetTypeStorageBucket)
	if len(got.Relationships) != 4 {
		t.Fatalf("expected kms + network + subnet + staging bucket edges, got %d: %#v", len(got.Relationships), got.Relationships)
	}

	wantAnchors := []string{
		secretsiam.GCPServiceAccountEmailDigest("dataflow-runner@demo-project.iam.gserviceaccount.com"),
		"//cloudkms.googleapis.com/projects/demo-project/locations/us-central1/keyRings/df/cryptoKeys/state",
		"//compute.googleapis.com/projects/demo-project/global/networks/analytics-vpc",
		"//compute.googleapis.com/projects/demo-project/regions/us-central1/subnetworks/analytics-subnet",
		"//storage.googleapis.com/projects/_/buckets/dataflow-staging-demo",
	}
	if !reflect.DeepEqual(got.CorrelationAnchors, wantAnchors) {
		t.Fatalf("anchors mismatch:\n got %#v\nwant %#v", got.CorrelationAnchors, wantAnchors)
	}

	for _, rel := range got.Relationships {
		if rel.SourceFullResourceName != dataflowJobFullName {
			t.Errorf("relationship source = %q, want job full name", rel.SourceFullResourceName)
		}
		if rel.SourceAssetType != assetTypeDataflowJob {
			t.Errorf("relationship source asset type = %q, want %q", rel.SourceAssetType, assetTypeDataflowJob)
		}
	}

	// The raw service-account email, temp storage URI, and object path must
	// never leak — only the fingerprint and the resolved bucket identity.
	blob, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal extraction: %v", err)
	}
	for _, banned := range []string{"dataflow-runner@demo-project.iam.gserviceaccount.com", "gs://", "/temp"} {
		if containsString(string(blob), banned) {
			t.Fatalf("dataflow job extraction leaked token %q: %s", banned, blob)
		}
	}
}

func TestExtractDataflowJobBatchJobMinimal(t *testing.T) {
	const data = `{"type": "JOB_TYPE_BATCH", "currentState": "JOB_STATE_DONE"}`
	got, err := extractDataflowJob(dataflowJobContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantAttrs := map[string]any{
		"job_type":      "JOB_TYPE_BATCH",
		"current_state": "JOB_STATE_DONE",
	}
	if !reflect.DeepEqual(got.Attributes, wantAttrs) {
		t.Fatalf("attributes mismatch:\n got %#v\nwant %#v", got.Attributes, wantAttrs)
	}
	if len(got.Relationships) != 0 {
		t.Errorf("expected no edges without an environment block, got %#v", got.Relationships)
	}
	if len(got.CorrelationAnchors) != 0 {
		t.Errorf("expected no anchors without an environment block, got %#v", got.CorrelationAnchors)
	}
}

func TestExtractDataflowJobEmptyDataYieldsNothing(t *testing.T) {
	got, err := extractDataflowJob(dataflowJobContext(`{}`))
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

func TestExtractDataflowJobMalformedDataErrors(t *testing.T) {
	if _, err := extractDataflowJob(dataflowJobContext(`{bad`)); err == nil {
		t.Fatalf("expected an error for malformed resource data")
	}
	if _, err := extractDataflowJob(dataflowJobContext(``)); err == nil {
		t.Fatalf("expected an error for empty resource data")
	}
}

func TestExtractDataflowJobBlankServiceAccountOmitsFingerprint(t *testing.T) {
	const data = `{"environment": {"serviceAccountEmail": ""}}`
	got, err := extractDataflowJob(dataflowJobContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := got.Attributes["service_account_fingerprint"]; ok {
		t.Errorf("blank service account must not set a fingerprint: %#v", got.Attributes)
	}
	if len(got.CorrelationAnchors) != 0 {
		t.Errorf("blank service account must not anchor, got %#v", got.CorrelationAnchors)
	}
}

func TestExtractDataflowJobShortNetworkAndSubnetworkNames(t *testing.T) {
	// Dataflow accepts bare short names for network/subnetwork within a worker
	// pool entry; both must still resolve to CAI full names and emit edges,
	// with the subnetwork region derived from the pool's own zone.
	const data = `{
		"environment": {
			"workerPools": [
				{"network": "default", "subnetwork": "sub0", "zone": "us-east1-b"}
			]
		}
	}`
	got, err := extractDataflowJob(dataflowJobContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertRelationship(t, got.Relationships, relationshipTypeDataflowJobUsesNetwork,
		"//compute.googleapis.com/projects/demo-project/global/networks/default", assetTypeComputeNetwork)
	assertRelationship(t, got.Relationships, relationshipTypeDataflowJobUsesSubnetwork,
		"//compute.googleapis.com/projects/demo-project/regions/us-east1/subnetworks/sub0", assetTypeComputeSubnetwork)
}

func TestExtractDataflowJobSubnetworkBareNameNeedsZone(t *testing.T) {
	// A bare subnetwork short name with no resolvable zone cannot be resolved,
	// so no edge is fabricated.
	const data = `{
		"environment": {
			"workerPools": [
				{"subnetwork": "sub0"}
			]
		}
	}`
	got, err := extractDataflowJob(dataflowJobContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, rel := range got.Relationships {
		if rel.RelationshipType == relationshipTypeDataflowJobUsesSubnetwork {
			t.Fatalf("expected no subnetwork edge without a resolvable zone, got %#v", rel)
		}
	}
}

func TestExtractDataflowJobFirstWorkerPoolWithReferenceWins(t *testing.T) {
	// Multiple worker pools: the first pool that reports a network/subnetwork
	// reference is used, mirroring "one effective network across pools".
	const data = `{
		"environment": {
			"workerPools": [
				{"zone": "us-central1-a"},
				{"network": "default", "subnetwork": "sub0", "zone": "us-central1-a"}
			]
		}
	}`
	got, err := extractDataflowJob(dataflowJobContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertRelationship(t, got.Relationships, relationshipTypeDataflowJobUsesNetwork,
		"//compute.googleapis.com/projects/demo-project/global/networks/default", assetTypeComputeNetwork)
	assertRelationship(t, got.Relationships, relationshipTypeDataflowJobUsesSubnetwork,
		"//compute.googleapis.com/projects/demo-project/regions/us-central1/subnetworks/sub0", assetTypeComputeSubnetwork)
}

func TestExtractDataflowJobDoesNotCrossLatchNetworkAcrossPools(t *testing.T) {
	// pool[0] carries only a network; pool[1] only a subnetwork. Because both
	// endpoints are resolved from the SAME single pool (the first that reports
	// either), the extractor must emit only pool[0]'s network edge and never
	// fabricate a network/subnetwork pairing that never co-occurred on any real
	// worker pool.
	const data = `{
		"environment": {
			"workerPools": [
				{"network": "net-a"},
				{"subnetwork": "sub-b", "zone": "us-central1-a"}
			]
		}
	}`
	got, err := extractDataflowJob(dataflowJobContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertRelationship(t, got.Relationships, relationshipTypeDataflowJobUsesNetwork,
		"//compute.googleapis.com/projects/demo-project/global/networks/net-a", assetTypeComputeNetwork)
	for _, rel := range got.Relationships {
		if rel.RelationshipType == relationshipTypeDataflowJobUsesSubnetwork {
			t.Fatalf("must not cross-latch a subnetwork from a different pool, got %#v", rel)
		}
	}
	netEdges := 0
	for _, rel := range got.Relationships {
		if rel.RelationshipType == relationshipTypeDataflowJobUsesNetwork {
			netEdges++
		}
	}
	if netEdges != 1 {
		t.Fatalf("expected exactly one network edge from the first pool, got %d: %#v", netEdges, got.Relationships)
	}
}

func TestExtractDataflowJobStagingBucketURLForms(t *testing.T) {
	// The Dataflow API documents tempStoragePrefix resource forms as
	// storage.googleapis.com/{bucket}/{object} and
	// {bucket}.storage.googleapis.com/{object}; gs://bucket/object is accepted
	// defensively. Every form must resolve to the same bucket edge and drop the
	// object path.
	const want = "//storage.googleapis.com/projects/_/buckets/staging-demo"
	cases := []struct {
		name   string
		prefix string
	}{
		{"gs scheme", "gs://staging-demo/tmp/artifacts"},
		{"path style", "storage.googleapis.com/staging-demo/tmp/artifacts"},
		{"path style https", "https://storage.googleapis.com/staging-demo/tmp/artifacts"},
		{"virtual hosted", "staging-demo.storage.googleapis.com/tmp/artifacts"},
		{"virtual hosted https", "https://staging-demo.storage.googleapis.com/tmp/artifacts"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data := `{"environment": {"tempStoragePrefix": "` + tc.prefix + `"}}`
			got, err := extractDataflowJob(dataflowJobContext(data))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			assertRelationship(t, got.Relationships, relationshipTypeDataflowJobUsesStagingBucket, want, assetTypeStorageBucket)
			// The object path must never survive into any anchor or edge.
			blob, err := json.Marshal(got)
			if err != nil {
				t.Fatalf("marshal extraction: %v", err)
			}
			for _, banned := range []string{"tmp/artifacts", "/tmp", "gs://", "storage.googleapis.com/staging-demo"} {
				if containsString(string(blob), banned) {
					t.Fatalf("form %q leaked token %q: %s", tc.prefix, banned, blob)
				}
			}
		})
	}
}

func TestDataflowStagingBucketUnsupportedFormYieldsEmpty(t *testing.T) {
	for _, in := range []string{"", "   ", "http://example.com/x", "/local/path", "s3://bucket/key"} {
		if got := dataflowStagingBucket(in); got != "" {
			t.Errorf("dataflowStagingBucket(%q) = %q, want empty", in, got)
		}
	}
}

func TestDataflowRegionFromZone(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"valid zone", "us-central1-a", "us-central1"},
		{"valid zone multi-digit", "europe-west4-b", "europe-west4"},
		{"bare region no zone suffix", "us-central1", ""},
		{"blank", "", ""},
		{"whitespace only", "   ", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := dataflowRegionFromZone(tc.in); got != tc.want {
				t.Errorf("dataflowRegionFromZone(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestDataflowJobKMSKeyFullName(t *testing.T) {
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
			if got := dataflowJobKMSKeyFullName(tc.in); got != tc.want {
				t.Errorf("dataflowJobKMSKeyFullName(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestExtractDataflowJobNoCMEKOmitsEdge(t *testing.T) {
	// An environment with no serviceKmsKeyName must not fabricate a CMEK edge,
	// attribute, or anchor.
	const data = `{"environment": {"serviceAccountEmail": "df@demo-project.iam.gserviceaccount.com"}}`
	got, err := extractDataflowJob(dataflowJobContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := got.Attributes["service_kms_key_name"]; ok {
		t.Errorf("absent CMEK must not set service_kms_key_name: %#v", got.Attributes)
	}
	for _, rel := range got.Relationships {
		if rel.RelationshipType == relationshipTypeDataflowJobEncryptedByKMSKey {
			t.Fatalf("expected no CMEK edge without serviceKmsKeyName, got %#v", rel)
		}
	}
}

func TestExtractDataflowJobSdkSupportStatusOmittedWhenAbsent(t *testing.T) {
	// A jobMetadata.sdkVersion without sdkSupportStatus keeps only sdk_version;
	// no fabricated support-status attribute.
	const data = `{"jobMetadata": {"sdkVersion": {"version": "2.60.0"}}}`
	got, err := extractDataflowJob(dataflowJobContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Attributes["sdk_version"] != "2.60.0" {
		t.Errorf("sdk_version = %v, want 2.60.0", got.Attributes["sdk_version"])
	}
	if _, ok := got.Attributes["sdk_support_status"]; ok {
		t.Errorf("absent sdkSupportStatus must not set sdk_support_status: %#v", got.Attributes)
	}
}
