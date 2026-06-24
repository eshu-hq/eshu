// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workflow

import (
	"strings"
	"testing"
)

func TestValidateTerraformStateCollectorConfigurationNoDiscoveryMentionsBackendFilters(t *testing.T) {
	t.Parallel()

	err := ValidateTerraformStateCollectorConfiguration(`{"discovery": {}}`)
	if err == nil {
		t.Fatal("ValidateTerraformStateCollectorConfiguration() error = nil, want discovery mode error")
	}
	if !strings.Contains(err.Error(), "backend_filters") {
		t.Fatalf("error = %q, want backend_filters mentioned", err.Error())
	}
}
