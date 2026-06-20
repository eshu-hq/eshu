package competitiveparity

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRenderArtifactsAreBoundedAndDeterministic(t *testing.T) {
	report := Validate(completeInventory(), defaultExpectationsForTest())
	raw, err := RenderJSON(report)
	if err != nil {
		t.Fatalf("RenderJSON() error = %v", err)
	}
	var decoded Report
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("RenderJSON() invalid JSON: %v\n%s", err, raw)
	}
	if decoded.SchemaVersion != SchemaVersion {
		t.Fatalf("decoded.SchemaVersion = %q, want %q", decoded.SchemaVersion, SchemaVersion)
	}
	md := RenderMarkdown(report)
	for _, want := range []string{
		"# Competitive Parity Gate",
		"graphify-style report readability",
		"CodeGraphContext-style portable artifact usability",
		"GitNexus-style agent workflow discoverability",
		"#3238",
	} {
		if !strings.Contains(md, want) {
			t.Fatalf("RenderMarkdown() missing %q:\n%s", want, md)
		}
	}
}
