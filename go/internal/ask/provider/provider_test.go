package provider

import "testing"

func TestTokenUsageTotal(t *testing.T) {
	t.Parallel()
	if got := (TokenUsage{InputTokens: 10, OutputTokens: 7}).Total(); got != 17 {
		t.Fatalf("Total = %d, want 17", got)
	}
}

func TestRoleConstants(t *testing.T) {
	t.Parallel()
	if RoleSystem != "system" || RoleUser != "user" || RoleAssistant != "assistant" || RoleTool != "tool" {
		t.Fatalf("role drift: %q %q %q %q", RoleSystem, RoleUser, RoleAssistant, RoleTool)
	}
}
