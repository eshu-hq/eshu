// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package azurecloud

import (
	"os"
	"path/filepath"
	"testing"
)

func loadFixture(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return raw
}

func TestParseResourceGraphPage(t *testing.T) {
	page, err := ParseResourceGraphPage(loadFixture(t, "resources_page1.json"))
	if err != nil {
		t.Fatalf("ParseResourceGraphPage error: %v", err)
	}
	if page.Count != 2 {
		t.Fatalf("Count = %d, want 2", page.Count)
	}
	if page.TotalRecords != 3 {
		t.Fatalf("TotalRecords = %d, want 3", page.TotalRecords)
	}
	if page.SkipToken != "skip-token-page-2" {
		t.Fatalf("SkipToken = %q", page.SkipToken)
	}
	if page.ResultTruncated {
		t.Fatal("ResultTruncated should be false")
	}
	if len(page.Rows) != 2 {
		t.Fatalf("len(Rows) = %d, want 2", len(page.Rows))
	}
	first := page.Rows[0]
	if first.ID == "" || first.Type != "microsoft.compute/virtualmachines" {
		t.Fatalf("unexpected first row: %+v", first)
	}
	if first.SKU["tier"] != "Standard" {
		t.Fatalf("sku tier = %v", first.SKU["tier"])
	}
	if first.Tags["env"] != "prod" {
		t.Fatalf("tags env = %v", first.Tags["env"])
	}
	if !first.HasIdentity() {
		t.Fatal("first row should report a managed identity")
	}
	if page.Rows[1].HasIdentity() {
		t.Fatal("storage row should not report a managed identity")
	}
}

func TestParseResourceGraphPageTruncated(t *testing.T) {
	page, err := ParseResourceGraphPage(loadFixture(t, "resources_truncated.json"))
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if !page.ResultTruncated {
		t.Fatal("ResultTruncated should be true")
	}
}

func TestParseResourceGraphPageRejectsInvalidJSON(t *testing.T) {
	if _, err := ParseResourceGraphPage([]byte("{not json")); err == nil {
		t.Fatal("expected error for invalid json")
	}
}

func TestResourceRowSKUClass(t *testing.T) {
	page, err := ParseResourceGraphPage(loadFixture(t, "resources_page1.json"))
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if got := page.Rows[0].SKUClass(); got != "Standard" {
		t.Fatalf("SKUClass = %q, want Standard", got)
	}
	page2, err := ParseResourceGraphPage(loadFixture(t, "resources_page2.json"))
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if got := page2.Rows[0].SKUClass(); got != "" {
		t.Fatalf("SKUClass for nil sku = %q, want empty", got)
	}
}
