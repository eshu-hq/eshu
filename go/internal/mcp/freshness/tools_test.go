// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package freshnesstools

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"testing"
)

func TestToolsPreserveFreshnessRegistrationContract(t *testing.T) {
	t.Parallel()

	tools := Tools()
	wantNames := []string{
		"get_generation_lifecycle",
		"get_changed_since",
		"get_repository_freshness",
		"get_service_changed_since",
	}
	if got, want := len(tools), len(wantNames); got != want {
		t.Fatalf("freshness tool count = %d, want %d", got, want)
	}
	for i, want := range wantNames {
		if got := tools[i].Name; got != want {
			t.Fatalf("freshness tool %d name = %q, want %q", i, got, want)
		}
	}

	encoded, err := json.Marshal(tools)
	if err != nil {
		t.Fatalf("marshal freshness tools: %v", err)
	}
	const wantDefinitionsHash = "74856c947785a6a5dd0673c83a76fb302b8bb93e8802b74b49a9692484cdf945"
	if got := fmt.Sprintf("%x", sha256.Sum256(encoded)); got != wantDefinitionsHash {
		t.Fatalf("freshness tool definitions hash = %s, want %s", got, wantDefinitionsHash)
	}
}

func TestToolsReturnIndependentDefinitions(t *testing.T) {
	t.Parallel()

	first := Tools()
	second := Tools()
	if len(first) != 4 || len(second) != 4 {
		t.Fatalf("freshness tool counts = %d and %d, want 4 and 4", len(first), len(second))
	}
	encodedSecond, err := json.Marshal(second)
	if err != nil {
		t.Fatalf("marshal second freshness tool set: %v", err)
	}

	first[0].Name = "mutated"
	if second[0].Name == "mutated" {
		t.Fatal("freshness tool constructors share slice storage")
	}
	for i := range first {
		mutateFreshnessSchema(first[i].InputSchema)
	}

	encodedSecondAfterMutation, err := json.Marshal(second)
	if err != nil {
		t.Fatalf("marshal second freshness tool set after mutation: %v", err)
	}
	if !bytes.Equal(encodedSecondAfterMutation, encodedSecond) {
		t.Fatal("freshness tool constructors share nested schema storage")
	}
}

func mutateFreshnessSchema(value any) {
	switch typed := value.(type) {
	case map[string]any:
		for _, child := range typed {
			mutateFreshnessSchema(child)
		}
		typed["__mutation__"] = true
	case []any:
		for i := range typed {
			mutateFreshnessSchema(typed[i])
		}
		if len(typed) > 0 {
			typed[0] = "mutated"
		}
	case []string:
		if len(typed) > 0 {
			typed[0] = "mutated"
		}
	}
}
