// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package exports

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var updateGolden = flag.Bool("update-golden", false, "rewrite testdata golden files from the live exporter output")

func TestSARIFExporter_Format(t *testing.T) {
	t.Parallel()
	if got := NewSARIFExporter().Format(); got != FormatSARIF {
		t.Fatalf("Format() = %q, want %q", got, FormatSARIF)
	}
}

func TestSARIFExporter_RejectsInvalidScope(t *testing.T) {
	t.Parallel()
	exporter := NewSARIFExporter()
	err := exporter.Export(&bytes.Buffer{}, Snapshot{}, Options{})
	if err == nil {
		t.Fatalf("Export() with empty scope returned nil error")
	}
	if !strings.Contains(err.Error(), "scope must set") {
		t.Fatalf("Export() error = %q, want substring %q", err.Error(), "scope must set")
	}
}

func TestSARIFExporter_GoldenFixtures(t *testing.T) {
	t.Parallel()
	stamp := time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name     string
		fixture  string
		snapshot Snapshot
		opts     Options
	}{
		{
			name:    "empty repository scope",
			fixture: "empty_repository_scope.json",
			snapshot: Snapshot{
				Scope:       Scope{Kind: ScopeKindRepository, RepositoryID: "repo-empty"},
				GeneratedAt: stamp,
			},
		},
		{
			name:    "single finding with manifest location",
			fixture: "single_finding_repository.json",
			snapshot: Snapshot{
				Scope:       Scope{Kind: ScopeKindRepository, RepositoryID: "repo-main"},
				GeneratedAt: stamp,
				Findings: []Finding{
					{
						FindingID:       "fnd-001",
						CVEID:           "CVE-2024-1234",
						AdvisoryID:      "GHSA-aaaa-bbbb-cccc",
						PackageID:       "pkg-npm-lodash",
						Ecosystem:       "npm",
						PackageName:     "lodash",
						PURL:            "pkg:npm/lodash@4.17.20",
						ObservedVersion: "4.17.20",
						FixedVersion:    "4.17.21",
						Summary:         "Prototype pollution in lodash",
						Description:     "Lodash versions before 4.17.21 allow prototype pollution via the merge helper.",
						Severity:        SeverityHigh,
						CVSSScore:       7.4,
						CVSSVector:      "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:L/I:L/A:L",
						KnownExploited:  false,
						EPSSProbability: "0.12345",
						RepositoryID:    "repo-main",
						Locations: []Location{
							{ManifestPath: "package.json", StartLine: 12, EndLine: 12},
						},
						AdvisorySources: []AdvisorySource{
							{Source: "ghsa", AdvisoryID: "GHSA-aaaa-bbbb-cccc", URL: "https://github.com/advisories/GHSA-aaaa-bbbb-cccc"},
							{Source: "nvd", AdvisoryID: "CVE-2024-1234"},
						},
						HelpURI: "https://github.com/advisories/GHSA-aaaa-bbbb-cccc",
					},
				},
			},
			opts: Options{Tool: Tool{Name: "eshu", Version: "0.0.3-pre-release"}},
		},
		{
			name:    "multi finding with scope drop and redaction",
			fixture: "multi_finding_redacted.json",
			snapshot: Snapshot{
				Scope:       Scope{Kind: ScopeKindRepository, RepositoryID: "repo-main"},
				GeneratedAt: stamp,
				Findings: []Finding{
					{
						FindingID:       "fnd-200",
						CVEID:           "CVE-2024-2222",
						AdvisoryID:      "GHSA-2222-2222-2222",
						PackageID:       "pkg-pypi-requests",
						Ecosystem:       "PyPI",
						PackageName:     "requests",
						PURL:            "pkg:pypi/requests@2.30.0",
						ObservedVersion: "2.30.0",
						FixedVersion:    "2.31.0",
						Summary:         "Header injection in requests",
						Severity:        SeverityCritical,
						CVSSScore:       9.1,
						KnownExploited:  true,
						RepositoryID:    "repo-main",
						Locations: []Location{
							{ManifestPath: "services/api/requirements.txt", StartLine: 4},
							{ManifestPath: "services/api/requirements.txt", StartLine: 8},
						},
						AdvisorySources: []AdvisorySource{
							{Source: "osv", AdvisoryID: "PYSEC-2024-1"},
							{Source: "ghsa", AdvisoryID: "GHSA-2222-2222-2222"},
						},
					},
					{
						FindingID:       "fnd-100",
						AdvisoryID:      "GHSA-1111-1111-1111",
						PackageID:       "pkg-golang-fasthttp",
						Ecosystem:       "Go",
						PackageName:     "github.com/valyala/fasthttp",
						PURL:            "pkg:golang/github.com/valyala/fasthttp@1.50.0",
						ObservedVersion: "1.50.0",
						FixedVersion:    "1.52.0",
						Severity:        SeverityMedium,
						CVSSScore:       5.3,
						RepositoryID:    "repo-main",
						Locations: []Location{
							{ManifestPath: "go.mod"},
						},
					},
					{
						FindingID:    "fnd-999",
						AdvisoryID:   "GHSA-9999-9999-9999",
						PackageID:    "pkg-npm-other",
						PackageName:  "other",
						Severity:     SeverityLow,
						RepositoryID: "repo-other-tenant",
						Locations: []Location{
							{ManifestPath: "secret/other/package.json"},
						},
					},
				},
			},
			opts: Options{
				Tool:     Tool{Name: "eshu", Version: "0.0.3-pre-release", URI: "https://eshu.dev"},
				Redactor: prefixRedactor{prefix: "services/"},
			},
		},
		{
			name:    "image digest scope",
			fixture: "image_digest_scope.json",
			snapshot: Snapshot{
				Scope: Scope{
					Kind:          ScopeKindImageDigest,
					SubjectDigest: "sha256:beefbeefbeefbeefbeefbeefbeefbeefbeefbeefbeefbeefbeefbeefbeefbeef",
				},
				GeneratedAt: stamp,
				Findings: []Finding{
					{
						FindingID:       "fnd-img-1",
						AdvisoryID:      "GHSA-img-1",
						PackageID:       "pkg-deb-openssl",
						Ecosystem:       "Debian",
						PackageName:     "openssl",
						PURL:            "pkg:deb/debian/openssl@1.1.1k-1+deb11u3",
						ObservedVersion: "1.1.1k-1+deb11u3",
						FixedVersion:    "1.1.1n-0+deb11u3",
						Severity:        SeverityHigh,
						SubjectDigest:   "sha256:beefbeefbeefbeefbeefbeefbeefbeefbeefbeefbeefbeefbeefbeefbeefbeef",
					},
				},
			},
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			exporter := NewSARIFExporter()
			if err := exporter.Export(&buf, tc.snapshot, tc.opts); err != nil {
				t.Fatalf("Export() error: %v", err)
			}
			goldenPath := filepath.Join("testdata", "sarif", tc.fixture)
			if *updateGolden {
				if err := os.WriteFile(goldenPath, buf.Bytes(), 0o644); err != nil {
					t.Fatalf("write golden: %v", err)
				}
				return
			}
			want, err := os.ReadFile(goldenPath)
			if err != nil {
				t.Fatalf("read golden %s: %v", goldenPath, err)
			}
			assertBytesEqual(t, want, buf.Bytes())
			assertScopeNotLeaked(t, buf.Bytes(), tc.snapshot.Scope)
		})
	}
}

