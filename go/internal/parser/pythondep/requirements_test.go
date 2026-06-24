// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package pythondep

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// TestParseRequirementsPreservesPinExtrasMarkersAndScopeReason locks in the
// pip requirements contract for the supply-chain reducer. Pinned versions,
// extras, environment markers, and runtime-vs-dev scope must all survive into
// the content_entity dependency row so vulnerability impact can be computed
// without inventing resolved versions.
func TestParseRequirementsPreservesPinExtrasMarkersAndScopeReason(t *testing.T) {
	t.Parallel()

	runtimePath := writeTempFile(t, "requirements.txt", strings.Join([]string{
		"# top-level comment",
		"requests[security]==2.31.0 ; python_version >= '3.8'",
		"Django>=4.2,<5.0",
		"   numpy ~= 1.26 ",
		"-e ./local-package",
		"git+https://github.com/psf/requests.git@v2.31.0#egg=requests-vcs",
		"file:///tmp/wheelhouse/foo-1.0-py3-none-any.whl",
		"--hash=sha256:deadbeef ignored",
		"@@@ not a valid requirement",
		"",
	}, "\n"))

	payload, err := ParseRequirements(runtimePath)
	if err != nil {
		t.Fatalf("ParseRequirements(runtime) error = %v, want nil", err)
	}
	rows := variableRows(t, payload)
	byName := rowsByName(rows)

	requests, ok := byName["requests"]
	if !ok {
		t.Fatalf("requests dependency missing from %#v", rows)
	}
	if got, want := requests["config_kind"], "dependency"; got != want {
		t.Fatalf("requests config_kind = %#v, want %q", got, want)
	}
	if got, want := requests["package_manager"], "pypi"; got != want {
		t.Fatalf("requests package_manager = %#v, want %q", got, want)
	}
	if got, want := requests["value"], "==2.31.0"; got != want {
		t.Fatalf("requests value = %#v, want pinned specifier %q", got, want)
	}
	if got, want := requests["section"], "requirements"; got != want {
		t.Fatalf("requests section = %#v, want %q", got, want)
	}
	if extras, ok := requests["extras"].([]string); !ok || !reflect.DeepEqual(extras, []string{"security"}) {
		t.Fatalf("requests extras = %#v, want [security]", requests["extras"])
	}
	if marker, ok := requests["marker"].(string); !ok || marker != "python_version >= '3.8'" {
		t.Fatalf("requests marker = %#v, want python_version >= '3.8'", requests["marker"])
	}
	if dev, _ := requests["dev_dependency"].(bool); dev {
		t.Fatalf("requests dev_dependency = true, want false for runtime requirements.txt")
	}

	django, ok := byName["Django"]
	if !ok {
		t.Fatalf("Django dependency missing")
	}
	if got, want := django["value"], ">=4.2,<5.0"; got != want {
		t.Fatalf("Django value = %#v, want range %q", got, want)
	}

	numpy, ok := byName["numpy"]
	if !ok {
		t.Fatalf("numpy dependency missing")
	}
	if got, want := numpy["value"], "~=1.26"; got != want {
		t.Fatalf("numpy value = %#v, want compatible-release %q", got, want)
	}

	// VCS, path, and URL forms must NOT be admitted as registry-version evidence.
	// They are preserved as separate provenance kinds so reducers can audit them
	// without inferring a pinned pip version.
	editable := findRowByConfigKind(rows, "editable_dependency")
	if editable == nil {
		t.Fatalf("expected an editable_dependency row for `-e ./local-package` in %#v", rows)
	}
	if got, want := editable["source_kind"], "path"; got != want {
		t.Fatalf("editable source_kind = %#v, want %q", got, want)
	}
	if got, want := editable["value"], "./local-package"; got != want {
		t.Fatalf("editable value = %#v, want path reference %q", got, want)
	}

	vcs := findRowByValueContains(rows, "github.com/psf/requests")
	if vcs == nil {
		t.Fatalf("expected a vcs row for git+https git URL in %#v", rows)
	}
	if got, want := vcs["config_kind"], "vcs_dependency"; got != want {
		t.Fatalf("vcs config_kind = %#v, want %q", got, want)
	}
	if got, want := vcs["source_kind"], "vcs"; got != want {
		t.Fatalf("vcs source_kind = %#v, want %q", got, want)
	}
	if got, want := vcs["name"], "requests-vcs"; got != want {
		t.Fatalf("vcs egg name = %#v, want %q (#egg= fragment)", got, want)
	}

	urlRow := findRowByValueContains(rows, "wheelhouse/foo-1.0")
	if urlRow == nil {
		t.Fatalf("expected url row for file:// wheel in %#v", rows)
	}
	if got, want := urlRow["config_kind"], "url_dependency"; got != want {
		t.Fatalf("url config_kind = %#v, want %q", got, want)
	}

	malformed := findRowByConfigKind(rows, "malformed_dependency")
	if malformed == nil {
		t.Fatalf("expected a malformed row for @@@ junk in %#v", rows)
	}
	if got, _ := malformed["malformed"].(bool); !got {
		t.Fatalf("malformed.malformed = %#v, want true", malformed["malformed"])
	}
	if got, _ := malformed["raw"].(string); got != "@@@ not a valid requirement" {
		t.Fatalf("malformed raw = %#v, want original line", malformed["raw"])
	}

	// Comments and pip flags must never produce dependency rows.
	for _, row := range rows {
		raw, _ := row["raw"].(string)
		if raw == "# top-level comment" || raw == "--hash=sha256:deadbeef ignored" {
			t.Fatalf("comment or flag emitted a row: %#v", row)
		}
	}
}

