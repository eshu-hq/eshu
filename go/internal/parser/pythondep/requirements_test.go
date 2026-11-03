// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package pythondep

import (
	"fmt"
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

// requirementsUserinfoSecret builds a credential-shaped value by
// concatenation so no source line looks like a committed secret to push
// protection.
func requirementsUserinfoSecret() string {
	return "tok" + "en-" + "value-9f3a"
}

// TestParseRequirementsRefusesAmbiguousVCSUserinfo proves a VCS line whose
// password holds a raw "/" is refused instead of cleaned (#9074 P2-1): the
// authority cut at the first "/" leaves "user:pa" with no "@", so the
// "SECRET@host" residue in the path cannot be told from a host and the row
// keeps its kind but loses its text, both bare and direct-reference shaped.
func TestParseRequirementsRefusesAmbiguousVCSUserinfo(t *testing.T) {
	t.Parallel()

	secret := requirementsUserinfoSecret()
	path := writeTempFile(t, "requirements.txt", strings.Join([]string{
		"git+https://ci-user:" + "pa/" + secret + "@files.example.com/repo@v1#egg=ambig-bare",
		"ambig-ref @ git+https://ci-user:" + "pa/" + secret + "@files.example.com/repo@feature/foo",
	}, "\n"))
	payload, err := ParseRequirements(path)
	if err != nil {
		t.Fatalf("ParseRequirements error = %v", err)
	}
	if text := fmt.Sprintf("%#v", payload); strings.Contains(text, secret) || strings.Contains(text, "ci-user") {
		t.Fatalf("payload carries credential text: %s", text)
	}
	rows := variableRows(t, payload)
	if len(rows) != 2 {
		t.Fatalf("len(rows) = %d, want 2: %#v", len(rows), rows)
	}
	for _, row := range rows {
		if got := row["config_kind"]; got != "vcs_dependency" {
			t.Errorf("config_kind = %#v, want vcs_dependency", got)
		}
		for _, field := range []string{"value", "source_url", "raw"} {
			if got, _ := row[field].(string); got != "" {
				t.Errorf("%s = %q, want it dropped", field, got)
			}
		}
		if row["source_cache_withheld"] != true {
			t.Errorf("source_cache_withheld = %#v, want true", row["source_cache_withheld"])
		}
	}
	if name, _ := rows[1]["name"].(string); name != "ambig-ref" {
		t.Errorf("direct-ref name = %q, want the left-hand name restored", name)
	}
}

// TestParseRequirementsDropsExtrasOnDemotedRows proves a demoted row keeps no
// extras: a PEP 508 extra can never legitimately hold "@" or "://", so a pin
// whose extra does is not the plain pin it parses as and demotes, and a
// marker-demoted direct reference drops its left-hand extras with everything
// else (#9074 P3-1).
func TestParseRequirementsDropsExtrasOnDemotedRows(t *testing.T) {
	t.Parallel()

	path := writeTempFile(t, "requirements.txt", strings.Join([]string{
		"smuggled[user@hostx]>=1",
		"marked[extra1] @ https://files.example.com/marked-1.0.tar.gz ; os_name == 'a@b'",
		"plain[extra2]==3.0",
	}, "\n"))
	payload, err := ParseRequirements(path)
	if err != nil {
		t.Fatalf("ParseRequirements error = %v", err)
	}
	rows := variableRows(t, payload)
	if len(rows) != 3 {
		t.Fatalf("len(rows) = %d, want 3: %#v", len(rows), rows)
	}
	for _, row := range []*map[string]any{&rows[0], &rows[1]} {
		if got := (*row)["config_kind"]; got != "malformed_dependency" {
			t.Errorf("config_kind = %#v, want malformed_dependency", got)
		}
		if extras, _ := (*row)["extras"].([]string); len(extras) != 0 {
			t.Errorf("extras = %#v, want them dropped", (*row)["extras"])
		}
		if _, present := (*row)["extras"]; present {
			t.Errorf("extras key present = %#v, want it absent", (*row)["extras"])
		}
	}
	plain := rows[2]
	if got := plain["config_kind"]; got != "dependency" {
		t.Errorf("plain config_kind = %#v, want dependency", got)
	}
	if extras, _ := plain["extras"].([]string); len(extras) != 1 || extras[0] != "extra2" {
		t.Errorf("plain extras = %#v, want [extra2] kept", plain["extras"])
	}
}

// TestParseRequirementsStripsUserinfoKeepsHostNameAndRefs proves a
// requirements URL or VCS row never persists userinfo: value, source_url,
// and raw keep the host, path, VCS ref, and package name with the
// `user[:password]@` component cut, and the row withholds its source_cache
// so the file's raw line range cannot reintroduce it (#9074). VCS refs such
// as @v1 and branches containing / survive the cut.
func TestParseRequirementsStripsUserinfoKeepsHostNameAndRefs(t *testing.T) {
	t.Parallel()

	secret := requirementsUserinfoSecret()
	path := writeTempFile(t, "requirements.txt", strings.Join([]string{
		"privpkg @ https://ci-user:" + secret + "@host.example/p.whl",
		"git+https://ci-user:" + secret + "@git.example.com/acme/privvcs.git@v1.0.0#egg=privvcs",
		"git+https://ci-user:" + secret + "@git.example.com/acme/privbranch.git@feature/foo#egg=privbranch",
		"https://ci-user:" + secret + "@files.example.com/bare-1.0.tar.gz",
	}, "\n"))
	payload, err := ParseRequirements(path)
	if err != nil {
		t.Fatalf("ParseRequirements error = %v", err)
	}
	if text := fmt.Sprintf("%#v", payload); strings.Contains(text, secret) || strings.Contains(text, "ci-user") {
		t.Fatalf("payload carries credential text: %s", text)
	}
	rows := variableRows(t, payload)
	if len(rows) != 4 {
		t.Fatalf("len(rows) = %d, want 4: %#v", len(rows), rows)
	}
	byName := rowsByName(rows)
	for name, kind := range map[string]string{
		"privpkg":    "url_dependency",
		"privvcs":    "vcs_dependency",
		"privbranch": "vcs_dependency",
	} {
		row, ok := byName[name]
		if !ok {
			t.Fatalf("%s missing from %#v", name, rows)
		}
		if got := row["config_kind"]; got != kind {
			t.Errorf("%s config_kind = %#v, want %q", name, got, kind)
		}
		for _, field := range []string{"value", "source_url", "raw"} {
			got, _ := row[field].(string)
			if got == "" {
				t.Errorf("%s %s is empty, want the scrubbed line kept", name, field)
			}
			if strings.Contains(got, "@ci-user") || strings.Contains(got, secret) {
				t.Errorf("%s %s = %q, want no userinfo", name, field, got)
			}
		}
		if row["source_cache_withheld"] != true {
			t.Errorf("%s source_cache_withheld = %#v, want true: the file line range still holds the raw line", name, row["source_cache_withheld"])
		}
	}
	if got, _ := byName["privpkg"]["value"].(string); got != "https://host.example/p.whl" {
		t.Errorf("privpkg value = %q, want host and path kept", got)
	}
	if got, _ := byName["privvcs"]["value"].(string); got != "git+https://git.example.com/acme/privvcs.git@v1.0.0#egg=privvcs" {
		t.Errorf("privvcs value = %q, want ref and fragment kept", got)
	}
	if got, _ := byName["privbranch"]["value"].(string); got != "git+https://git.example.com/acme/privbranch.git@feature/foo#egg=privbranch" {
		t.Errorf("privbranch value = %q, want slash branch kept", got)
	}
	if got, _ := byName["privbranch"]["source_url"].(string); got != "git+https://git.example.com/acme/privbranch.git@feature/foo" {
		t.Errorf("privbranch source_url = %q, want fragment stripped, ref kept", got)
	}
	bare := findRowByValueContains(rows, "files.example.com")
	if bare == nil {
		t.Fatalf("expected a url row for the bare wheel URL in %#v", rows)
	}
	if got := bare["config_kind"]; got != "url_dependency" {
		t.Errorf("bare url config_kind = %#v, want url_dependency", got)
	}
	if name, _ := bare["name"].(string); name != "" {
		t.Errorf("bare url name = %q, want empty (no egg fragment)", name)
	}
	if got, _ := bare["value"].(string); got != "https://files.example.com/bare-1.0.tar.gz" {
		t.Errorf("bare url value = %q, want host and path kept", got)
	}
}

// TestParseRequirementsRefusesUncleanableSourceText proves a requirements
// line that cannot be scrubbed reliably keeps its kind but loses its text: a
// password with a raw "/" moves the real userinfo past the authority cut, and
// a scheme-less "user@host:path" line cannot separate userinfo from a host.
// Both rows withhold their source_cache (#9074).
func TestParseRequirementsRefusesUncleanableSourceText(t *testing.T) {
	t.Parallel()

	secret := requirementsUserinfoSecret()
	path := writeTempFile(t, "requirements.txt", strings.Join([]string{
		"slash-url @ https://ci-user:" + "pa/" + secret + "@files.example.com/slash-url-1.0.tar.gz",
		"bare-user:" + secret + "@host.example.com/pkg",
	}, "\n"))
	payload, err := ParseRequirements(path)
	if err != nil {
		t.Fatalf("ParseRequirements error = %v", err)
	}
	if text := fmt.Sprintf("%#v", payload); strings.Contains(text, secret) || strings.Contains(text, "ci-user") {
		t.Fatalf("payload carries credential text: %s", text)
	}
	rows := variableRows(t, payload)
	if len(rows) != 2 {
		t.Fatalf("len(rows) = %d, want 2: %#v", len(rows), rows)
	}
	slash, ok := rowsByName(rows)["slash-url"]
	if !ok {
		t.Fatalf("slash-url missing from %#v", rows)
	}
	if got := slash["config_kind"]; got != "url_dependency" {
		t.Errorf("slash-url config_kind = %#v, want url_dependency", got)
	}
	for _, field := range []string{"value", "source_url", "raw"} {
		if got, _ := slash[field].(string); got != "" {
			t.Errorf("slash-url %s = %q, want it dropped", field, got)
		}
	}
	if slash["source_cache_withheld"] != true {
		t.Errorf("slash-url source_cache_withheld = %#v, want true", slash["source_cache_withheld"])
	}
	bare := rows[1]
	if got := bare["config_kind"]; got != "malformed_dependency" {
		t.Errorf("scheme-less userinfo line config_kind = %#v, want malformed_dependency", got)
	}
	if name, _ := bare["name"].(string); name != "" {
		t.Errorf("scheme-less userinfo line name = %q, want it dropped", name)
	}
	for _, field := range []string{"value", "raw"} {
		if got, _ := bare[field].(string); got != "" {
			t.Errorf("scheme-less userinfo line %s = %q, want it dropped", field, got)
		}
	}
}

// TestParseRequirementsDemotesUnsafeMarkers proves a requirements row whose
// marker holds URL-shaped text is demoted to a nameless, valueless malformed
// row — never left with the marker merely cleared, which would collapse two
// lines differing only in their markers onto one content identity (#9074).
func TestParseRequirementsDemotesUnsafeMarkers(t *testing.T) {
	t.Parallel()

	secret := requirementsUserinfoSecret()
	path := writeTempFile(t, "requirements.txt", strings.Join([]string{
		"marked==2.0 ; python_version > '3' and platform_node == 'https://ci-user:" + secret + "@m.example'",
		"plain==3.0 ; python_version >= '3.9'",
	}, "\n"))
	payload, err := ParseRequirements(path)
	if err != nil {
		t.Fatalf("ParseRequirements error = %v", err)
	}
	if text := fmt.Sprintf("%#v", payload); strings.Contains(text, secret) {
		t.Fatalf("payload carries credential text: %s", text)
	}
	rows := variableRows(t, payload)
	if len(rows) != 2 {
		t.Fatalf("len(rows) = %d, want 2: %#v", len(rows), rows)
	}
	marked := rows[0]
	if got := marked["config_kind"]; got != "malformed_dependency" {
		t.Errorf("marked config_kind = %#v, want malformed_dependency", got)
	}
	if name, _ := marked["name"].(string); name != "" {
		t.Errorf("marked name = %q, want it dropped", name)
	}
	for _, field := range []string{"value", "marker", "raw"} {
		if got, _ := marked[field].(string); got != "" {
			t.Errorf("marked %s = %q, want it dropped", field, got)
		}
	}
	plain := rows[1]
	if got := plain["marker"]; got != "python_version >= '3.9'" {
		t.Errorf("plain marker = %#v, want the PEP 508 marker kept", got)
	}
	if _, withheld := plain["source_cache_withheld"]; withheld {
		t.Errorf("plain requirements pin sets source_cache_withheld: %#v", plain)
	}
}

// TestParseRequirementsNormalizesURLRawToParsedLine proves a requirements URL
// or VCS row stores the trimmed parsed line as raw, never the scanner's raw
// line: a trailing inline comment can carry URL text the row scrubbed.
func TestParseRequirementsNormalizesURLRawToParsedLine(t *testing.T) {
	t.Parallel()

	path := writeTempFile(t, "requirements.txt",
		"cleanpkg @ https://host.example/clean-1.0.tar.gz # internal mirror\n")
	payload, err := ParseRequirements(path)
	if err != nil {
		t.Fatalf("ParseRequirements error = %v", err)
	}
	rows := variableRows(t, payload)
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1: %#v", len(rows), rows)
	}
	row := rows[0]
	if got, _ := row["raw"].(string); got != "cleanpkg @ https://host.example/clean-1.0.tar.gz" {
		t.Errorf("raw = %#v, want the trimmed parsed line without the comment", got)
	}
	if _, withheld := row["source_cache_withheld"]; withheld {
		t.Errorf("clean url row sets source_cache_withheld: %#v", row)
	}
}
