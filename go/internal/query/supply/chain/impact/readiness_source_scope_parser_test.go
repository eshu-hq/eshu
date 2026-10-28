// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"regexp"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/vulnsource"
)

// TestReadinessSourceTokensAgreeWithSharedSourceParser keeps the source tokens
// the readiness reader accepts from collector configuration and the shared
// parser the extension host writes with in lockstep (#9420). Readiness matches
// a target's configured source against the state row's source, and the host
// recorder writes that column from the scope id, so every source token the
// reader accepts must be the token the parser derives from the scope grammar
// the coordinator and the configuration use.
func TestReadinessSourceTokensAgreeWithSharedSourceParser(t *testing.T) {
	t.Parallel()

	accepted := regexp.MustCompile(`TRIM\(target->>'source'\) NOT IN \(([^)]*)\)`).FindStringSubmatch(ListReadinessQuery)
	if accepted == nil {
		t.Fatal("accepted source list not found in ListReadinessQuery")
	}
	tokens := regexp.MustCompile(`'([a-z_]+)'`).FindAllStringSubmatch(accepted[1], -1)
	if len(tokens) < 4 {
		t.Fatalf("found %d accepted source tokens, want at least 4 (nvd, first_epss, cisa_kev, osv)", len(tokens))
	}
	scopeFor := map[string]string{
		"nvd":        "vuln-intel://nvd/cve",
		"first_epss": "vuln-intel://first/epss",
		"cisa_kev":   "vuln-intel://cisa/kev",
		"osv":        "vuln-intel://osv/npm",
		"glad":       "vuln-intel://glad/npm",
	}
	for _, token := range tokens {
		scopeID, known := scopeFor[token[1]]
		if !known {
			t.Errorf("readiness accepts source %q, which has no scope grammar in this lockstep table", token[1])
			continue
		}
		if got, ok := vulnsource.SourceForScopeID(scopeID); !ok || got != token[1] {
			t.Errorf("SourceForScopeID(%q) = (%q, %t), readiness accepts source %q", scopeID, got, ok, token[1])
		}
	}
}