// TestParseRequirementsRecordsDevScopeFromFilename ensures requirements-dev.txt
// is tagged as a dev/test scope so the supply-chain reducer can decide whether
// to bound impact to runtime code only. The reducer does not yet read this
// field, but the source-of-truth payload must already preserve it so the
// reducer can graduate without a second parser change.
func TestParseRequirementsRecordsDevScopeFromFilename(t *testing.T) {
	t.Parallel()

	devPath := writeTempFile(t, "requirements-dev.txt", "pytest>=7.0\n")
	payload, err := ParseRequirements(devPath)
	if err != nil {
		t.Fatalf("ParseRequirements(dev) error = %v", err)
	}
	rows := variableRows(t, payload)
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}
	if got, _ := rows[0]["dev_dependency"].(bool); !got {
		t.Fatalf("dev_dependency = %#v, want true for requirements-dev.txt", rows[0]["dev_dependency"])
	}
	if got, want := rows[0]["section"], "requirements-dev"; got != want {
		t.Fatalf("section = %#v, want %q", got, want)
	}
}

// TestParseRequirementsKeepsPlainPathRequirementsAsPathNotEditable proves
// that a non-editable path requirement like `./libs/foo` reaches the row
// contract as `path_dependency`, not `editable_dependency`. Mixing the two
// would erase the distinction between an installed-from-disk dep and one
// that gets relinked through pip's `-e` editable install flow.
func TestParseRequirementsKeepsPlainPathRequirementsAsPathNotEditable(t *testing.T) {
	t.Parallel()

	path := writeTempFile(t, "requirements.txt", "./libs/foo\n")
	payload, err := ParseRequirements(path)
	if err != nil {
		t.Fatalf("ParseRequirements error = %v", err)
	}
	rows := variableRows(t, payload)
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}
	row := rows[0]
	if got, want := row["config_kind"], "path_dependency"; got != want {
		t.Fatalf("config_kind = %#v, want %q (plain `./libs/foo` is a non-editable path requirement)", got, want)
	}
	if got, want := row["source_kind"], "path"; got != want {
		t.Fatalf("source_kind = %#v, want %q", got, want)
	}
	if got, want := row["value"], "./libs/foo"; got != want {
		t.Fatalf("value = %#v, want %q", got, want)
	}
}

