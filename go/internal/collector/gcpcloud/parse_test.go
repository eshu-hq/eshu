// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return data
}

func TestParseAssetsListPage(t *testing.T) {
	page, err := ParseAssetsListPage(readFixture(t, "assets_list_page1.json"))
	if err != nil {
		t.Fatalf("ParseAssetsListPage: %v", err)
	}
	if page.NextPageToken != "PAGE2TOKEN" {
		t.Fatalf("NextPageToken = %q, want PAGE2TOKEN", page.NextPageToken)
	}
	if len(page.Resources) != 2 {
		t.Fatalf("len(Resources) = %d, want 2", len(page.Resources))
	}
	if page.ReadTime.IsZero() {
		t.Fatal("ReadTime is zero")
	}

	vm := page.Resources[0]
	if vm.Name != "//compute.googleapis.com/projects/my-project/zones/us-central1-a/instances/vm-1" {
		t.Fatalf("vm.Name = %q (raw full resource name must be preserved)", vm.Name)
	}
	if vm.AssetType != "compute.googleapis.com/Instance" {
		t.Fatalf("vm.AssetType = %q", vm.AssetType)
	}
	if vm.State != "RUNNING" {
		t.Fatalf("vm.State = %q, want RUNNING", vm.State)
	}
	if vm.Location != "us-central1-a" {
		t.Fatalf("vm.Location = %q", vm.Location)
	}
	if vm.Labels["env"] != "prod" {
		t.Fatalf("vm label env = %q", vm.Labels["env"])
	}
}

func TestParseAssetsListPageIAMPolicyBindings(t *testing.T) {
	page, err := ParseAssetsListPage(readFixture(t, "assets_list_iam_policy.json"))
	if err != nil {
		t.Fatalf("ParseAssetsListPage: %v", err)
	}
	if len(page.Resources) != 1 {
		t.Fatalf("len(Resources) = %d, want 1", len(page.Resources))
	}

	resource := page.Resources[0]
	if resource.Name != "//storage.googleapis.com/projects/_/buckets/iam-bucket" {
		t.Fatalf("resource.Name = %q", resource.Name)
	}
	if got := len(resource.IAMPolicyBindings); got != 3 {
		t.Fatalf("len(IAMPolicyBindings) = %d, want 3", got)
	}
	viewer := resource.IAMPolicyBindings[0]
	if viewer.Role != "roles/storage.objectViewer" {
		t.Fatalf("viewer.Role = %q", viewer.Role)
	}
	if len(viewer.Members) != 2 {
		t.Fatalf("viewer.Members = %v, want two members", viewer.Members)
	}
	if !viewer.ConditionPresent {
		t.Fatal("viewer.ConditionPresent = false, want true")
	}
	if viewer.ConditionFingerprintInput == "" {
		t.Fatal("viewer.ConditionFingerprintInput is empty")
	}
	if containsAny(viewer.ConditionFingerprintInput, "\n") {
		t.Fatalf("viewer.ConditionFingerprintInput not compact: %q", viewer.ConditionFingerprintInput)
	}
	if viewer.Etag != "etag-iam-1" {
		t.Fatalf("viewer.Etag = %q", viewer.Etag)
	}
	admin := resource.IAMPolicyBindings[1]
	if admin.ConditionPresent {
		t.Fatal("admin.ConditionPresent = true, want false")
	}
	if admin.Etag != "etag-iam-1" {
		t.Fatalf("admin.Etag = %q", admin.Etag)
	}
	if resource.Extension["iamPolicy"] != nil || resource.Extension["policy"] != nil {
		t.Fatalf("raw IAM policy leaked into extension: %#v", resource.Extension)
	}
}

func TestParseAssetsListPageServiceAccountEmail(t *testing.T) {
	page, err := ParseAssetsListPage(readFixture(t, "assets_list_page2.json"))
	if err != nil {
		t.Fatalf("ParseAssetsListPage: %v", err)
	}

	var serviceAccount ResourceObservation
	for _, resource := range page.Resources {
		if resource.AssetType == serviceAccountAssetType {
			serviceAccount = resource
			break
		}
	}
	if serviceAccount.Name == "" {
		t.Fatal("service account resource not found in fixture")
	}
	if got, want := serviceAccount.ServiceAccountEmail, "svc@my-project.iam.gserviceaccount.com"; got != want {
		t.Fatalf("ServiceAccountEmail = %q, want %q", got, want)
	}
}

