package query

import (
	"strings"
	"testing"
)

func TestBuildDeadCodeGraphCypherPrefiltersDefaultPolicyExclusions(t *testing.T) {
	t.Parallel()

	cypher := buildDeadCodeGraphCypher(true, GraphBackendNornicDB)
	for _, want := range []string{
		"NOT (toLower(f.relative_path) CONTAINS '/test/'",
		"NOT (toLower(f.relative_path) CONTAINS '/gen/'",
		"coalesce(e.enclosing_function, '') = ''",
	} {
		if !strings.Contains(cypher, want) {
			t.Fatalf("dead-code cypher missing %q:\n%s", want, cypher)
		}
	}
}

func TestDeadCodeCandidateScanLimitUsesFullWindowForSmallDisplayLimits(t *testing.T) {
	t.Parallel()

	if got, want := deadCodeCandidateScanLimit(50), 10000; got != want {
		t.Fatalf("deadCodeCandidateScanLimit(50) = %d, want %d", got, want)
	}
}
