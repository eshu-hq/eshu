// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package packageidentity

import (
	"reflect"
	"testing"
)

// Maven and Gradle manifests store entity_name as the case-preserved
// "group:artifact" coordinate, so the name set a reducer sends to the exact
// entity_name predicate must contain that exact form for every shape an
// advisory can name a Maven package in (issue #9286).
func TestManifestEntityNameCandidatesMavenColonCoordinate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		ecosystem Ecosystem
		input     string
		want      []string
	}{
		{
			name:      "slash purl form gains the colon coordinate",
			ecosystem: EcosystemMaven,
			input:     "com.google.code.gson/gson",
			want:      []string{"com.google.code.gson/gson", "com.google.code.gson:gson"},
		},
		{
			name:      "colon coordinate stays and keeps its slash join form",
			ecosystem: EcosystemMaven,
			input:     "com.google.code.gson:gson",
			want:      []string{"com.google.code.gson/gson", "com.google.code.gson:gson"},
		},
		{
			name:      "mixed case keeps the stored case and adds the lowercase form",
			ecosystem: EcosystemMaven,
			input:     "org.Example/Core-API",
			want: []string{
				"org.Example/Core-API", "org.Example:Core-API",
				"org.example/core-api", "org.example:core-api",
			},
		},
		{
			name:      "artifact-only name has no group to derive",
			ecosystem: EcosystemMaven,
			input:     "gson",
			want:      []string{"gson"},
		},
		{
			name:      "registry path segment is not a group",
			ecosystem: EcosystemMaven,
			input:     "maven2/com.google.code.gson:gson",
			want:      []string{"maven2/com.google.code.gson/gson", "maven2/com.google.code.gson:gson"},
		},
		{
			name:      "other ecosystems are unchanged",
			ecosystem: EcosystemNPM,
			input:     "@scope/pkg",
			want:      []string{"@scope/pkg"},
		},
		{
			name:      "blank yields nothing",
			ecosystem: EcosystemMaven,
			input:     "  ",
			want:      nil,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got := ManifestEntityNameCandidates(test.ecosystem, test.input)
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("ManifestEntityNameCandidates(%q, %q) = %#v, want %#v", test.ecosystem, test.input, got, test.want)
			}
		})
	}
}
