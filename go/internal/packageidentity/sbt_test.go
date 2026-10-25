// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package packageidentity

import (
	"reflect"
	"testing"
)

func keyNames(keys []ConsumptionKey) []string {
	names := make([]string, 0, len(keys))
	for _, key := range keys {
		names = append(names, string(key.Ecosystem)+"|"+key.PackageName)
	}
	return names
}

func TestManifestConsumptionKeysSBTJoinRules(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input ManifestKeyInput
		want  []ConsumptionKey
	}{
		{
			name: "percent row joins the exact maven coordinate",
			input: ManifestKeyInput{
				PackageManager: "sbt", Names: []string{"org.slf4j:slf4j-api"}, SBTCrossVersion: "none",
			},
			want: ConsumptionKeys("maven", "org.slf4j:slf4j-api"),
		},
		{
			name: "double percent row with a Scala 2.13 artifact joins only the suffixed artifact",
			input: ManifestKeyInput{
				PackageManager: "sbt", Names: []string{"org.typelevel:cats-core"},
				SBTCrossVersion: "binary", SBTBinaryArtifact: "org.typelevel:cats-core_2.13",
			},
			want: ConsumptionKeys("maven", "org.typelevel:cats-core_2.13"),
		},
		{
			name: "double percent row with a Scala 3 artifact joins only the _3 artifact",
			input: ManifestKeyInput{
				PackageManager: "sbt", Names: []string{"org.typelevel:cats-core"},
				SBTCrossVersion: "binary", SBTBinaryArtifact: "org.typelevel:cats-core_3",
			},
			want: ConsumptionKeys("maven", "org.typelevel:cats-core_3"),
		},
		{
			name: "double percent row without a known binary version joins nothing",
			input: ManifestKeyInput{
				PackageManager: "sbt", Names: []string{"org.typelevel:cats-core"}, SBTCrossVersion: "binary",
			},
		},
		{
			name: "triple percent platform row joins nothing",
			input: ManifestKeyInput{
				PackageManager: "sbt", Names: []string{"org.scala-js:scalajs-dom"},
				SBTCrossVersion: "scalajs", SBTBinaryArtifact: "org.scala-js:scalajs-dom_2.13",
			},
		},
		{
			name: "custom cross-version row joins nothing",
			input: ManifestKeyInput{
				PackageManager: "sbt", Names: []string{"com.lihaoyi:ammonite"}, SBTCrossVersion: "custom",
			},
		},
		{
			name:  "row without a cross-version label joins nothing",
			input: ManifestKeyInput{PackageManager: "sbt", Names: []string{"org.slf4j:slf4j-api"}},
		},
		{
			name: "a binary artifact on a percent row is ignored",
			input: ManifestKeyInput{
				PackageManager: "sbt", Names: []string{"org.slf4j:slf4j-api"},
				SBTCrossVersion: "none", SBTBinaryArtifact: "org.slf4j:slf4j-api_2.13",
			},
			want: ConsumptionKeys("maven", "org.slf4j:slf4j-api"),
		},
		{
			name: "package manager match is case and space insensitive",
			input: ManifestKeyInput{
				PackageManager: " SBT ", Names: []string{"a:b"}, SBTCrossVersion: "binary", SBTBinaryArtifact: "a:b_3",
			},
			want: ConsumptionKeys("maven", "a:b_3"),
		},
		{
			name: "gradle row ignores sbt fields and keeps the maven join",
			input: ManifestKeyInput{
				PackageManager: "gradle", Names: []string{"a:b"}, SBTCrossVersion: "binary", SBTBinaryArtifact: "a:b_3",
			},
			want: ConsumptionKeys("gradle", "a:b"),
		},
		{
			name:  "npm row is unchanged",
			input: ManifestKeyInput{PackageManager: "npm", Names: []string{"Left-Pad"}},
			want:  ConsumptionKeys("npm", "Left-Pad"),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got := ManifestConsumptionKeys(test.input)
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("keys = %v, want %v", keyNames(got), keyNames(test.want))
			}
		})
	}
}

func TestManifestConsumptionKeysSBTNeverJoinsUnsuffixedName(t *testing.T) {
	t.Parallel()

	suffixed := ManifestConsumptionKeys(ManifestKeyInput{
		PackageManager: "sbt", Names: []string{"org.typelevel:cats-core"},
		SBTCrossVersion: "binary", SBTBinaryArtifact: "org.typelevel:cats-core_2.13",
	})
	unsuffixed := make(map[ConsumptionKey]struct{})
	for _, key := range ConsumptionKeys("maven", "org.typelevel:cats-core") {
		unsuffixed[key] = struct{}{}
	}
	for _, key := range suffixed {
		if _, ok := unsuffixed[key]; ok {
			t.Fatalf("suffixed sbt row produced the unsuffixed key %v", key)
		}
	}
}

func TestNormalizeEcosystemKeepsSBTUnaliased(t *testing.T) {
	t.Parallel()

	if got := NormalizeEcosystem("sbt"); got != "" {
		t.Fatalf("NormalizeEcosystem(sbt) = %q, want empty: sbt joins through ManifestConsumptionKeys only", got)
	}
}

func TestManifestPackageManagersForEcosystem(t *testing.T) {
	t.Parallel()

	if got, want := ManifestPackageManagersForEcosystem("maven"), []string{"gradle", "maven", "sbt"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("maven managers = %v, want %v", got, want)
	}
	if got, want := ManifestPackageManagersForEcosystem("npm"), []string{"npm"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("npm managers = %v, want %v", got, want)
	}
}