func TestParseAssetsListPageDNSRecordSets(t *testing.T) {
	page, err := ParseAssetsListPage(readFixture(t, "assets_list_dns_record.json"))
	if err != nil {
		t.Fatalf("ParseAssetsListPage: %v", err)
	}
	if len(page.Resources) != 1 {
		t.Fatalf("len(Resources) = %d, want 1", len(page.Resources))
	}

	resource := page.Resources[0]
	if resource.DisplayName == "svc.internal.example." {
		t.Fatal("resource DisplayName leaked raw DNS record name")
	}
	if got := len(resource.DNSRecords); got != 1 {
		t.Fatalf("len(DNSRecords) = %d, want 1", got)
	}
	record := resource.DNSRecords[0]
	wantZone := "//dns.googleapis.com/projects/123456789/locations/global/managedZones/987654321"
	if record.ManagedZoneFullResourceName != wantZone {
		t.Fatalf("ManagedZoneFullResourceName = %q, want %q", record.ManagedZoneFullResourceName, wantZone)
	}
	if record.RecordType != "CNAME" {
		t.Fatalf("RecordType = %q, want CNAME", record.RecordType)
	}
	if record.RecordName != "svc.internal.example." {
		t.Fatalf("RecordName = %q", record.RecordName)
	}
	if len(record.Targets) != 3 {
		t.Fatalf("Targets = %#v, want three raw targets for envelope dedupe", record.Targets)
	}
	if record.TTLSeconds != 300 {
		t.Fatalf("TTLSeconds = %d, want 300", record.TTLSeconds)
	}
	if resource.Extension["raw_data"] != nil || resource.Extension["rrdatas"] != nil {
		t.Fatalf("raw DNS data leaked into extension: %#v", resource.Extension)
	}
}

func TestParseAssetsListPageRelationships(t *testing.T) {
	page, err := ParseAssetsListPage(readFixture(t, "assets_list_relationship.json"))
	if err != nil {
		t.Fatalf("ParseAssetsListPage: %v", err)
	}
	if len(page.Resources) != 1 {
		t.Fatalf("len(Resources) = %d, want 1", len(page.Resources))
	}

	resource := page.Resources[0]
	if got := len(resource.Relationships); got != 1 {
		t.Fatalf("len(Relationships) = %d, want 1", got)
	}
	rel := resource.Relationships[0]
	if rel.SourceFullResourceName != resource.Name {
		t.Fatalf("SourceFullResourceName = %q, want %q", rel.SourceFullResourceName, resource.Name)
	}
	if rel.SourceAssetType != "compute.googleapis.com/Instance" {
		t.Fatalf("SourceAssetType = %q", rel.SourceAssetType)
	}
	if rel.TargetFullResourceName != "//compute.googleapis.com/projects/my-project/zones/us-central1-a/disks/disk-rel" {
		t.Fatalf("TargetFullResourceName = %q", rel.TargetFullResourceName)
	}
	if rel.TargetAssetType != "compute.googleapis.com/Disk" {
		t.Fatalf("TargetAssetType = %q", rel.TargetAssetType)
	}
	if rel.RelationshipType != "INSTANCE_TO_DISK" {
		t.Fatalf("RelationshipType = %q", rel.RelationshipType)
	}
	if rel.SupportState != RelationshipSupportSupported {
		t.Fatalf("SupportState = %q, want supported", rel.SupportState)
	}
}

func TestParseAssetsListPageImageReferences(t *testing.T) {
	page, err := ParseAssetsListPage(readFixture(t, "assets_list_image_reference.json"))
	if err != nil {
		t.Fatalf("ParseAssetsListPage: %v", err)
	}
	if len(page.Resources) != 2 {
		t.Fatalf("len(Resources) = %d, want 2", len(page.Resources))
	}

	service := page.Resources[0]
	if service.Name != "//run.googleapis.com/projects/my-project/locations/us-central1/services/api-service" {
		t.Fatalf("service.Name = %q", service.Name)
	}
	if got := len(service.ImageReferences); got != 2 {
		t.Fatalf("len(service.ImageReferences) = %d, want 2", got)
	}
	api := service.ImageReferences[0]
	if api.OwningFullResourceName != service.Name {
		t.Fatalf("OwningFullResourceName = %q, want %q", api.OwningFullResourceName, service.Name)
	}
	if api.ContainerName != "api" {
		t.Fatalf("ContainerName = %q, want api", api.ContainerName)
	}
	if api.ImageReference != "us-docker.pkg.dev/my-project/team/api@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef" {
		t.Fatalf("ImageReference = %q", api.ImageReference)
	}
	if api.ImageDigest != "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef" {
		t.Fatalf("ImageDigest = %q", api.ImageDigest)
	}
	worker := service.ImageReferences[1]
	if worker.ImageDigest != "" {
		t.Fatalf("worker.ImageDigest = %q, want empty tag-only reference", worker.ImageDigest)
	}

	job := page.Resources[1]
	if got := len(job.ImageReferences); got != 1 {
		t.Fatalf("len(job.ImageReferences) = %d, want 1", got)
	}
	if job.ImageReferences[0].ContainerName != "batch" {
		t.Fatalf("job container = %q, want batch", job.ImageReferences[0].ContainerName)
	}
	if containsAny(stringify(service.Extension), "SAFE_RUNTIME_ENV", "runtime-value", "containers") {
		t.Fatalf("resource extension leaked runtime template data: %#v", service.Extension)
	}
}

