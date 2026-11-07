// SPDX-License-Identifier: MIT
// Copyright (c) 2026 eshu-hq

package hosted

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/playbook"
)

// TestStarterPlaybooksDrainsQueryImport pins the onboarding artifact seam
// after the #9669 row-3 drain: starter workflows project the playbook
// catalog leaf (the same catalog query.PlaybookCatalog forwards), never the
// handler package. The catalog is the single source of truth for
// first-class starter workflows, so an empty projection fails here instead
// of shipping an artifact with no starter workflows.
func TestStarterPlaybooksDrainsQueryImport(t *testing.T) {
	catalog := playbook.Catalog()
	if len(catalog) == 0 {
		t.Fatal("expected non-empty playbook catalog")
	}
	projected := StarterPlaybooks()
	if len(projected) != len(catalog) {
		t.Fatalf("projected %d starter playbooks from %d catalog entries", len(projected), len(catalog))
	}
	first := projected[0]
	if first.PlaybookID != catalog[0].ID || first.Version != catalog[0].Version {
		t.Fatalf("first starter playbook = %+v, want ID %q version %q", first, catalog[0].ID, catalog[0].Version)
	}
	if len(first.Tools) != len(catalog[0].Steps) {
		t.Fatalf("first starter playbook tools = %d, want %d steps", len(first.Tools), len(catalog[0].Steps))
	}
	if prompts := StarterPrompts(); len(prompts) == 0 {
		t.Fatal("expected non-empty starter prompts")
	}
}
