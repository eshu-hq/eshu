// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package correlation

import (
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/securityalert"
)

// A Directory.Packages.props PackageVersion row (#8824) is a central version
// pin: NuGet never warns about an unused one, so it does not prove that any
// project in the repository references the package. These tests hold that a
// pin never becomes package consumption or security-alert evidence, while a
// reference next to it still does.

const nugetCentralRepoID = "repo-dotnet"

func nugetCentralPinFact(name, version string, flag any, observedAt time.Time) facts.Envelope {
	envelope := packageManifestDependencyFactWithMetadata(
		nugetCentralRepoID, "dotnet-worker", "Directory.Packages.props", name, "nuget", version, observedAt,
		map[string]any{
			"section":                     "PackageVersion",
			"requested_version":           version,
			"version_evidence":            "package_reference",
			"central_version_declaration": flag,
		},
	)
	envelope.FactID = "pin:" + name
	return envelope
}

func nugetCentralReferenceFact(path, section, name string, observedAt time.Time) facts.Envelope {
	envelope := packageManifestDependencyFactWithMetadata(
		nugetCentralRepoID, "dotnet-worker", path, name, "nuget", "", observedAt,
		map[string]any{
			"section":           section,
			"version_evidence":  "missing_version",
			"partial_evidence":  true,
			"direct_dependency": true,
		},
	)
	envelope.FactID = "ref:" + path + ":" + name
	return envelope
}

func nugetCentralAlert() securityalert.ProviderSecurityAlert {
	return securityalert.ProviderSecurityAlert{
		SecurityAlertReconciliationDecision: securityalert.SecurityAlertReconciliationDecision{
			RepositoryID: nugetCentralRepoID,
			PackageID:    "pkg:nuget/newtonsoft.json",
			Ecosystem:    "nuget",
			PackageName:  "Newtonsoft.Json",
		},
	}
}

func TestNuGetCentralVersionPinIsNotPackageConsumption(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 10, 24, 9, 0, 0, 0, time.UTC)
	// The flag reaches the reducer as a JSON bool from the parser and as the
	// string "true" from a stringly decoded payload; both must exclude the row.
	for _, flag := range []any{true, "true", "TRUE"} {
		envelopes := []facts.Envelope{
			packageRegistryPackageFact("pkg:nuget/newtonsoft.json", "nuget", "Newtonsoft.Json", "", observedAt),
			packageSourceRepositoryFact(nugetCentralRepoID, "dotnet-worker", "https://example.test/dotnet-worker", false, observedAt),
			nugetCentralPinFact("Newtonsoft.Json", "13.0.3", flag, observedAt),
		}
		if got := ExtractPackageManifestDependencies(envelopes); len(got) != 0 {
			t.Fatalf("flag %#v: ExtractPackageManifestDependencies = %#v, want no dependency for a central pin", flag, got)
		}
		if got := BuildPackageConsumptionDecisions(envelopes); len(got) != 0 {
			t.Fatalf("flag %#v: BuildPackageConsumptionDecisions = %#v, want no consumption for a central pin", flag, got)
		}
		alerts := []securityalert.ProviderSecurityAlert{nugetCentralAlert()}
		if got := ExtractSecurityAlertManifestConsumptions(alerts, envelopes); len(got) != 0 {
			t.Fatalf("flag %#v: ExtractSecurityAlertManifestConsumptions = %#v, want no alert evidence from a central pin", flag, got)
		}
	}
}

func TestNuGetCentralVersionPinKeepsReferenceConsumption(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 10, 24, 9, 5, 0, 0, time.UTC)
	envelopes := []facts.Envelope{
		packageRegistryPackageFact("pkg:nuget/newtonsoft.json", "nuget", "Newtonsoft.Json", "", observedAt),
		packageRegistryPackageFact("pkg:nuget/serilog", "nuget", "Serilog", "", observedAt),
		packageSourceRepositoryFact(nugetCentralRepoID, "dotnet-worker", "https://example.test/dotnet-worker", false, observedAt),
		nugetCentralPinFact("Newtonsoft.Json", "13.0.3", true, observedAt),
		nugetCentralPinFact("Serilog", "4.0.0", true, observedAt),
		nugetCentralReferenceFact("src/Worker/Worker.csproj", "PackageReference", "Newtonsoft.Json", observedAt),
		nugetCentralReferenceFact("Directory.Packages.props", "GlobalPackageReference", "Serilog", observedAt),
	}

	decisions := BuildPackageConsumptionDecisions(envelopes)
	if got, want := len(decisions), 2; got != want {
		t.Fatalf("len(decisions) = %d, want %d: %#v", got, want, decisions)
	}
	for _, decision := range decisions {
		if decision.ManifestSection == "PackageVersion" {
			t.Fatalf("decision from a central pin: %#v", decision)
		}
		if len(decision.EvidenceFactIDs) != 1 || !strings.HasPrefix(decision.EvidenceFactIDs[0], "ref:") {
			t.Fatalf("EvidenceFactIDs = %#v, want only the reference fact", decision.EvidenceFactIDs)
		}
	}

	consumptions := ExtractSecurityAlertManifestConsumptions(
		[]securityalert.ProviderSecurityAlert{nugetCentralAlert()}, envelopes,
	)
	if got, want := len(consumptions), 1; got != want {
		t.Fatalf("len(alert consumptions) = %d, want %d: %#v", got, want, consumptions)
	}
	if got, want := consumptions[0].FactID, "ref:src/Worker/Worker.csproj:Newtonsoft.Json"; got != want {
		t.Fatalf("alert consumption FactID = %q, want the project reference %q", got, want)
	}
}

// A PackageVersion row without the flag, or with it false, is outside the
// central-pin contract and keeps today's behavior: this test pins the filter
// to the flag, not to the section name.
func TestNuGetCentralVersionFilterKeysOnTheFlag(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 10, 24, 9, 10, 0, 0, time.UTC)
	for _, flag := range []any{false, "false", ""} {
		envelopes := []facts.Envelope{
			packageRegistryPackageFact("pkg:nuget/newtonsoft.json", "nuget", "Newtonsoft.Json", "", observedAt),
			nugetCentralPinFact("Newtonsoft.Json", "13.0.3", flag, observedAt),
		}
		if got := len(BuildPackageConsumptionDecisions(envelopes)); got != 1 {
			t.Fatalf("flag %#v: len(decisions) = %d, want 1", flag, got)
		}
	}
}