func TestParseAssetsListRedactsDataPlane(t *testing.T) {
	page, err := ParseAssetsListPage(readFixture(t, "assets_list_page1.json"))
	if err != nil {
		t.Fatalf("ParseAssetsListPage: %v", err)
	}
	vm := page.Resources[0]
	// The startup script (with a secret), network IPs, and the raw resource data
	// blob must never survive parsing into the extension or any other field.
	for k, v := range vm.Extension {
		if containsAny(stringify(v), "hunter2", "203.0.113.7", "10.0.0.5", "startup-script", "networkInterfaces") {
			t.Fatalf("extension field %q leaked data-plane content: %v", k, v)
		}
	}
	if vm.Extension["raw_data"] != nil {
		t.Fatal("extension must not carry the raw provider resource data blob")
	}
}

func TestParseSearchAllResources(t *testing.T) {
	page, err := ParseSearchAllResourcesPage(readFixture(t, "search_all_resources.json"))
	if err != nil {
		t.Fatalf("ParseSearchAllResourcesPage: %v", err)
	}
	if len(page.Resources) != 1 {
		t.Fatalf("len(Resources) = %d, want 1", len(page.Resources))
	}
	net := page.Resources[0]
	if net.AssetType != "compute.googleapis.com/Network" {
		t.Fatalf("net.AssetType = %q", net.AssetType)
	}
	if net.State != "ACTIVE" {
		t.Fatalf("net.State = %q", net.State)
	}
	wantAncestors := []string{"projects/123456789", "folders/4455667788", "organizations/9988776655"}
	if len(net.Ancestors) != len(wantAncestors) {
		t.Fatalf("net.Ancestors = %v, want %v", net.Ancestors, wantAncestors)
	}
	for i := range wantAncestors {
		if net.Ancestors[i] != wantAncestors[i] {
			t.Fatalf("net.Ancestors[%d] = %q, want %q", i, net.Ancestors[i], wantAncestors[i])
		}
	}
}

func TestParseInvalidJSON(t *testing.T) {
	if _, err := ParseAssetsListPage([]byte("not json")); err == nil {
		t.Fatal("want error for invalid JSON")
	}
}

func TestCaiResourceStatusTolersStringAndObject(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"bare string", `"RUNNING"`, "RUNNING"},
		{"object with state", `{"state":"ERROR","detail":"x"}`, "ERROR"},
		{"object without state", `{"detail":"x"}`, ""},
		{"null", `null`, ""},
		{"number ignored", `42`, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var s caiResourceStatus
			if err := json.Unmarshal([]byte(tc.in), &s); err != nil {
				t.Fatalf("unmarshal %s: %v", tc.in, err)
			}
			if string(s) != tc.want {
				t.Errorf("caiResourceStatus(%s) = %q, want %q", tc.in, string(s), tc.want)
			}
		})
	}
}

func TestParseAssetsListPageObjectStatusPopulatesState(t *testing.T) {
	const raw = `{"assets":[{"name":"//dataproc.googleapis.com/projects/p/regions/r/clusters/c","assetType":"dataproc.googleapis.com/Cluster","resource":{"data":{"status":{"state":"RUNNING"}}}}]}`
	page, err := ParseAssetsListPage([]byte(raw))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(page.Resources) != 1 {
		t.Fatalf("want 1 resource, got %d", len(page.Resources))
	}
	if page.Resources[0].State != "RUNNING" {
		t.Errorf("object-status base State = %q, want RUNNING", page.Resources[0].State)
	}
}
