// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package pythondep

import (
	"fmt"
	"strings"
	"testing"
)

// constraintsSecret builds a credential-shaped value by concatenation so no
// source line looks like a committed secret to push protection.
func constraintsSecret() string {
	return "tok" + "en-" + "value-9f3a"
}

// TestParseRequirementsConstraintsPinsAreVersionPoliciesNotDeclarations proves
// the pip contract for constraints files: a line limits the version a package
// gets if something else installs it, and never declares the dependency. Each
// registry pin keeps config_kind=dependency so the content identity and the
// pypi row shape match requirements.txt, and carries
// central_version_declaration=true so every consumption reader drops it
// (#8821, same flag as a NuGet central PackageVersion, #8824).
func TestParseRequirementsConstraintsPinsAreVersionPoliciesNotDeclarations(t *testing.T) {
	t.Parallel()

	secret := constraintsSecret()
	path := writeTempFile(t, "constraints.txt", strings.Join([]string{
		"# pinned by pip-compile",
		"-c base-constraints.txt",
		"-r requirements.txt",
		"--index-url https://ci-user:" + secret + "@packages.example.com/simple",
		"--extra-index-url=https://ci-user:" + secret + "@packages.example.com/extra",
		"requests==2.31.0",
		"Django>=4.2,<5.0 ; python_version >= '3.9'",
		"numpy ~= 1.26 # inline comment",
		"",
	}, "\n"))

	payload, err := ParseRequirements(path)
	if err != nil {
		t.Fatalf("ParseRequirements(constraints.txt) error = %v, want nil", err)
	}
	rows := variableRows(t, payload)
	if got, want := len(rows), 3; got != want {
		t.Fatalf("len(rows) = %d, want %d (option lines are never rows): %#v", got, want, rows)
	}
	byName := rowsByName(rows)
	for name, wantValue := range map[string]string{
		"requests": "==2.31.0",
		"Django":   ">=4.2,<5.0",
		"numpy":    "~=1.26",
	} {
		row, ok := byName[name]
		if !ok {
			t.Fatalf("%s missing from %#v", name, rows)
		}
		if got := row["config_kind"]; got != "dependency" {
			t.Errorf("%s config_kind = %#v, want dependency", name, got)
		}
		if got := row["central_version_declaration"]; got != true {
			t.Errorf("%s central_version_declaration = %#v, want true", name, got)
		}
		if got := row["section"]; got != "constraints" {
			t.Errorf("%s section = %#v, want constraints", name, got)
		}
		if got := row["package_manager"]; got != PackageManager {
			t.Errorf("%s package_manager = %#v, want %q", name, got, PackageManager)
		}
		if got := row["value"]; got != wantValue {
			t.Errorf("%s value = %#v, want %q", name, got, wantValue)
		}
		if _, set := row["direct_dependency"]; set {
			t.Errorf("%s sets direct_dependency; a constraint proves no declaration", name)
		}
		if _, set := row["dev_dependency"]; set {
			t.Errorf("%s dev_dependency set for plain constraints.txt", name)
		}
	}
	if got := byName["Django"]["marker"]; got != "python_version >= '3.9'" {
		t.Errorf("Django marker = %#v, want the PEP 508 marker kept", got)
	}
	if text := fmt.Sprintf("%#v", payload); strings.Contains(text, secret) {
		t.Fatalf("payload carries index credential %q: %s", secret, text)
	}
}

