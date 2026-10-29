// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import "testing"

func TestIsSbtBuildPropertiesPathAcceptsLauncherPin(t *testing.T) {
	for _, tc := range []struct {
		path string
		want bool
	}{
		{"project/build.properties", true},
		{"repo/project/build.properties", true},
		{"PROJECT/BUILD.PROPERTIES", true},
		{"project/other.properties", false},
		{"build.properties", false},
		{"project/build.properties.bak", false},
	} {
		t.Run(tc.path, func(t *testing.T) {
			if got := isSbtBuildPropertiesPath(tc.path); got != tc.want {
				t.Fatalf("isSbtBuildPropertiesPath(%q) = %v, want %v", tc.path, got, tc.want)
			}
		})
	}
}
