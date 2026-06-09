package gcpcloud

import (
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
