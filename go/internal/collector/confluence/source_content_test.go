package confluence

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestSourcePersistsStorageContentOnSectionFacts(t *testing.T) {
	t.Parallel()

	body := `<h1>Deployment</h1><p>payment-api deploys from Argo CD.</p>`
	client := &fakeClient{
		space:      Space{ID: "100", Key: "PLAT", Name: "Platform"},
		spacePages: []Page{confluencePage("123", "Payment Service Deployment", 17, body)},
	}
	source := Source{
		Client: client,
		Config: SourceConfig{
			BaseURL: "https://example.atlassian.net/wiki",
			SpaceID: "100",
			Now:     fixedNow,
		},
	}

	collected, ok, err := source.Next(context.Background())
	if err != nil {
		t.Fatalf("Next() error = %v, want nil", err)
	}
	if !ok {
		t.Fatal("Next() ok = false, want true")
	}

	section := factsByKind(drainFacts(t, collected.Facts), facts.DocumentationSectionFactKind)[0]
	if got, want := section.SchemaVersion, facts.DocumentationSectionFactSchemaVersion; got != want {
		t.Fatalf("section SchemaVersion = %q, want %q", got, want)
	}
	if got, want := payloadString(section.Payload, "content"), body; got != want {
		t.Fatalf("section content = %q, want %q", got, want)
	}
	if got, want := payloadString(section.Payload, "content_format"), "storage"; got != want {
		t.Fatalf("section content_format = %q, want %q", got, want)
	}
	if got := payloadString(section.Payload, "text_hash"); got == "" {
		t.Fatal("section text_hash is empty")
	}
}