func TestSARIFExporter_FormattingContract(t *testing.T) {
	t.Parallel()
	snapshot := Snapshot{
		Scope:       Scope{Kind: ScopeKindRepository, RepositoryID: "repo-main"},
		GeneratedAt: time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC),
		Findings: []Finding{
			{FindingID: "fnd-a", AdvisoryID: "GHSA-a", PackageName: "p", Severity: SeverityHigh, RepositoryID: "repo-main"},
		},
	}
	var buf bytes.Buffer
	if err := NewSARIFExporter().Export(&buf, snapshot, Options{}); err != nil {
		t.Fatalf("Export() error: %v", err)
	}
	body := buf.Bytes()
	if len(body) == 0 || body[len(body)-1] != '\n' {
		t.Fatalf("output does not end with a single trailing newline (encoding/json.Encoder.Encode behavior)")
	}
	if len(body) >= 2 && body[len(body)-2] == '\n' {
		t.Fatalf("output ends with more than one trailing newline; want exactly one")
	}
	if !strings.Contains(string(body), "\n  \"version\":") {
		t.Fatalf("output missing two-space indent on top-level keys; got:\n%s", body)
	}
}

func TestSARIFExporter_IsDeterministic(t *testing.T) {
	t.Parallel()
	snapshot := Snapshot{
		Scope:       Scope{Kind: ScopeKindRepository, RepositoryID: "repo-main"},
		GeneratedAt: time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC),
		Findings: []Finding{
			{FindingID: "z", AdvisoryID: "GHSA-z", PackageName: "z-pkg", Severity: SeverityHigh, RepositoryID: "repo-main"},
			{FindingID: "a", AdvisoryID: "GHSA-a", PackageName: "a-pkg", Severity: SeverityHigh, RepositoryID: "repo-main"},
			{FindingID: "m", AdvisoryID: "GHSA-m", PackageName: "m-pkg", Severity: SeverityHigh, RepositoryID: "repo-main"},
		},
	}
	exporter := NewSARIFExporter()
	var first, second bytes.Buffer
	if err := exporter.Export(&first, snapshot, Options{}); err != nil {
		t.Fatalf("first Export() error: %v", err)
	}
	if err := exporter.Export(&second, snapshot, Options{}); err != nil {
		t.Fatalf("second Export() error: %v", err)
	}
	if !bytes.Equal(first.Bytes(), second.Bytes()) {
		t.Fatalf("Export() output not byte-identical across runs:\nfirst:\n%s\nsecond:\n%s", first.String(), second.String())
	}
}

