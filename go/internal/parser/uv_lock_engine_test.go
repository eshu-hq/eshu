// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"os"
	"path/filepath"
	"testing"
)

// TestEngineParsesUVLockThroughTheRegistry proves uv.lock is admitted by exact
// name, routed to the python_toml parser, and that only a PyPI-index package
// reaches the reducer as config_kind=dependency while git and private-index
// packages and a malformed entry do not.
func TestEngineParsesUVLockThroughTheRegistry(t *testing.T) {
	t.Parallel()

	definition, ok := DefaultRegistry().LookupByPath("services/api/uv.lock")
	if !ok || definition.ParserKey != "python_toml" {
		t.Fatalf("LookupByPath(uv.lock) = %#v, %v, want python_toml", definition, ok)
	}
	if _, ok := DefaultRegistry().LookupByPath("services/api/uv.lock.bak"); ok {
		t.Fatal("uv.lock.bak must not be admitted: the rule is an exact name")
	}

	body := "version = 1\n\n" +
		"[[package]]\nname = \"requests\"\nversion = \"2.31.0\"\nsource = { registry = \"https://pypi.org/simple\" }\n\n" +
		"[[package]]\nname = \"internal\"\nversion = \"1.0.0\"\nsource = { registry = \"https://index.internal.example/simple\" }\n\n" +
		"[[package]]\nname = \"forked\"\nversion = \"0.1.0\"\nsource = { git = \"https://github.com/acme/forked.git#0123456789abcdef0123456789abcdef01234567\" }\n\n" +
		"[[package]]\nname = \"odd\"\nversion = \"not a version\"\nsource = { registry = \"https://pypi.org/simple\" }\n"
	dir := t.TempDir()
	path := filepath.Join(dir, "uv.lock")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v", err)
	}
	payload, err := engine.ParsePath(dir, path, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v", err)
	}
	kinds := map[string]string{}
	rows, _ := payload["variables"].([]map[string]any)
	for _, row := range rows {
		kinds[row["name"].(string)], _ = row["config_kind"].(string)
	}
	want := map[string]string{
		"requests": "dependency",
		"internal": "url_dependency",
		"forked":   "vcs_dependency",
		"odd":      "malformed_dependency",
	}
	for name, kind := range want {
		if kinds[name] != kind {
			t.Fatalf("config_kind[%s] = %q, want %q (all: %#v)", name, kinds[name], kind, kinds)
		}
	}
	state, _ := payload["uv_lock_state"].(map[string]any)
	if state["state"] != "partial" || state["entries"] != 4 || state["resolved"] != 3 || state["unresolved"] != 1 {
		t.Fatalf("uv_lock_state = %#v, want partial 4 entries / 3 resolved / 1 unresolved", state)
	}
}
