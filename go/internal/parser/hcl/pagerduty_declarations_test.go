// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package hcl

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

func TestPagerDutyDeclarationsRedactPrivateSourceAndUnresolvedValues(t *testing.T) {
	t.Parallel()

	filePath := writeHCLTestFile(t, "team-checkout.tf", `module "checkout_pagerduty_service" {
  source                  = "private.registry.example.com/team/pagerduty-service/aws"
  name                    = "Checkout"
  acknowledgement_timeout = try(var.pagerduty_acknowledgement_timeout, 0)
}
`)

	got, err := Parse(filePath, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	rows := bucketForTest(t, got, "pagerduty_declarations")
	service := namedItemForTest(t, rows, "module.checkout_pagerduty_service")
	if _, exists := service["module_source"]; exists {
		t.Fatalf("module_source = %#v, want private module source omitted", service["module_source"])
	}
	if got, ok := service["module_source_fingerprint"].(string); !ok || strings.TrimSpace(got) == "" {
		t.Fatalf("module_source_fingerprint = %#v, want non-empty string", service["module_source_fingerprint"])
	}
	if got, want := service["module_source_redacted"], true; got != want {
		t.Fatalf("module_source_redacted = %#v, want %#v", got, want)
	}
	if _, exists := service["acknowledgement_timeout"]; exists {
		t.Fatalf("acknowledgement_timeout = %#v, want unresolved expression omitted", service["acknowledgement_timeout"])
	}
	if got, want := service["acknowledgement_timeout_resolution"], "unresolved"; got != want {
		t.Fatalf("acknowledgement_timeout_resolution = %#v, want %#v", got, want)
	}
}
