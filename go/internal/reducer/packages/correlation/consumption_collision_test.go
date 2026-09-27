// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package correlation

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestBuildPackageConsumptionDecisionsRejectsCollidingRegistryIdentityInEitherOrder(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 9, 26, 0, 0, 0, 0, time.UTC)
	first := packageRegistryPackageFact("pypi://pypi.org/simple/requests-a", "pypi", "requests", "", observedAt)
	second := packageRegistryPackageFact("pypi://pypi.org/simple/requests-b", "pypi", "requests", "", observedAt)
	manifest := packageManifestDependencyFact("repo-app", "app", "requirements.txt", "requests", "python", "==2.0.0", observedAt)

	for _, envelopes := range [][]facts.Envelope{
		{first, second, manifest},
		{second, first, manifest},
	} {
		if decisions := BuildPackageConsumptionDecisions(envelopes); len(decisions) != 0 {
			t.Fatalf("BuildPackageConsumptionDecisions() = %#v, want no decisions for colliding registry identities", decisions)
		}
	}
}
