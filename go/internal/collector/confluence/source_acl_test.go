package confluence

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestSourceEmitsPartialSourceACLStateOnContentFacts asserts that Confluence
// content/evidence facts carry the bounded source_acl_state. Confluence reads
// are credential-viewable but per-source and per-page restrictions are not
// collected, so the read is incomplete and stays partial (fail closed; never
// upgraded to allowed).
func TestSourceEmitsPartialSourceACLStateOnContentFacts(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.May, 9, 12, 0, 0, 0, time.UTC)
	client := &fakeClient{
		space: Space{
			ID:    "100",
			Key:   "PLAT",
			Name:  "Platform",
			Links: Links{Base: "https://example.atlassian.net/wiki"},
		},
		spacePages: []Page{
			confluencePage("123", "Payment Service Deployment", 17, `<p>Body.</p>`),
		},
	}
	source := Source{
		Client: client,
		Config: SourceConfig{
			BaseURL:  "https://example.atlassian.net/wiki",
			SpaceID:  "100",
			SpaceKey: "PLAT",
			Now:      func() time.Time { return observedAt },
		},
	}

	collected, ok, err := source.Next(context.Background())
	if err != nil {
		t.Fatalf("Next() error = %v, want nil", err)
	}
	if !ok {
		t.Fatal("Next() ok = false, want true")
	}
	envelopes := drainFacts(t, collected.Facts)

	sourceFact := factsByKind(envelopes, facts.DocumentationSourceFactKind)[0]
	sourceACL := payloadMap(sourceFact.Payload, "acl_summary")
	if got, want := payloadString(sourceACL, "source_acl_state"), facts.SourceACLStatePartial; got != want {
		t.Fatalf("source fact source_acl_state = %q, want %q", got, want)
	}

	documentFact := factsByKind(envelopes, facts.DocumentationDocumentFactKind)[0]
	documentACL := payloadMap(documentFact.Payload, "acl_summary")
	if got, want := payloadString(documentACL, "source_acl_state"), facts.SourceACLStatePartial; got != want {
		t.Fatalf("document fact source_acl_state = %q, want %q", got, want)
	}
}
