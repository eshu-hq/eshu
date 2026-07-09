// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import "testing"

// TestAdminCredentialFromEnvTrimsInlinePassword proves ESHU_ADMIN_PASSWORD is
// trimmed the same way ESHU_ADMIN_PASSWORD_FILE content already is: a
// trailing newline or leading/trailing whitespace in the inline env value
// (common when the value is piped in from a shell heredoc or a secrets
// manager that appends "\n") must not silently become part of the password,
// or the operator-supplied credential would never match what they typed.
func TestAdminCredentialFromEnvTrimsInlinePassword(t *testing.T) {
	getenv := testGetenv(map[string]string{
		"ESHU_ADMIN_USERNAME": "operator",
		"ESHU_ADMIN_PASSWORD": "correct-horse-battery-staple\n",
	})
	username, password, ok := adminCredentialFromEnv(getenv)
	if !ok {
		t.Fatalf("adminCredentialFromEnv() ok = false, want true (username=%q password=%q)", username, password)
	}
	if username != "operator" {
		t.Fatalf("adminCredentialFromEnv() username = %q, want %q", username, "operator")
	}
	if password != "correct-horse-battery-staple" {
		t.Fatalf("adminCredentialFromEnv() password = %q, want trimmed %q", password, "correct-horse-battery-staple")
	}
}

// TestAdminCredentialFromEnvRequiresBothUsernameAndPassword proves ok is
// false unless both resolve to a non-empty value, even after trimming (a
// password that is only whitespace must not count as "set").
func TestAdminCredentialFromEnvRequiresBothUsernameAndPassword(t *testing.T) {
	tests := map[string]map[string]string{
		"missing password": {"ESHU_ADMIN_USERNAME": "operator"},
		"missing username": {"ESHU_ADMIN_PASSWORD": "correct-horse-battery-staple"},
		"whitespace-only password": {
			"ESHU_ADMIN_USERNAME": "operator",
			"ESHU_ADMIN_PASSWORD": "   ",
		},
	}
	for name, env := range tests {
		t.Run(name, func(t *testing.T) {
			_, _, ok := adminCredentialFromEnv(testGetenv(env))
			if ok {
				t.Fatalf("adminCredentialFromEnv() ok = true, want false for %s", name)
			}
		})
	}
}
