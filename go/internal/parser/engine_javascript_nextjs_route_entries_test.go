// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"path/filepath"
	"testing"
)

func TestDefaultEngineParsePathNextJSAppRouteEntriesFromExportedHandlers(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "src", "app", "api", "accounts", "[id]", "route.ts")
	writeTestFile(
		t,
		filePath,
		`import { NextResponse } from "next/server";

export async function GET() {
  return NextResponse.json({ ok: true });
}

export const POST = async function POST() {
  return NextResponse.json({ created: true }, { status: 201 });
};

function DELETE() {
  return NextResponse.json({ deleted: true });
}
`,
	)

	got := mustParsePath(t, repoRoot, filePath)

	assertFrameworksEqual(t, got, "nextjs")
	assertNestedStringValue(t, got, "nextjs", "module_kind", "route")
	assertNestedStringSliceEqual(t, got, "nextjs", "route_segments", []string{"api", "accounts", "[id]"})
	assertNestedStringSliceEqual(t, got, "nextjs", "route_verbs", []string{"GET", "POST"})
	assertNestedRouteEntriesEqual(t, got, "nextjs", []map[string]string{
		{"method": "GET", "path": "/api/accounts/[id]", "handler": "GET"},
		{"method": "POST", "path": "/api/accounts/[id]", "handler": "POST"},
	})
}

func TestDefaultEngineParsePathNextJSRootAppRouteEntry(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "src", "app", "route.ts")
	writeTestFile(
		t,
		filePath,
		`export async function GET() {
  return Response.json({ ok: true });
}
`,
	)

	got := mustParsePath(t, repoRoot, filePath)

	assertFrameworksEqual(t, got, "nextjs")
	assertNestedStringValue(t, got, "nextjs", "module_kind", "route")
	assertNestedStringSliceEqual(t, got, "nextjs", "route_segments", []string{})
	assertNestedStringSliceEqual(t, got, "nextjs", "route_verbs", []string{"GET"})
	assertNestedRouteEntriesEqual(t, got, "nextjs", []map[string]string{
		{"method": "GET", "path": "/", "handler": "GET"},
	})
}

func TestDefaultEngineParsePathNextJSPagesAPIRouteEntryFromNamedDefault(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "pages", "api", "reports", "[reportId].ts")
	writeTestFile(
		t,
		filePath,
		`import type { NextApiRequest, NextApiResponse } from "next";

export default function reportHandler(_req: NextApiRequest, res: NextApiResponse) {
  res.status(200).json({ ok: true });
}
`,
	)

	got := mustParsePath(t, repoRoot, filePath)

	assertFrameworksEqual(t, got, "nextjs")
	assertNestedStringValue(t, got, "nextjs", "module_kind", "pages_api")
	assertNestedStringSliceEqual(t, got, "nextjs", "route_segments", []string{"api", "reports", "[reportId]"})
	assertNestedStringSliceEqual(t, got, "nextjs", "route_verbs", []string{"ANY"})
	assertNestedRouteEntriesEqual(t, got, "nextjs", []map[string]string{
		{"method": "ANY", "path": "/api/reports/[reportId]", "handler": "reportHandler"},
	})
}

func TestDefaultEngineParsePathNextJSPagesAPIAnonymousDefaultOmitsRouteEntries(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "pages", "api", "reports.ts")
	writeTestFile(
		t,
		filePath,
		`export default (_req, res) => {
  res.status(200).json({ ok: true });
};
`,
	)

	got := mustParsePath(t, repoRoot, filePath)
	semantics := nestedSemanticsSection(t, got, "nextjs")
	if _, ok := semantics["route_entries"]; ok {
		t.Fatalf("framework_semantics.nextjs.route_entries = %#v, want absent for anonymous pages/api default", semantics["route_entries"])
	}
}

func TestDefaultEngineParsePathNextJSPageDoesNotEmitRouteEntries(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "src", "app", "dashboard", "page.tsx")
	writeTestFile(
		t,
		filePath,
		`export default function DashboardPage() {
  return <main>Dashboard</main>;
}
`,
	)

	got := mustParsePath(t, repoRoot, filePath)
	semantics := nestedSemanticsSection(t, got, "nextjs")
	if _, ok := semantics["route_entries"]; ok {
		t.Fatalf("framework_semantics.nextjs.route_entries = %#v, want absent for page modules", semantics["route_entries"])
	}
}

func TestDefaultEngineParsePathNextJSUnsupportedAppSegmentOmitsRouteEntries(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "src", "app", "@modal", "api", "accounts", "route.ts")
	writeTestFile(
		t,
		filePath,
		`export async function GET() {
  return Response.json({ ok: true });
}
`,
	)

	got := mustParsePath(t, repoRoot, filePath)
	semantics := nestedSemanticsSection(t, got, "nextjs")
	if _, ok := semantics["route_entries"]; ok {
		t.Fatalf("framework_semantics.nextjs.route_entries = %#v, want absent for parallel route segments", semantics["route_entries"])
	}
}
