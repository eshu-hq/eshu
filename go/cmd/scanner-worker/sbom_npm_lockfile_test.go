// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"testing"
)

func TestNPMLockDependencyUnmarshalStringResetsPriorObject(t *testing.T) {
	t.Parallel()

	var dependency npmLockDependency
	if err := json.Unmarshal([]byte(`{
		"version": "1.2.3",
		"dev": true,
		"optional": true,
		"peer": true,
		"dependencies": {
			"nested": {"version": "4.5.6"}
		}
	}`), &dependency); err != nil {
		t.Fatalf("Unmarshal(object) error = %v, want nil", err)
	}
	if dependency.Version == "" || len(dependency.Dependencies) == 0 {
		t.Fatalf("dependency after object unmarshal = %#v, want populated fields", dependency)
	}

	if err := json.Unmarshal([]byte(`"^7.0.0"`), &dependency); err != nil {
		t.Fatalf("Unmarshal(string range) error = %v, want nil", err)
	}
	if dependency.Version != "" || dependency.Dev || dependency.Optional || dependency.Peer || len(dependency.Dependencies) != 0 {
		t.Fatalf("dependency after string range unmarshal = %#v, want zero-value dependency", dependency)
	}
}
