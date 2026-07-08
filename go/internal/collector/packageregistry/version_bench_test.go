// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package packageregistry

import (
	"testing"
	"time"
)

var benchmarkEnvelopeSink any

func BenchmarkNewPackageVersionEnvelope(b *testing.B) {
	observation := PackageVersionObservation{
		Package: PackageIdentity{
			Ecosystem: EcosystemMaven,
			Registry:  "https://repo.maven.apache.org/maven2/",
			Namespace: "org.apache.maven",
			RawName:   "maven-core",
		},
		Version:             "3.9.9",
		ScopeID:             "scope:package:maven-central",
		GenerationID:        "generation:package:maven-central:001",
		CollectorInstanceID: "maven-central",
		FencingToken:        7,
		ObservedAt:          time.Date(2026, 7, 8, 2, 0, 0, 0, time.UTC),
		PublishedAt:         time.Date(2026, 5, 11, 9, 15, 0, 0, time.UTC),
		Deprecated:          true,
		ArtifactURLs: []string{
			"https://repo.maven.apache.org/maven2/org/apache/maven/maven-core/3.9.9/maven-core-3.9.9.jar",
		},
		Checksums: map[string]string{
			"sha1": "0123456789abcdef",
		},
		SourceURI: "https://repo.maven.apache.org/maven2/org/apache/maven/maven-core/maven-metadata.xml",
	}

	b.ReportAllocs()
	for b.Loop() {
		envelope, err := NewPackageVersionEnvelope(observation)
		if err != nil {
			b.Fatalf("NewPackageVersionEnvelope() error = %v", err)
		}
		benchmarkEnvelopeSink = envelope
	}
}