// TestParseRequirementsConstraintsFilenameScope proves the section and the
// dev scope follow the constraints file name the way requirements-* names do,
// for both separators, and that unrelated names stay unflagged.
func TestParseRequirementsConstraintsFilenameScope(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name        string
		wantSection string
		wantDev     bool
	}{
		{name: "constraints.txt", wantSection: "constraints"},
		{name: "constraints-prod.txt", wantSection: "constraints-prod"},
		{name: "constraints-dev.txt", wantSection: "constraints-dev", wantDev: true},
		{name: "constraints_test.txt", wantSection: "constraints_test", wantDev: true},
		{name: "Constraints-CI.txt", wantSection: "constraints-ci", wantDev: true},
	} {
		path := writeTempFile(t, tc.name, "requests==2.31.0\n")
		payload, err := ParseRequirements(path)
		if err != nil {
			t.Fatalf("%s: ParseRequirements error = %v", tc.name, err)
		}
		rows := variableRows(t, payload)
		if len(rows) != 1 {
			t.Fatalf("%s: len(rows) = %d, want 1", tc.name, len(rows))
		}
		row := rows[0]
		if got := row["section"]; got != tc.wantSection {
			t.Errorf("%s section = %#v, want %q", tc.name, got, tc.wantSection)
		}
		if got := row["dev_dependency"] == true; got != tc.wantDev {
			t.Errorf("%s dev_dependency = %v, want %v", tc.name, got, tc.wantDev)
		}
		if row["central_version_declaration"] != true {
			t.Errorf("%s central_version_declaration = %#v, want true", tc.name, row["central_version_declaration"])
		}
	}
}

// TestParseRequirementsDoesNotFlagRequirementsFilesAsConstraints is the
// negative control: a requirements file declares its dependencies, so its rows
// must stay consumption evidence and never carry the policy flag.
func TestParseRequirementsDoesNotFlagRequirementsFilesAsConstraints(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"requirements.txt", "requirements-dev.txt", "requirements_constraints.txt", "my-constraints.txt"} {
		path := writeTempFile(t, name, "requests==2.31.0\n")
		payload, err := ParseRequirements(path)
		if err != nil {
			t.Fatalf("%s: ParseRequirements error = %v", name, err)
		}
		rows := variableRows(t, payload)
		if len(rows) != 1 {
			t.Fatalf("%s: len(rows) = %d, want 1", name, len(rows))
		}
		if _, flagged := rows[0]["central_version_declaration"]; flagged {
			t.Errorf("%s row carries central_version_declaration; only constraints files are policy", name)
		}
	}
}

// TestParseRequirementsConstraintsDropsCredentialBearingSourceText proves that
// a constraints line with a direct source never persists the source text. The
// kind survives so the row still reads as non-registry provenance, but the
// value, source_url, and raw line are empty, so a userinfo credential, a
// token in a query, or a password containing a slash cannot reach a row.
func TestParseRequirementsConstraintsDropsCredentialBearingSourceText(t *testing.T) {
	t.Parallel()

	secret := constraintsSecret()
	path := writeTempFile(t, "constraints-prod.txt", strings.Join([]string{
		"direct-url @ https://ci-user:" + secret + "@files.example.com/direct-url-1.0.tar.gz",
		"slash-url @ https://ci-user:" + "pa/" + secret + "@files.example.com/slash-url-1.0.tar.gz",
		"query-url @ https://files.example.com/query-url-1.0.tar.gz?sig=" + secret,
		"direct-vcs @ git+https://ci-user:" + secret + "@git.example.com/acme/direct-vcs.git@v1.0.0",
		"bare-user:" + secret + "@host.example.com/pkg",
		"@@@ " + secret + " junk",
		"./vendor/pkg:" + secret + "@mirror",
	}, "\n"))

	payload, err := ParseRequirements(path)
	if err != nil {
		t.Fatalf("ParseRequirements error = %v", err)
	}
	if text := fmt.Sprintf("%#v", payload); strings.Contains(text, secret) || strings.Contains(text, "ci-user") {
		t.Fatalf("payload carries credential text: %s", text)
	}
	rows := variableRows(t, payload)
	if got, want := len(rows), 7; got != want {
		t.Fatalf("len(rows) = %d, want %d: %#v", got, want, rows)
	}
	for _, row := range rows {
		if kind := row["config_kind"]; kind == "dependency" {
			t.Errorf("row %#v kept config_kind=dependency for a non-registry line", row)
		}
		if row["central_version_declaration"] != true {
			t.Errorf("row %#v missing central_version_declaration; no constraint row is a declaration", row)
		}
		for _, field := range []string{"value", "source_url", "raw"} {
			if got, _ := row[field].(string); got != "" {
				t.Errorf("row %v %s = %q, want it dropped", row["name"], field, got)
			}
		}
	}
	byName := rowsByName(rows)
	for name, kind := range map[string]string{
		"direct-url": "url_dependency",
		"slash-url":  "url_dependency",
		"query-url":  "url_dependency",
		"direct-vcs": "vcs_dependency",
	} {
		if got := byName[name]["config_kind"]; got != kind {
			t.Errorf("%s config_kind = %#v, want %q", name, got, kind)
		}
	}
}

