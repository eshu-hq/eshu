// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"regexp"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/vulnsource"
)

// TestReadinessSourceScopeTableAgreesWithSharedSourceParser keeps the
// readiness reader's scope-id to source table and the shared parser the
// extension host writes with in lockstep (#9420). Every literal
// 'vuln-intel://...' AS scope_id, '<source>' AS source pair in the shipped
// query must parse to the same source, and every scope-less source arm
// (matched by source and ecosystem) must be a source the parser can emit.
func TestReadinessSourceScopeTableAgreesWithSharedSourceParser(t *testing.T) {
	t.Parallel()

	literal := regexp.MustCompile(`'(vuln-intel://[^']*)'(?:\s*\|\|\s*TRIM\(\$9\))? AS scope_id, '([a-z_]+)' AS source`)
	pairs := literal.FindAllStringSubmatch(ListReadinessQuery, -1)
	if len(pairs) < 4 {
		t.Fatalf("found %d literal scope/source pairs in ListReadinessQuery, want at least 4 (nvd/cve, nvd/<cve>, cisa/kev, first/epss)", len(pairs))
	}
	for _, pair := range pairs {
		scopeID, want := pair[1], pair[2]
		if scopeID == "vuln-intel://nvd/" {
			scopeID += "CVE-2021-23337"
		}
		got, ok := vulnsource.SourceForScopeID(scopeID)
		if !ok || got != want {
			t.Errorf("SourceForScopeID(%q) = (%q, %t), readiness maps it to %q", scopeID, got, ok, want)
		}
	}

	ecosystemArm := regexp.MustCompile(`NULL::text AS scope_id, '([a-z_]+)' AS source, ecosystem`)
	arms := ecosystemArm.FindAllStringSubmatch(ListReadinessQuery, -1)
	if len(arms) < 2 {
		t.Fatalf("found %d ecosystem-matched source arms, want at least 2 (osv, glad)", len(arms))
	}
	for _, arm := range arms {
		scopeID := "vuln-intel://" + arm[1] + "/npm"
		if got, ok := vulnsource.SourceForScopeID(scopeID); !ok || got != arm[1] {
			t.Errorf("SourceForScopeID(%q) = (%q, %t), readiness matches source %q by ecosystem", scopeID, got, ok, arm[1])
		}
	}
}
