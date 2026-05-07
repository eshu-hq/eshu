package postgres

import (
	"strings"
	"testing"
)

func TestProjectorClaimSupersedesStaleTerminalGenerations(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"stale.status IN ('pending', 'retrying', 'failed', 'dead_letter')",
		"stale_generation.status IN ('pending', 'failed')",
		"generation.status IN ('pending', 'failed')",
	} {
		if !strings.Contains(claimProjectorWorkQuery, want) {
			t.Fatalf("claimProjectorWorkQuery missing %q:\n%s", want, claimProjectorWorkQuery)
		}
	}
}
