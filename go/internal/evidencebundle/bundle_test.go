package evidencebundle

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBuildDemoBundleIsDeterministicAndValid(t *testing.T) {
	first := BuildDemoBundle(DemoBundleOptions{ScopeID: "repo:demo/service"})
	second := BuildDemoBundle(DemoBundleOptions{ScopeID: "repo:demo/service"})

	firstRaw := marshalBundleForTest(t, first)
	secondRaw := marshalBundleForTest(t, second)
	if string(firstRaw) != string(secondRaw) {
		t.Fatalf("BuildDemoBundle() is not deterministic:\nfirst=%s\nsecond=%s", firstRaw, secondRaw)
	}
	if err := Validate(first); err != nil {
		t.Fatalf("Validate(BuildDemoBundle()) error = %v", err)
	}
	for _, want := range []string{
		SchemaVersion,
		"supply_chain_impact",
		"pre_change_impact",
		"ask_eshu",
		"capability_catalog",
		"share_safe_v1",
		"reproduce",
	} {
		if !strings.Contains(string(firstRaw), want) {
			t.Fatalf("bundle missing %q:\n%s", want, firstRaw)
		}
	}
}

func TestValidateRejectsPrivateCanaries(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Bundle)
		want   string
	}{
		{
			name: "private endpoint",
			mutate: func(bundle *Bundle) {
				bundle.Reproduce[0].Target = "https://internal.example.invalid/api"
			},
			want: "private endpoint",
		},
		{
			name: "credential",
			mutate: func(bundle *Bundle) {
				bundle.Contents.AnswerPackets[0].Summary = "Authorization: Bearer secret"
			},
			want: "credential",
		},
		{
			name: "json token",
			mutate: func(bundle *Bundle) {
				bundle.Reproduce[0].Args["payload"] = `{"token":"ghp_example1234567890"}`
			},
			want: "credential",
		},
		{
			name: "raw prompt",
			mutate: func(bundle *Bundle) {
				bundle.Contents.AnswerPackets[0].EvidenceHandles = append(bundle.Contents.AnswerPackets[0].EvidenceHandles, "raw_prompt: explain request")
			},
			want: "raw prompt",
		},
		{
			name: "absolute path",
			mutate: func(bundle *Bundle) {
				bundle.Source.Repository = "/Users/example/private/repo"
			},
			want: "local absolute path",
		},
		{
			name: "workspace path",
			mutate: func(bundle *Bundle) {
				bundle.Source.Repository = "/workspace/eshu/private/repo"
			},
			want: "local absolute path",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bundle := BuildDemoBundle(DemoBundleOptions{ScopeID: "repo:demo/service"})
			tt.mutate(&bundle)
			err := Validate(bundle)
			if err == nil {
				t.Fatal("Validate() error = nil, want rejection")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Validate() error = %q, want substring %q", err, tt.want)
			}
		})
	}
}

func TestRenderJSONFullyOrdersDuplicatePacketSummaries(t *testing.T) {
	first := BuildDemoBundle(DemoBundleOptions{ScopeID: "repo:demo/service"})
	second := BuildDemoBundle(DemoBundleOptions{ScopeID: "repo:demo/service"})

	duplicatesA := []PacketSummary{
		{
			Family:          "ask_eshu",
			Schema:          "answer_packet.v1",
			TruthClass:      "derived",
			Summary:         "second answer",
			EvidenceHandles: []string{"handle:z", "handle:a"},
			NextCalls:       []string{"mcp:get_repo_context"},
		},
		{
			Family:          "ask_eshu",
			Schema:          "answer_packet.v1",
			TruthClass:      "authoritative",
			Summary:         "first answer",
			EvidenceHandles: []string{"handle:b"},
			NextCalls:       []string{"api:/api/v0/ask"},
		},
	}
	duplicatesB := []PacketSummary{duplicatesA[1], duplicatesA[0]}
	first.Contents.AnswerPackets = duplicatesA
	second.Contents.AnswerPackets = duplicatesB

	firstRaw := marshalBundleForTest(t, first)
	secondRaw := marshalBundleForTest(t, second)
	if string(firstRaw) != string(secondRaw) {
		t.Fatalf("RenderJSON() order differs for equivalent packet set:\nfirst=%s\nsecond=%s", firstRaw, secondRaw)
	}
}

func marshalBundleForTest(t *testing.T, bundle Bundle) []byte {
	t.Helper()
	raw, err := RenderJSON(bundle)
	if err != nil {
		t.Fatalf("RenderJSON() error = %v", err)
	}
	var decoded Bundle
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("RenderJSON() invalid JSON: %v\n%s", err, raw)
	}
	return raw
}
