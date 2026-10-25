// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package correlation

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// A pip constraints row (#8821) limits the version a package gets if something
// else installs it, so the parser flags it central_version_declaration. These
// tests hold the PyPI row shape the parser emits: a constraint is never
// package consumption, and the same pin in requirements.txt still is.

func pipManifestFact(path, section, name, version string, extra map[string]any, observedAt time.Time) facts.Envelope {
	metadata := map[string]any{"section": section}
	for key, value := range extra {
		metadata[key] = value
	}
	envelope := packageManifestDependencyFactWithMetadata(
		"repo-python", "python-api", path, name, "pypi", version, observedAt, metadata,
	)
	envelope.FactID = "pip:" + path + ":" + name
	return envelope
}

func TestPipConstraintPinIsNotPackageConsumption(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 10, 25, 9, 0, 0, 0, time.UTC)
	base := []facts.Envelope{
		packageRegistryPackageFact("pkg:pypi/requests", "pypi", "requests", "", observedAt),
		packageSourceRepositoryFact("repo-python", "python-api", "https://example.test/python-api", false, observedAt),
	}

	constraint := append(append([]facts.Envelope{}, base...), pipManifestFact(
		"constraints.txt", "constraints", "requests", "==2.31.0",
		map[string]any{"central_version_declaration": true}, observedAt))
	if got := ExtractPackageManifestDependencies(constraint); len(got) != 0 {
		t.Fatalf("ExtractPackageManifestDependencies(constraints.txt) = %#v, want none for a version policy", got)
	}
	if got := BuildPackageConsumptionDecisions(constraint); len(got) != 0 {
		t.Fatalf("BuildPackageConsumptionDecisions(constraints.txt) = %#v, want no consumption", got)
	}

	requirement := append(append([]facts.Envelope{}, base...), pipManifestFact(
		"requirements.txt", "requirements", "requests", "==2.31.0", nil, observedAt))
	if got := ExtractPackageManifestDependencies(requirement); len(got) != 1 {
		t.Fatalf("ExtractPackageManifestDependencies(requirements.txt) = %#v, want the declared dependency", got)
	}
}
