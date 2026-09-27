// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package packageidentity

import (
	"reflect"
	"testing"
)

func TestConsumptionKeysUseManifestCorrelationNormalization(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		ecosystem string
		names     []string
		want      []ConsumptionKey
	}{
		{
			name:      "pypi retains normalized and source lowercase candidates",
			ecosystem: "python",
			names:     []string{"Friendly_Bard...Plugin"},
			want: []ConsumptionKey{
				{Ecosystem: EcosystemPyPI, PackageName: "friendly-bard-plugin"},
				{Ecosystem: EcosystemPyPI, PackageName: "friendly_bard...plugin"},
			},
		},
		{
			name:      "maven accepts colon and slash namespace forms",
			ecosystem: "maven",
			names:     []string{"org.apache.maven:maven-core", "org.apache.maven/maven-core"},
			want: []ConsumptionKey{
				{Ecosystem: EcosystemMaven, PackageName: "org.apache.maven/maven-core"},
				{Ecosystem: EcosystemMaven, PackageName: "org.apache.maven:maven-core"},
			},
		},
		{
			name:      "unknown ecosystem fails closed",
			ecosystem: "made-up",
			names:     []string{"package"},
			want:      nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if got := ConsumptionKeys(test.ecosystem, test.names...); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("ConsumptionKeys(%q, %q) = %#v, want %#v", test.ecosystem, test.names, got, test.want)
			}
		})
	}
}
