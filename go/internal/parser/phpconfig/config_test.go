// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package phpconfig

import (
	"strings"
	"testing"
)

func TestParsePHPConfiguration(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, path, source                  string
		wantName, wantValue, wantMode       string
		wantLine, wantDropped, wantUnparsed int
	}{
		{"ini section", "conf.d/site.ini", "; comment\n[Session]\nsession.upload_progress.enabled = On\n", "session.upload_progress.enabled", "On", "", 3, 0, 0},
		{"htaccess", ".htaccess", "php_value memory_limit 256M\n", "memory_limit", "256M", "", 1, 0, 0},
		{"apache module", "apache2/sites-enabled/site.conf", "LoadModule php_module modules/libphp.so\n", "", "", "mod_php", 1, 0, 0},
		{"fpm pool", "php-fpm.d/www.conf", "[www]\nuser = www-data\n", "", "", "php-fpm", 1, 0, 0},
		{"fpm global only", "php-fpm.d/global.conf", "[global]\nerror_log = /tmp/fpm.log\n", "", "", "", 0, 0, 0},
		{"secret", "php.ini", "session.cookie_domain = token=abc123secret\n", "", "", "", 0, 1, 0},
		{"URL userinfo", "php.ini", "session.cookie_domain = https://admin:credential123@example.test\n", "", "", "", 0, 1, 0},
		{"inline comment", "php.ini", "memory_limit = 256M ; documented\n", "memory_limit", "256M", "", 1, 0, 0},
		{"malformed", "php.ini", "memory_limit = \"unterminated\n", "", "", "", 0, 0, 1},
		{"unsearchable name", "php.ini", "mémory_limit = 256M\n", "", "", "", 0, 0, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := Parse(tc.path, []byte(tc.source))
			if len(got.Directives) != 0 || tc.wantName != "" {
				if len(got.Directives) != 1 || got.Directives[0].Name != tc.wantName || got.Directives[0].Value != tc.wantValue || got.Directives[0].Line != tc.wantLine {
					t.Fatalf("directives = %#v", got.Directives)
				}
			}
			if len(got.Modes) != 0 || tc.wantMode != "" {
				if len(got.Modes) != 1 || got.Modes[0].Mode != tc.wantMode {
					t.Fatalf("modes = %#v", got.Modes)
				}
			}
			if got.Dropped != tc.wantDropped || got.Unparsed != tc.wantUnparsed {
				t.Fatalf("dropped/unparsed = %d/%d, want %d/%d", got.Dropped, got.Unparsed, tc.wantDropped, tc.wantUnparsed)
			}
		})
	}
}

func TestParsePHPConfigurationOverlongLineFailsClosed(t *testing.T) {
	t.Parallel()
	got := Parse("php.ini", []byte("memory_limit = 256M\n"+strings.Repeat("x", maxLineBytes+1)))
	if len(got.Directives) != 0 || len(got.Modes) != 0 || got.Unparsed == 0 {
		t.Fatalf("overlong file must fail closed: %#v", got)
	}
}

func TestParsePHPConfigurationDropsCredentialShapedSection(t *testing.T) {
	t.Parallel()
	const seed = "auth_token_SecretSection98765"
	got := Parse("php.ini", []byte("["+seed+"]\nmemory_limit = 256M\n[PHP]\nmax_execution_time = 30\n"))
	if got.Dropped != 1 || got.Unparsed != 1 || len(got.Directives) != 1 ||
		got.Directives[0].Name != "max_execution_time" || got.Directives[0].Section != "PHP" {
		t.Fatalf("invalid section must suppress child rows until a valid section: dropped=%d unparsed=%d directives=%d", got.Dropped, got.Unparsed, len(got.Directives))
	}
	for _, directive := range got.Directives {
		if strings.Contains(directive.Section, seed) {
			t.Fatal("secret section retained")
		}
	}
}

func TestParsePHPConfigurationMalformedSectionSuppressesChildRows(t *testing.T) {
	t.Parallel()
	got := Parse("php.ini", []byte("[broken\nmemory_limit = 256M\n[PHP]\nmax_execution_time = 30\n"))
	if got.Unparsed != 2 || len(got.Directives) != 1 ||
		got.Directives[0].Name != "max_execution_time" || got.Directives[0].Section != "PHP" {
		t.Fatalf("malformed section must suppress child rows until a valid section: unparsed=%d directives=%d", got.Unparsed, len(got.Directives))
	}
}

func TestParsePHPConfigurationDropsPassCredentialKeys(t *testing.T) {
	t.Parallel()
	const seed = "SeedSMTPPass987654321"
	for _, name := range []string{"smtp_pass", "smtp.pass", "smtp-pass", "smtpPass", "smtppass", "pass"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := Parse("php.ini", []byte(name+" = "+seed+"\nmemory_limit = 256M\n"))
			if got.Dropped != 1 || len(got.Directives) != 1 || got.Directives[0].Name != "memory_limit" {
				t.Fatalf("credential key must be dropped while safe directive remains: dropped=%d directives=%d", got.Dropped, len(got.Directives))
			}
		})
	}
}