func TestSARIFExporter_DropsOutOfScopeFindings(t *testing.T) {
	t.Parallel()
	snapshot := Snapshot{
		Scope:       Scope{Kind: ScopeKindRepository, RepositoryID: "repo-keep"},
		GeneratedAt: time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC),
		Findings: []Finding{
			{FindingID: "kept", AdvisoryID: "GHSA-keep", RepositoryID: "repo-keep"},
			{FindingID: "dropped", AdvisoryID: "GHSA-drop", RepositoryID: "repo-other"},
		},
	}
	var buf bytes.Buffer
	if err := NewSARIFExporter().Export(&buf, snapshot, Options{}); err != nil {
		t.Fatalf("Export() error: %v", err)
	}

	var log map[string]any
	if err := json.Unmarshal(buf.Bytes(), &log); err != nil {
		t.Fatalf("unmarshal log: %v", err)
	}

	runs, ok := log["runs"].([]any)
	if !ok || len(runs) != 1 {
		t.Fatalf("runs missing or wrong shape: %v", log["runs"])
	}
	run := runs[0].(map[string]any)
	results := run["results"].([]any)
	if len(results) != 1 {
		t.Fatalf("results length = %d, want 1; results=%v", len(results), results)
	}

	props := run["properties"].(map[string]any)
	if got := props["eshu.droppedFindings"]; got != float64(1) {
		t.Fatalf("eshu.droppedFindings = %v, want 1", got)
	}

	body := buf.String()
	if strings.Contains(body, "GHSA-drop") {
		t.Fatalf("output leaks out-of-scope advisory id GHSA-drop:\n%s", body)
	}
	if strings.Contains(body, "repo-other") {
		t.Fatalf("output leaks out-of-scope repository id repo-other:\n%s", body)
	}
}

