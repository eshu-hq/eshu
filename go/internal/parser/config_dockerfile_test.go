// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDockerfilePHPServerModeEvidence(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, source, wantMode string
	}{
		{"apache base", "FROM php:8.4-apache\n", "mod_php"},
		{"fpm base", "FROM php:8.4-fpm\n", "php-fpm"},
		{"apache distro base", "FROM php:8.4-apache-bookworm\n", "mod_php"},
		{"fpm distro base", "FROM php:8.4-fpm-alpine\n", "php-fpm"},
		{"unresolved apache suffix", "FROM php:${PHP_TAG}-apache\n", ""},
		{"ambiguous tag", "FROM php:8.4-apache-fpm\n", ""},
		{"unresolved base", "FROM php:${PHP_TAG}\n", ""},
		{"non-PHP base", "FROM alpine:3.20\n", ""},
		{"builder only", "FROM php:8.4-apache AS builder\nFROM alpine:3.20\n", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			path := filepath.Join(dir, "Dockerfile")
			if err := os.WriteFile(path, []byte(tc.source), 0o600); err != nil {
				t.Fatal(err)
			}
			engine, err := DefaultEngine()
			if err != nil {
				t.Fatal(err)
			}
			got, err := engine.ParsePath(dir, path, false, Options{})
			if err != nil {
				t.Fatal(err)
			}
			rows, _ := got["variables"].([]map[string]any)
			if tc.wantMode == "" {
				if len(rows) != 0 {
					t.Fatalf("unexpected mode rows: %#v", rows)
				}
				return
			}
			if len(rows) != 1 || rows[0]["name"] != "php_server_mode:"+tc.wantMode || rows[0]["line_number"] != 1 {
				t.Fatalf("mode rows = %#v", rows)
			}
		})
	}
}

func TestDockerfileA2EnmodConsumesNormalizedInstallRows(t *testing.T) {
	t.Parallel()
	metadata := map[string]any{
		"dockerfile_stages":   []map[string]any{{"name": "runtime", "stage_index": 0, "base_image": "debian", "base_tag": "12"}},
		"dockerfile_installs": []map[string]any{{"manager": "a2enmod", "name": "php8.2", "stage": "runtime", "line_number": 3}},
	}
	rows := phpDockerfileModeRows(metadata)
	if len(rows) != 1 || rows[0]["name"] != "php_server_mode:mod_php" || rows[0]["line_number"] != 3 {
		t.Fatalf("mode rows = %#v", rows)
	}
}

func TestDockerfilePHPModeRejectsUnrelatedAndNonRuntimeA2Enmod(t *testing.T) {
	t.Parallel()
	metadata := map[string]any{
		"dockerfile_stages": []map[string]any{
			{"name": "builder", "stage_index": 0, "base_image": "debian", "base_tag": "12"},
			{"name": "runtime", "stage_index": 1, "base_image": "debian", "base_tag": "12"},
		},
		"dockerfile_installs": []map[string]any{
			{"manager": "a2enmod", "name": "php8.2", "stage": "builder", "line_number": 2},
			{"manager": "a2enmod", "name": "phpmyadmin", "stage": "runtime", "line_number": 4},
			{"manager": "docker-php-ext-enable", "name": "php8.2", "stage": "runtime", "line_number": 5},
		},
	}
	if rows := phpDockerfileModeRows(metadata); len(rows) != 0 {
		t.Fatalf("unrelated/non-runtime modules claimed mod_php: %#v", rows)
	}
}

func TestDockerfilePHPModePreservesConflictingDeclarations(t *testing.T) {
	t.Parallel()
	metadata := map[string]any{
		"dockerfile_stages":   []map[string]any{{"name": "runtime", "stage_index": 0, "base_image": "php", "base_tag": "8.4-fpm", "line_number": 1}},
		"dockerfile_installs": []map[string]any{{"manager": "a2enmod", "name": "php8.2", "stage": "runtime", "line_number": 3}},
	}
	rows := phpDockerfileModeRows(metadata)
	if len(rows) != 2 || rows[0]["name"] != "php_server_mode:php-fpm" || rows[1]["name"] != "php_server_mode:mod_php" || rows[1]["source_kind"] != "a2enmod" {
		t.Fatalf("conflicting evidence = %#v", rows)
	}
}

func TestDockerfilePHPModeConsumesRealNormalizedRUNRows(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, source string
		wantLine     int
	}{
		{"final stage module", "FROM debian:12 AS builder\nRUN a2enmod php8.2\nFROM debian:12 AS runtime\nRUN a2enmod phpmyadmin\nRUN docker-php-ext-enable mysqli\nRUN a2enmod php8.3\n", 6},
		{"builder only", "FROM debian:12 AS builder\nRUN a2enmod php8.2\nFROM alpine:3.20 AS runtime\n", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			path := filepath.Join(root, "Dockerfile")
			if err := os.WriteFile(path, []byte(tc.source), 0o600); err != nil {
				t.Fatal(err)
			}
			engine, err := DefaultEngine()
			if err != nil {
				t.Fatal(err)
			}
			parsed, err := engine.ParsePath(root, path, false, Options{})
			if err != nil {
				t.Fatal(err)
			}
			installs, _ := parsed["dockerfile_installs"].([]map[string]any)
			foundNormalized := false
			for _, install := range installs {
				if install["manager"] == "a2enmod" && install["name"] == "php8.2" && install["stage"] == "builder" && install["line_number"] == 2 {
					foundNormalized = true
				}
			}
			if !foundNormalized {
				t.Fatal("shared Dockerfile parser did not emit builder a2enmod evidence")
			}
			rows, _ := parsed["variables"].([]map[string]any)
			if tc.wantLine == 0 {
				if len(rows) != 0 {
					t.Fatalf("builder-only module created runtime mode: count=%d", len(rows))
				}
				return
			}
			if len(rows) != 1 || rows[0]["name"] != "php_server_mode:mod_php" || rows[0]["source_kind"] != "a2enmod" || rows[0]["stage"] != "runtime" || rows[0]["line_number"] != tc.wantLine {
				t.Fatalf("final-stage mode provenance mismatch: count=%d", len(rows))
			}
		})
	}
}
