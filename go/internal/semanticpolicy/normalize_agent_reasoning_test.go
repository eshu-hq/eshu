package semanticpolicy_test

import (
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/semanticpolicy"
	"github.com/eshu-hq/eshu/go/internal/semanticprofile"
)

// TestNormalizeAcceptsAgentReasoningSourceClass proves the policy validator
// recognizes the agent_reasoning source class. Without this, a deployment that
// governs an Ask Eshu provider profile through ESHU_SEMANTIC_EXTRACTION_POLICY_JSON
// (a rule or egress/denied source class) would fail policy validation before
// API/MCP startup, so the agent_reasoning profile NewAdapter requires could not
// be governed or enabled.
func TestNormalizeAcceptsAgentReasoningSourceClass(t *testing.T) {
	t.Parallel()
	// Enabled is false so Normalize exercises source-class validation
	// (normalizeSourceClasses -> isSupportedSourceClass) without also requiring a
	// full rule set; the same validation gates rule and egress source classes.
	policy := semanticpolicy.Policy{
		PolicyID:            "ask-eshu-policy",
		DeniedSourceClasses: []string{semanticprofile.SourceAgentReasoning},
	}
	out, err := semanticpolicy.Normalize(policy)
	if err != nil {
		t.Fatalf("Normalize() with agent_reasoning source class error = %v, want nil", err)
	}
	if !slices.Contains(out.DeniedSourceClasses, semanticprofile.SourceAgentReasoning) {
		t.Fatalf("agent_reasoning source class not preserved: %+v", out.DeniedSourceClasses)
	}
}
