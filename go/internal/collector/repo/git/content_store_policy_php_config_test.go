// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
)

func TestPHPConfigMetadataPolicyKeepsSafeDirectiveWithoutBody(t *testing.T) {
	t.Parallel()
	const seed = "SeedPHPConfigToken987654321"
	for _, tc := range []struct{ path, body string }{
		{"php.ini", "memory_limit = 256M\npassword = " + seed + "\n"},
		{"conf.d/site.ini", "session.upload_progress.enabled = On\npassword = " + seed + "\n"},
		{".htaccess", "php_value memory_limit 256M\nphp_value password " + seed + "\n"},
		{"apache2/sites/site.conf", "LoadModule php_module modules/libphp.so\nphp_value password " + seed + "\n"},
		{"php-fpm.d/www.conf", "[www]\npassword = " + seed + "\n"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			t.Parallel()
			_, envelopes := snapshotWithPolicy(t, parser.DefaultRegistry(), tc.path, tc.body)
			if got := contentFact(t, envelopes, tc.path).Payload["content_body"]; got != "" {
				t.Fatalf("content body = %#v", got)
			}
			assertNoSeedInAnyFact(t, envelopes, seed)
			found := false
			for _, envelope := range envelopes {
				if envelope.FactKind != "content_entity" {
					continue
				}
				if envelope.Payload["relative_path"] != tc.path && envelope.Payload["content_path"] != tc.path {
					continue
				}
				found = true
				if envelope.Payload["source_cache"] != "" {
					t.Fatalf("source cache = %#v", envelope.Payload["source_cache"])
				}
			}
			if !found {
				t.Fatal("no configuration entity reached content facts")
			}
		})
	}
}