// TestParseRequirementsKeepsPEP508DirectReferencesOutOfRegistryEvidence proves
// that `name @ source` entries are provenance, not PyPI version evidence. Pip
// accepts these PEP 508 direct-reference forms, but the reducer must not admit
// them as public-registry consumption until a separate lockfile or registry
// row proves an installed PyPI version.
func TestParseRequirementsKeepsPEP508DirectReferencesOutOfRegistryEvidence(t *testing.T) {
	t.Parallel()

	path := writeTempFile(t, "requirements.txt", strings.Join([]string{
		"direct-url @ https://example.com/direct-url-1.0.0.tar.gz",
		"direct-tight@https://example.com/direct-tight-1.0.0.tar.gz",
		"direct-vcs @ git+https://github.com/acme/direct-vcs.git@v1.0.0",
		"direct-path @ ../vendor/direct-path",
	}, "\n"))
	payload, err := ParseRequirements(path)
	if err != nil {
		t.Fatalf("ParseRequirements error = %v", err)
	}
	rows := variableRows(t, payload)
	byName := rowsByName(rows)

	for _, tc := range []struct {
		name       string
		configKind string
		sourceKind string
	}{
		{name: "direct-url", configKind: "url_dependency", sourceKind: "url"},
		{name: "direct-tight", configKind: "url_dependency", sourceKind: "url"},
		{name: "direct-vcs", configKind: "vcs_dependency", sourceKind: "vcs"},
		{name: "direct-path", configKind: "path_dependency", sourceKind: "path"},
	} {
		row, ok := byName[tc.name]
		if !ok {
			t.Fatalf("%s missing from %#v", tc.name, rows)
		}
		if got := row["config_kind"]; got != tc.configKind {
			t.Fatalf("%s config_kind = %#v, want %q", tc.name, got, tc.configKind)
		}
		if got := row["source_kind"]; got != tc.sourceKind {
			t.Fatalf("%s source_kind = %#v, want %q", tc.name, got, tc.sourceKind)
		}
		if got := row["package_manager"]; got != "pypi" {
			t.Fatalf("%s package_manager = %#v, want pypi", tc.name, got)
		}
	}
}

// TestParseRequirementsHandlesEmptyFileWithoutPanicOrFakeRows guards the
// safety rule that an empty/whitespace-only requirements file must not produce
// dependency rows. Empty manifest evidence is not the same as "no
// dependencies."
func TestParseRequirementsHandlesEmptyFileWithoutPanicOrFakeRows(t *testing.T) {
	t.Parallel()

	path := writeTempFile(t, "requirements.txt", "\n\n# only comments\n\n")
	payload, err := ParseRequirements(path)
	if err != nil {
		t.Fatalf("ParseRequirements(empty) error = %v", err)
	}
	rows := variableRows(t, payload)
	if len(rows) != 0 {
		t.Fatalf("len(rows) = %d, want 0 for empty/comment-only file (got %#v)", len(rows), rows)
	}
}

func writeTempFile(t *testing.T, name string, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
	return path
}

func variableRows(t *testing.T, payload map[string]any) []map[string]any {
	t.Helper()
	rows, ok := payload["variables"].([]map[string]any)
	if !ok {
		t.Fatalf("payload.variables = %T, want []map[string]any", payload["variables"])
	}
	return rows
}

func rowsByName(rows []map[string]any) map[string]map[string]any {
	out := make(map[string]map[string]any, len(rows))
	for _, row := range rows {
		name, _ := row["name"].(string)
		if name == "" {
			continue
		}
		out[name] = row
	}
	return out
}

func findRowByConfigKind(rows []map[string]any, kind string) map[string]any {
	for _, row := range rows {
		if got, _ := row["config_kind"].(string); got == kind {
			return row
		}
	}
	return nil
}

func findRowByValueContains(rows []map[string]any, needle string) map[string]any {
	for _, row := range rows {
		value, _ := row["value"].(string)
		if value != "" && strings.Contains(value, needle) {
			return row
		}
	}
	return nil
}