func TestSARIFExporter_AppliesPathRedaction(t *testing.T) {
	t.Parallel()
	snapshot := Snapshot{
		Scope:       Scope{Kind: ScopeKindRepository, RepositoryID: "repo-main"},
		GeneratedAt: time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC),
		Findings: []Finding{
			{
				FindingID:    "fnd",
				AdvisoryID:   "GHSA-x",
				PackageName:  "pkg",
				Severity:     SeverityHigh,
				RepositoryID: "repo-main",
				Locations: []Location{
					{ManifestPath: "internal/private/secrets.yaml"},
					{ManifestPath: "public/manifest.yaml"},
				},
			},
		},
	}
	opts := Options{Redactor: prefixRedactor{prefix: "public/"}}
	var buf bytes.Buffer
	if err := NewSARIFExporter().Export(&buf, snapshot, opts); err != nil {
		t.Fatalf("Export() error: %v", err)
	}
	body := buf.String()
	if strings.Contains(body, "internal/private/secrets.yaml") {
		t.Fatalf("output leaks redacted path internal/private/secrets.yaml:\n%s", body)
	}
	if !strings.Contains(body, "public/manifest.yaml") {
		t.Fatalf("output dropped preserved path public/manifest.yaml:\n%s", body)
	}
	if !strings.Contains(body, "redacted-path") {
		t.Fatalf("output missing redaction marker:\n%s", body)
	}

	// Caller's snapshot must not be mutated.
	if got := snapshot.Findings[0].Locations[0].ManifestPath; got != "internal/private/secrets.yaml" {
		t.Fatalf("caller snapshot mutated: got %q, want %q", got, "internal/private/secrets.yaml")
	}
}

func TestSARIFExporter_AdvisoryScopeMatchesByCVEOrAdvisoryID(t *testing.T) {
	t.Parallel()
	snapshot := Snapshot{
		Scope:       Scope{Kind: ScopeKindAdvisory, AdvisoryID: "CVE-2024-9999"},
		GeneratedAt: time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC),
		Findings: []Finding{
			{FindingID: "match-by-cve", CVEID: "CVE-2024-9999", AdvisoryID: "GHSA-aa", PackageName: "p", Severity: SeverityHigh, RepositoryID: "repo-a"},
			{FindingID: "no-match", CVEID: "CVE-2024-0001", AdvisoryID: "GHSA-bb", PackageName: "p", Severity: SeverityHigh, RepositoryID: "repo-a"},
		},
	}
	var buf bytes.Buffer
	if err := NewSARIFExporter().Export(&buf, snapshot, Options{}); err != nil {
		t.Fatalf("Export() error: %v", err)
	}
	if !strings.Contains(buf.String(), "match-by-cve") {
		t.Fatalf("output missing match-by-cve finding:\n%s", buf.String())
	}
	if strings.Contains(buf.String(), "no-match") {
		t.Fatalf("output contains out-of-scope finding no-match:\n%s", buf.String())
	}
}

// prefixRedactor is a deterministic test redactor that preserves any path
// starting with prefix and replaces every other path with a stable marker.
type prefixRedactor struct {
	prefix string
}

func (p prefixRedactor) RedactPath(path string) string {
	if strings.HasPrefix(path, p.prefix) {
		return path
	}
	return "redacted-path"
}

// assertBytesEqual compares raw bytes so the formatting contract (indent
// width, trailing newline, escape behavior) is part of what the golden
// fixture locks. If the JSON values are semantically equal the diff would
// be invisible to a canonicalizing comparator, so we surface the byte-level
// mismatch directly and let the operator regenerate the fixture with
// `-update-golden` if the change is intentional.
func assertBytesEqual(t *testing.T, want, got []byte) {
	t.Helper()
	if bytes.Equal(want, got) {
		return
	}
	// Sanity-check both sides parse as JSON so an unmarshal failure does
	// not get hidden behind a byte-diff message.
	var wantValue, gotValue any
	if err := json.Unmarshal(want, &wantValue); err != nil {
		t.Fatalf("golden does not parse as JSON: %v\n%s", err, want)
	}
	if err := json.Unmarshal(got, &gotValue); err != nil {
		t.Fatalf("exporter output does not parse as JSON: %v\n%s", err, got)
	}
	t.Fatalf("SARIF output diverges from golden (byte mismatch — regenerate with -update-golden if intentional):\nwant (%d bytes):\n%s\ngot (%d bytes):\n%s",
		len(want), want, len(got), got)
}

func assertScopeNotLeaked(t *testing.T, raw []byte, scope Scope) {
	t.Helper()
	body := string(raw)
	for _, leakSubstring := range []string{"repo-other-tenant", "secret/other"} {
		if scope.Kind == ScopeKindRepository && scope.RepositoryID == "repo-other-tenant" {
			continue
		}
		if strings.Contains(body, leakSubstring) {
			t.Fatalf("output leaks out-of-scope substring %q:\n%s", leakSubstring, body)
		}
	}
}
