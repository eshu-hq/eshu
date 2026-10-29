// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package nuget_test

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/nuget"
)

func TestDotNetProjectEvidenceShapes(t *testing.T) {
	for _, tc := range []struct {
		name, body, want, state string
		rejected                int
	}{
		{"exact", `<Project><PropertyGroup><TargetFramework>8.0.300</TargetFramework></PropertyGroup></Project>`, "8.0.300", "parsed_values", 0},
		{"range", `<Project><PropertyGroup><TargetFramework>&gt;=8.0</TargetFramework></PropertyGroup></Project>`, ">=8.0", "parsed_values", 0},
		{"alias", `<Project><PropertyGroup><TargetFramework>net8.0</TargetFramework></PropertyGroup></Project>`, "net8.0", "parsed_values", 0},
		{"placeholder", `<Project><PropertyGroup><TargetFramework>${TFM}</TargetFramework></PropertyGroup></Project>`, "${TFM}", "parsed_values", 0},
		{"credential", `<Project><PropertyGroup><TargetFramework>https://user:password@example.invalid</TargetFramework></PropertyGroup></Project>`, "", "parsed_empty", 1},
		{"malformed", `<Project><PropertyGroup><TargetFramework>net8.0`, "", "unavailable", 0},
		{"conditional", `<Project><PropertyGroup Condition="'$(Configuration)' == 'Debug'"><TargetFramework>net8.0</TargetFramework></PropertyGroup></Project>`, "", "parsed_empty", 0},
		{"conditional_value", `<Project><PropertyGroup><TargetFramework Condition="'$(Configuration)' == 'Debug'">net8.0</TargetFramework></PropertyGroup></Project>`, "", "parsed_empty", 0},
		{"nested_value", `<Project><PropertyGroup><TargetFramework><Value>net8.0</Value></TargetFramework></PropertyGroup></Project>`, "", "parsed_empty", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := nuget.ExtractDotNetProjectEvidence([]byte(tc.body))
			count := 0
			if tc.want != "" {
				count = 1
			}
			if e.State != tc.state || e.Rejected != tc.rejected || len(e.Values) != count {
				t.Fatalf("evidence = %+v", e)
			}
			if count == 1 && e.Values[0].Value != tc.want {
				t.Fatalf("value = %+v", e.Values)
			}
		})
	}
}
