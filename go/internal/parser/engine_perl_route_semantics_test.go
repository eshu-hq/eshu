// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"path/filepath"
	"slices"
	"testing"
)

func TestDefaultEngineParsePathPerlExactFrameworkRouteEntries(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	mojoPath := filepath.Join(repoRoot, "mojo.pl")
	writeTestFile(
		t,
		mojoPath,
		`use Mojolicious::Lite;

sub health {}
sub create_order {}
sub parenthesized {}
sub show_order {}

get "/health" => \&health;
post "/orders" => \&create_order;
get("/ready" => \&parenthesized);
get "/orders/:id" => \&show_order;
`,
	)

	mojo := mustParsePath(t, repoRoot, mojoPath)

	assertFrameworksEqual(t, mojo, "mojolicious")
	assertNestedStringSliceEqual(t, mojo, "mojolicious", "route_methods", []string{"GET", "POST"})
	assertNestedStringSliceEqual(t, mojo, "mojolicious", "route_paths", []string{"/health", "/orders", "/ready", "/orders/:id"})
	assertNestedRouteEntriesEqual(t, mojo, "mojolicious", []map[string]string{
		{"method": "GET", "path": "/health", "handler": "health"},
		{"method": "POST", "path": "/orders", "handler": "create_order"},
		{"method": "GET", "path": "/ready", "handler": "parenthesized"},
		{"method": "GET", "path": "/orders/:id", "handler": "show_order"},
	})

	dancerPath := filepath.Join(repoRoot, "dancer.pl")
	writeTestFile(
		t,
		dancerPath,
		`use Dancer2;

sub health {}
sub create_order {}
sub delete_order {}
sub show_order {}

get "/health" => \&health;
post "/orders" => \&create_order;
del '/orders/:id' => \&delete_order;
get "/orders/:id" => \&show_order;
`,
	)

	dancer := mustParsePath(t, repoRoot, dancerPath)

	assertFrameworksEqual(t, dancer, "dancer")
	assertNestedStringSliceEqual(t, dancer, "dancer", "route_methods", []string{"GET", "POST", "DELETE"})
	assertNestedStringSliceEqual(t, dancer, "dancer", "route_paths", []string{"/health", "/orders", "/orders/:id"})
	assertNestedRouteEntriesEqual(t, dancer, "dancer", []map[string]string{
		{"method": "GET", "path": "/health", "handler": "health"},
		{"method": "POST", "path": "/orders", "handler": "create_order"},
		{"method": "DELETE", "path": "/orders/:id", "handler": "delete_order"},
		{"method": "GET", "path": "/orders/:id", "handler": "show_order"},
	})
}

func TestDefaultEngineParsePathPerlPreservesQualifiedRouteHandlers(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "qualified.pl")
	writeTestFile(
		t,
		filePath,
		`use Dancer2;

package Public;
sub show {}

package Admin;
sub show {}

get "/orders" => \&Admin::show;
`,
	)

	got := mustParsePath(t, repoRoot, filePath)

	assertFrameworksEqual(t, got, "dancer")
	assertNestedRouteEntriesEqual(t, got, "dancer", []map[string]string{
		{"method": "GET", "path": "/orders", "handler": "Admin::show"},
	})
	functions, ok := got["functions"].([]map[string]any)
	if !ok {
		t.Fatalf("functions = %T, want []map[string]any", got["functions"])
	}
	var fullNames []string
	for _, function := range functions {
		if fullName, _ := function["full_name"].(string); fullName != "" {
			fullNames = append(fullNames, fullName)
		}
	}
	if !slices.Contains(fullNames, "Admin::show") {
		t.Fatalf("functions full names = %#v, want Admin::show", fullNames)
	}
}

func TestDefaultEngineParsePathPerlSkipsNonExactFrameworkRoutes(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "dynamic.pl")
	writeTestFile(
		t,
		filePath,
		`use Mojolicious::Lite;
use Dancer2;

my $dynamic_path = "/health";
sub health {}

get $dynamic_path => \&health;
get "/inline" => sub { health() };
get "/controller" => "orders#show";
any "/any" => \&health;
MY_get "/wrapped" => \&health;
get "/ambiguous" => \&health;
`,
	)

	got := mustParsePath(t, repoRoot, filePath)

	assertFrameworksEqual(t, got)
}
