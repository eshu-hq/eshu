// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package elixir

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

func TestParseMixExsEmitsHexManifestDependencies(t *testing.T) {
	t.Parallel()

	path := writeElixirDependencyFixture(t, "mix.exs", `defmodule Demo.MixProject do
  use Mix.Project

  defp deps do
    [
      {:phoenix_html, "~> 4.2"},
      {:private_pkg, "~> 0.1", organization: "Acme"},
      {:postgrex_app, "~> 0.19", hex: :postgrex},
      {:jason, "~> 1.4", only: :test},
      {:forked, "~> 1.0", github: "acme/forked"},
      {:cowboy, github: "ninenines/cowboy"}
    ]
  end
end
`)

	payload, err := Parse(path, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	rows := elixirDependencyRowsByName(t, payload)

	phoenix := rows["phoenix_html"]
	if phoenix == nil {
		t.Fatalf("phoenix_html dependency row missing: %#v", rows)
	}
	assertElixirDependencyString(t, phoenix, "config_kind", "dependency")
	assertElixirDependencyString(t, phoenix, "package_manager", "hex")
	assertElixirDependencyString(t, phoenix, "value", "~> 4.2")
	assertElixirDependencyString(t, phoenix, "dependency_scope", "runtime")
	assertElixirDependencyBool(t, phoenix, "direct_dependency", true)

	privatePkg := rows["private_pkg"]
	if privatePkg == nil {
		t.Fatalf("private_pkg dependency row missing: %#v", rows)
	}
	assertElixirDependencyString(t, privatePkg, "namespace", "acme")

	postgrex := rows["postgrex"]
	if postgrex == nil {
		t.Fatalf("postgrex package override row missing: %#v", rows)
	}
	assertElixirDependencyString(t, postgrex, "app_name", "postgrex_app")
	assertElixirDependencyString(t, postgrex, "value", "~> 0.19")

	jason := rows["jason"]
	if jason == nil {
		t.Fatalf("jason dependency row missing: %#v", rows)
	}
	assertElixirDependencyString(t, jason, "dependency_scope", "test")

	forked := rows["forked"]
	if forked == nil {
		t.Fatalf("forked provenance row missing: %#v", rows)
	}
	assertElixirDependencyString(t, forked, "config_kind", "vcs_dependency")

	cowboy := rows["cowboy"]
	if cowboy == nil {
		t.Fatalf("cowboy provenance row missing: %#v", rows)
	}
	assertElixirDependencyString(t, cowboy, "config_kind", "vcs_dependency")
}

func TestParseMixLockEmitsExactHexDependencyVersions(t *testing.T) {
	t.Parallel()

	path := writeElixirDependencyFixture(t, "mix.lock", `%{
  "decimal": {:hex, :decimal, "2.1.1", "checksum", [:mix], [], "hexpm", "outer"},
  "phoenix_html": {:hex, :phoenix_html, "4.2.1", "checksum", [:mix], [{:plug, "~> 1.5", [hex: :plug, repo: "hexpm", optional: false]}], "hexpm", "outer"},
  "local_lib": {:git, "https://example.invalid/local.git", "abc"}
}
`)

	payload, err := Parse(path, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	rows := elixirDependencyRowsByName(t, payload)

	phoenix := rows["phoenix_html"]
	if phoenix == nil {
		t.Fatalf("phoenix_html lockfile row missing: %#v", rows)
	}
	assertElixirDependencyString(t, phoenix, "config_kind", "dependency")
	assertElixirDependencyString(t, phoenix, "package_manager", "hex")
	assertElixirDependencyString(t, phoenix, "value", "4.2.1")
	assertElixirDependencyString(t, phoenix, "section", "mix.lock")
	assertElixirDependencyBool(t, phoenix, "lockfile", true)
	assertElixirDependencyBool(t, phoenix, "direct_dependency", true)

	plug := rows["plug"]
	if plug == nil {
		t.Fatalf("plug transitive row missing: %#v", rows)
	}
	assertElixirDependencyString(t, plug, "value", "~> 1.5")
	assertElixirDependencyBool(t, plug, "direct_dependency", false)
	if got, want := plug["dependency_depth"], 2; got != want {
		t.Fatalf("plug dependency_depth = %#v, want %d", got, want)
	}

	if local := rows["local_lib"]; local != nil {
		t.Fatalf("local git lock entry emitted dependency row %#v; non-Hex entries must stay out of consumption truth", local)
	}
}

func writeElixirDependencyFixture(t *testing.T, name string, body string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	return path
}

func elixirDependencyRowsByName(t *testing.T, payload map[string]any) map[string]map[string]any {
	t.Helper()

	rawRows, ok := payload["variables"].([]map[string]any)
	if !ok {
		t.Fatalf("variables type = %T, want []map[string]any", payload["variables"])
	}
	rows := make(map[string]map[string]any, len(rawRows))
	for _, row := range rawRows {
		name, _ := row["name"].(string)
		if name != "" {
			rows[name] = row
		}
	}
	return rows
}

func assertElixirDependencyString(t *testing.T, row map[string]any, key string, want string) {
	t.Helper()

	if got, _ := row[key].(string); got != want {
		t.Fatalf("%s = %#v, want %q in row %#v", key, row[key], want, row)
	}
}

func assertElixirDependencyBool(t *testing.T, row map[string]any, key string, want bool) {
	t.Helper()

	if got, _ := row[key].(bool); got != want {
		t.Fatalf("%s = %#v, want %v in row %#v", key, row[key], want, row)
	}
}
