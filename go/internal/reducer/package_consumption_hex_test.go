// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	elixirparser "github.com/eshu-hq/eshu/go/internal/parser/elixir"
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

func TestBuildPackageConsumptionDecisionsAdmitsParsedMixLockExactVersion(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 6, 1, 14, 30, 0, 0, time.UTC)
	rows := parseMixRowsForTest(t, "mix.lock", `%{
  "phoenix_html": {:hex, :phoenix_html, "4.2.1", "checksum", [:mix], [{:plug, "~> 1.5", [hex: :plug, repo: "hexpm", optional: false]}], "hexpm", "outer"}
}
`)
	envelopes := []facts.Envelope{
		packageRegistryPackageFact("hex://repo.hex.pm/phoenix_html", "hex", "phoenix_html", "", observedAt),
		packageRegistryPackageFact("hex://repo.hex.pm/plug", "hex", "plug", "", observedAt),
		packageSourceRepositoryFact("repo-elixir", "api", "https://github.com/acme/api", false, observedAt),
		mixContentEntityEnvelope(t, "repo-elixir", "api", "mix.lock", "phoenix_html", rows, observedAt),
		mixContentEntityEnvelope(t, "repo-elixir", "api", "mix.lock", "plug", rows, observedAt),
	}

	decisions := BuildPackageConsumptionDecisions(envelopes)
	if got, want := len(decisions), 2; got != want {
		t.Fatalf("len(decisions) = %d, want %d: %#v", got, want, decisions)
	}
	byPackage := map[string]PackageConsumptionDecision{}
	for _, decision := range decisions {
		byPackage[decision.PackageID] = decision
	}

	phoenix := byPackage["hex://repo.hex.pm/phoenix_html"]
	if got, want := phoenix.DependencyRange, "4.2.1"; got != want {
		t.Fatalf("phoenix_html DependencyRange = %q, want %q", got, want)
	}
	if !phoenix.Lockfile {
		t.Fatalf("phoenix_html Lockfile = false, want true")
	}
	if phoenix.DirectDependency == nil || !*phoenix.DirectDependency {
		t.Fatalf("phoenix_html DirectDependency = %#v, want true", phoenix.DirectDependency)
	}
	if !reflect.DeepEqual(phoenix.DependencyPath, []string{"phoenix_html"}) {
		t.Fatalf("phoenix_html DependencyPath = %#v, want direct Mix path", phoenix.DependencyPath)
	}

	plug := byPackage["hex://repo.hex.pm/plug"]
	if got, want := plug.DependencyRange, "~> 1.5"; got != want {
		t.Fatalf("plug DependencyRange = %q, want %q", got, want)
	}
	if plug.DirectDependency == nil || *plug.DirectDependency {
		t.Fatalf("plug DirectDependency = %#v, want false for nested Mix dependency", plug.DirectDependency)
	}
	if !reflect.DeepEqual(plug.DependencyPath, []string{"phoenix_html", "plug"}) {
		t.Fatalf("plug DependencyPath = %#v, want nested Mix path", plug.DependencyPath)
	}
}

func TestBuildPackageConsumptionDecisionsRejectsParsedMixGitDependency(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 6, 1, 14, 35, 0, 0, time.UTC)
	rows := parseMixRowsForTest(t, "mix.exs", `defmodule Demo.MixProject do
  use Mix.Project

  defp deps do
    [
      {:cowboy, github: "ninenines/cowboy"}
    ]
  end
end
`)
	envelopes := []facts.Envelope{
		packageRegistryPackageFact("hex://repo.hex.pm/cowboy", "hex", "cowboy", "", observedAt),
		packageSourceRepositoryFact("repo-elixir", "api", "https://github.com/acme/api", false, observedAt),
		mixContentEntityEnvelope(t, "repo-elixir", "api", "mix.exs", "cowboy", rows, observedAt),
	}

	if decisions := BuildPackageConsumptionDecisions(envelopes); len(decisions) != 0 {
		t.Fatalf("BuildPackageConsumptionDecisions admitted Mix git provenance as Hex consumption: %#v", decisions)
	}
}

func TestBuildPackageConsumptionDecisionsMatchesPrivateHexOrganization(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 6, 1, 14, 40, 0, 0, time.UTC)
	rows := parseMixRowsForTest(t, "mix.exs", `defmodule Demo.MixProject do
  use Mix.Project

  defp deps do
    [
      {:private_pkg, "~> 0.1", organization: "Acme"}
    ]
  end
end
`)
	envelopes := []facts.Envelope{
		packageRegistryPackageFact("hex://repo.hex.pm/acme/private_pkg", "hex", "private_pkg", "acme", observedAt),
		packageSourceRepositoryFact("repo-elixir", "api", "https://github.com/acme/api", false, observedAt),
		mixContentEntityEnvelope(t, "repo-elixir", "api", "mix.exs", "private_pkg", rows, observedAt),
	}

	decisions := BuildPackageConsumptionDecisions(envelopes)
	if got, want := len(decisions), 1; got != want {
		t.Fatalf("len(decisions) = %d, want %d: %#v", got, want, decisions)
	}
	if got, want := decisions[0].PackageID, "hex://repo.hex.pm/acme/private_pkg"; got != want {
		t.Fatalf("PackageID = %q, want private organization package identity", got)
	}
}

func parseMixRowsForTest(t *testing.T, name string, body string) []map[string]any {
	t.Helper()

	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write %s fixture: %v", name, err)
	}
	payload, err := elixirparser.Parse(path, false, shared.Options{})
	if err != nil {
		t.Fatalf("elixirparser.Parse(%s): %v", name, err)
	}
	rows, _ := payload["variables"].([]map[string]any)
	return rows
}

func mixContentEntityEnvelope(
	t *testing.T,
	repoID string,
	repoName string,
	relativePath string,
	dependencyName string,
	rows []map[string]any,
	observedAt time.Time,
) facts.Envelope {
	t.Helper()

	for _, row := range rows {
		name, _ := row["name"].(string)
		if name == dependencyName {
			return mixContentEntityEnvelopeForRow(repoID, repoName, relativePath, dependencyName, row, observedAt)
		}
	}
	t.Fatalf("dependency row for %q missing from parsed Mix rows", dependencyName)
	return facts.Envelope{}
}

func mixContentEntityEnvelopeForRow(
	repoID string,
	repoName string,
	relativePath string,
	dependencyName string,
	row map[string]any,
	observedAt time.Time,
) facts.Envelope {
	metadata := map[string]any{
		"config_kind":       row["config_kind"],
		"package_manager":   row["package_manager"],
		"section":           row["section"],
		"value":             row["value"],
		"dependency_scope":  row["dependency_scope"],
		"namespace":         row["namespace"],
		"lockfile":          row["lockfile"],
		"dependency_path":   row["dependency_path"],
		"dependency_depth":  row["dependency_depth"],
		"direct_dependency": row["direct_dependency"],
	}
	return facts.Envelope{
		FactID:        "mix-dep:" + repoID + ":" + dependencyName + ":" + relativePath,
		FactKind:      factKindContentEntity,
		ObservedAt:    observedAt,
		IsTombstone:   false,
		SourceRef:     facts.Ref{SourceSystem: "git"},
		StableFactKey: "content_entity:" + repoID + ":" + dependencyName + ":" + relativePath,
		Payload: map[string]any{
			"repo_id":         repoID,
			"relative_path":   relativePath,
			"entity_type":     "Variable",
			"entity_name":     dependencyName,
			"entity_metadata": metadata,
			"repo_name":       repoName,
		},
	}
}