// TestParseRequirementsConstraintsKeepsNoRawLineOrUnsafeMarker proves that the
// raw line is never stored for a constraints row, that an inline comment
// therefore cannot carry text into a row, that a marker holding URL-shaped
// text is dropped, and that every row asks the content layer to withhold the
// entity source_cache (the line range of the file, sliced by the content
// shaper). A plain marker survives.
func TestParseRequirementsConstraintsKeepsNoRawLineOrUnsafeMarker(t *testing.T) {
	t.Parallel()

	secret := constraintsSecret()
	path := writeTempFile(t, "constraints.txt", strings.Join([]string{
		"pinned==1.0 # mirror https://ci-user:" + secret + "@mirror.example/simple",
		"marked==2.0 ; python_version > '3' and platform_node == 'https://ci-user:" + secret + "@m.example'",
		"plain==3.0 ; python_version >= '3.9'",
		"direct @ https://ci-user:" + secret + "@host.example/p.whl",
	}, "\n"))
	payload, err := ParseRequirements(path)
	if err != nil {
		t.Fatalf("ParseRequirements error = %v", err)
	}
	if text := fmt.Sprintf("%#v", payload); strings.Contains(text, secret) {
		t.Fatalf("payload carries credential text: %s", text)
	}
	rows := variableRows(t, payload)
	if len(rows) != 4 {
		t.Fatalf("len(rows) = %d, want 4: %#v", len(rows), rows)
	}
	for _, row := range rows {
		if _, has := row["raw"]; has {
			t.Errorf("row %v keeps raw; a constraints row must not store the line", row["name"])
		}
		if row["source_cache_withheld"] != true {
			t.Errorf("row %v source_cache_withheld = %#v, want true", row["name"], row["source_cache_withheld"])
		}
	}
	byName := rowsByName(rows)
	if _, has := byName["marked"]["marker"]; has {
		t.Errorf("marked keeps a marker with URL-shaped text: %#v", byName["marked"]["marker"])
	}
	if got := byName["plain"]["marker"]; got != "python_version >= '3.9'" {
		t.Errorf("plain marker = %#v, want the PEP 508 marker kept", got)
	}
}

// TestParseRequirementsDoesNotWithholdSourceCacheForRequirements is the
// negative control: a requirements row still stores raw and leaves the
// source_cache alone.
func TestParseRequirementsDoesNotWithholdSourceCacheForRequirements(t *testing.T) {
	t.Parallel()

	path := writeTempFile(t, "requirements.txt", "requests==2.31.0\n")
	payload, err := ParseRequirements(path)
	if err != nil {
		t.Fatalf("ParseRequirements error = %v", err)
	}
	row := variableRows(t, payload)[0]
	if _, has := row["source_cache_withheld"]; has {
		t.Errorf("requirements row sets source_cache_withheld: %#v", row)
	}
	if row["raw"] != "requests==2.31.0" {
		t.Errorf("requirements raw = %#v, want the line kept", row["raw"])
	}
}

// TestEggNameFromURLIgnoresFragmentInsideAuthority proves the egg name is read
// only from a fragment after the authority: a raw "#" inside userinfo is not a
// fragment start, so text there must not become a package name.
func TestEggNameFromURLIgnoresFragmentInsideAuthority(t *testing.T) {
	t.Parallel()

	for content, want := range map[string]string{
		"git+https://host.example/org/repo.git@v1#egg=real":    "real",
		"https://host.example/p.whl#egg=real&subdirectory=x":   "real",
		"https://u:p#egg=leaked&x@host.example/p.whl":          "",
		"git+https://u:p#egg=leaked@host.example/org/repo.git": "",
		"https://host.example":                                 "",
		"https://u:p@host.example/p.whl#sha256=abc":            "",
		"https://host.example/a?x=1#egg=real":                  "real",
	} {
		if got := eggNameFromURL(content); got != want {
			t.Errorf("eggNameFromURL(%q) = %q, want %q", content, got, want)
		}
	}
}
