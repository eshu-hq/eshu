// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPHPConfigAdmissionAndSafeParserOutput(t *testing.T) {
	t.Parallel()
	registry := DefaultRegistry()
	for _, path := range []string{"php.ini", "conf.d/site.ini", ".htaccess", "php-fpm.d/www.conf", "apache2/sites-enabled/site.conf"} {
		def, ok := registry.LookupByPath(path)
		if !ok || def.ParserKey != "php_config" || def.ContentStore.Policy != ContentStoreMetadata {
			t.Errorf("%s: definition = %#v, %t", path, def, ok)
		}
	}
	if def, ok := registry.LookupByPath("application.conf"); !ok || def.ParserKey != "raw_text" {
		t.Fatalf("unrelated conf definition = %#v, %t", def, ok)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "php.ini")
	if err := os.WriteFile(path, []byte("memory_limit = 256M\npassword = secret-abc\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	engine, err := DefaultEngine()
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := engine.ParsePath(dir, path, false, Options{IndexSource: true})
	if err != nil {
		t.Fatal(err)
	}
	rows, ok := parsed["variables"].([]map[string]any)
	if !ok || len(rows) != 1 || rows[0]["name"] != "php_directive:memory_limit" || rows[0]["value"] != "256M" || rows[0]["source_cache_withheld"] != true {
		t.Fatalf("safe rows = %#v", parsed["variables"])
	}
	if _, ok := parsed["source"]; ok {
		t.Fatal("metadata-only config body leaked")
	}
	if parsed["php_config_dropped"] != 1 {
		t.Fatalf("drop count = %#v", parsed["php_config_dropped"])
	}
}

func TestPHPFPMBehindNginxConfiguration(t *testing.T) {
	t.Parallel()
	registry := DefaultRegistry()
	if nginx, ok := registry.LookupByPath("nginx/site.conf"); !ok || nginx.ParserKey != "raw_text" {
		t.Fatalf("nginx config dispatch = %#v, %t", nginx, ok)
	}
	if fpm, ok := registry.LookupByPath("php-fpm.d/www.conf"); !ok || fpm.ParserKey != "php_config" {
		t.Fatalf("FPM pool dispatch = %#v, %t", fpm, ok)
	}
	root := t.TempDir()
	fpmPath := filepath.Join(root, "php-fpm.d", "www.conf")
	if err := os.MkdirAll(filepath.Dir(fpmPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fpmPath, []byte("[www]\nlisten = 127.0.0.1:9000\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	engine, err := DefaultEngine()
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := engine.ParsePath(root, fpmPath, false, Options{})
	if err != nil {
		t.Fatal(err)
	}
	rows, _ := parsed["variables"].([]map[string]any)
	if len(rows) != 1 || rows[0]["name"] != "php_server_mode:php-fpm" || rows[0]["line_number"] != 1 {
		t.Fatalf("FPM rows = %#v", rows)
	}
}
