package confluence

import "testing"

func TestDocumentPayloadMarksACLAsPartialWhenRestrictionsAreNotCollected(t *testing.T) {
	t.Parallel()

	payload := documentPayload(
		"doc-source:confluence:example:100",
		"https://example.atlassian.net/wiki",
		confluencePage("123", "Payment Service Deployment", 17, "<p>body</p>"),
	)

	if payload.ACLSummary == nil {
		t.Fatal("ACLSummary = nil, want partial ACL summary")
	}
	if got, want := payload.ACLSummary.Visibility, "credential_viewable"; got != want {
		t.Fatalf("Visibility = %q, want %q", got, want)
	}
	if !payload.ACLSummary.IsPartial {
		t.Fatal("IsPartial = false, want true because page restrictions were not collected")
	}
	if got, want := payload.ACLSummary.PartialReason, "confluence_page_restrictions_not_collected"; got != want {
		t.Fatalf("PartialReason = %q, want %q", got, want)
	}
}

func TestExtractLinksReadsConfluenceStorageLinks(t *testing.T) {
	t.Parallel()

	body := `
<p>
  <ac:link>
    <ri:page ri:content-id="123" ri:content-title="Payment Runbook"/>
    <ac:plain-text-link-body><![CDATA[Payment Runbook]]></ac:plain-text-link-body>
  </ac:link>
  <ac:link>
    <ri:attachment ri:filename="diagram.png"/>
    <ac:plain-text-link-body><![CDATA[diagram]]></ac:plain-text-link-body>
  </ac:link>
  <ac:link>
    <ri:url ri:value="https://example.com/external"/>
    <ac:plain-text-link-body><![CDATA[external]]></ac:plain-text-link-body>
  </ac:link>
  <a href="https://github.com/example/repo">repo</a>
</p>`

	links := extractLinks(body)
	if got, want := len(links), 4; got != want {
		t.Fatalf("len(links) = %d, want %d: %#v", got, want, links)
	}
	wantHrefs := []string{
		"confluence:page:123",
		"confluence:attachment:diagram.png",
		"https://example.com/external",
		"https://github.com/example/repo",
	}
	for index, want := range wantHrefs {
		if links[index].href != want {
			t.Fatalf("links[%d].href = %q, want %q", index, links[index].href, want)
		}
	}
}
