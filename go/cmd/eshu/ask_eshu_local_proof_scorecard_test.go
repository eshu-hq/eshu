package main

import (
	"bytes"
	"strings"
	"testing"
)

// TestAskEshuLocalProofScorecardFixturePasses proves the committed redacted
// answer-quality scorecard fixture used by the Ask Eshu local proof
// (scripts/verify-ask-eshu-local-proof.sh) passes the scorer through the real
// CLI command. The fixture is share-safe: it contains no private paths,
// hostnames, credentials, or raw addresses, and it covers all seven prompt
// families required by the scorecard.
func TestAskEshuLocalProofScorecardFixturePasses(t *testing.T) {
	cmd := newAnswerQualityScorecardCommand()
	cmd.SetArgs([]string{"--from", "testdata/ask-eshu-local-proof-scorecard.json", "--json"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("scorecard command returned error for committed fixture: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), `"pass": true`) {
		t.Fatalf("committed fixture did not pass the scorecard:\n%s", out.String())
	}
}
