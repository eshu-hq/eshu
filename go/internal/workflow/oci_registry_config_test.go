// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workflow

import (
	"encoding/json"
	"testing"
)

func TestValidateOCIRegistryCollectorConfigurationRoundTripsAllowEmptySuccessor(t *testing.T) {
	t.Parallel()

	raw := `{"targets":[{
		"provider":"dockerhub",
		"registry":"registry-1.docker.io",
		"repository":"library/busybox",
		"references":["latest"],
		"allow_empty_successor":true
	}]}`

	if err := ValidateOCIRegistryCollectorConfiguration(raw); err != nil {
		t.Fatalf("ValidateOCIRegistryCollectorConfiguration() error = %v, want nil", err)
	}
	var decoded ociRegistryCollectorConfiguration
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	if len(decoded.Targets) != 1 {
		t.Fatalf("len(Targets) = %d, want 1", len(decoded.Targets))
	}
	if !decoded.Targets[0].AllowEmptySuccessor {
		t.Fatal("AllowEmptySuccessor = false with allow_empty_successor:true, want true")
	}

	absent := `{"targets":[{
		"provider":"dockerhub",
		"registry":"registry-1.docker.io",
		"repository":"library/busybox",
		"references":["latest"]
	}]}`
	if err := ValidateOCIRegistryCollectorConfiguration(absent); err != nil {
		t.Fatalf("ValidateOCIRegistryCollectorConfiguration() error = %v, want nil", err)
	}
	var decodedAbsent ociRegistryCollectorConfiguration
	if err := json.Unmarshal([]byte(absent), &decodedAbsent); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	if decodedAbsent.Targets[0].AllowEmptySuccessor {
		t.Fatal("AllowEmptySuccessor = true without allow_empty_successor, want false default")
	}
}
