// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workflow

import (
	"encoding/json"
	"testing"
)

func TestValidateTerraformStateCollectorConfigurationRoundTripsAllowEmptySuccessor(t *testing.T) {
	t.Parallel()

	withAttestation := func(attested string) string {
		key := ""
		if attested != "" {
			key = `"allow_empty_successor":` + attested + ","
		}
		return `{` + key + `"discovery":{"seeds":[{
			"kind": "local",
			"path": "/tmp/prod.tfstate",
			"repo_id": "platform-infra"
		}]}}`
	}

	raw := withAttestation("true")
	if err := ValidateTerraformStateCollectorConfiguration(raw); err != nil {
		t.Fatalf("ValidateTerraformStateCollectorConfiguration() error = %v, want nil", err)
	}
	var decoded terraformStateCollectorConfiguration
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	if !decoded.AllowEmptySuccessor {
		t.Fatal("AllowEmptySuccessor = false with allow_empty_successor:true, want true")
	}

	absent := withAttestation("")
	if err := ValidateTerraformStateCollectorConfiguration(absent); err != nil {
		t.Fatalf("ValidateTerraformStateCollectorConfiguration() error = %v, want nil", err)
	}
	var decodedAbsent terraformStateCollectorConfiguration
	if err := json.Unmarshal([]byte(absent), &decodedAbsent); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	if decodedAbsent.AllowEmptySuccessor {
		t.Fatal("AllowEmptySuccessor = true without allow_empty_successor, want false default")
	}
}
