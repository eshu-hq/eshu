package confluence

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/doctruth"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

func TestSourceSyncsSpacePagesIntoDocumentationFacts(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.May, 9, 12, 0, 0, 0, time.UTC)
	client := &fakeClient{
		space: Space{
			ID:   "100",
			Key:  "PLAT",
			Name: "Platform",
			Links: Links{
				Base: "https://example.atlassian.net/wiki",
			},
		},
		spacePages: []Page{
			confluencePage("123", "Payment Service Deployment", 17, `<h1>Deployment</h1><p>See <a href="https://github.com/example/platform-deployments/payment.yaml">deployment chart</a>.</p>`),
			confluencePage("124", "Payment Service Deployment", 3, `<p>Duplicate title, different page.</p>`),
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
	if got, want := collected.Scope.ScopeKind, scope.KindDocumentationSource; got != want {
		t.Fatalf("ScopeKind = %q, want %q", got, want)
	}
	if got, want := collected.Scope.CollectorKind, scope.CollectorDocumentation; got != want {
		t.Fatalf("CollectorKind = %q, want %q", got, want)
	}
	if got, want := collected.Scope.SourceSystem, "confluence"; got != want {
		t.Fatalf("SourceSystem = %q, want %q", got, want)
	}

	envelopes := drainFacts(t, collected.Facts)
	assertFactCount(t, envelopes, facts.DocumentationSourceFactKind, 1)
	assertFactCount(t, envelopes, facts.DocumentationDocumentFactKind, 2)
	assertFactCount(t, envelopes, facts.DocumentationSectionFactKind, 2)
	assertFactCount(t, envelopes, facts.DocumentationLinkFactKind, 1)
	if collected.FactCount != len(envelopes) {
		t.Fatalf("FactCount = %d, want %d", collected.FactCount, len(envelopes))
	}

	sourceFact := factsByKind(envelopes, facts.DocumentationSourceFactKind)[0]
	aclSummary := payloadMap(sourceFact.Payload, "acl_summary")
	if got, want := payloadString(aclSummary, "visibility"), "credential_viewable"; got != want {
		t.Fatalf("source acl visibility = %q, want %q", got, want)
	}
	if !payloadBool(aclSummary, "is_partial") {
		t.Fatal("source acl is_partial = false, want true")
	}
	if got, want := payloadString(aclSummary, "partial_reason"), "confluence_source_restrictions_not_collected"; got != want {
		t.Fatalf("source acl partial_reason = %q, want %q", got, want)
	}

	documents := factsByKind(envelopes, facts.DocumentationDocumentFactKind)
	if documents[0].StableFactKey == documents[1].StableFactKey {
		t.Fatalf("duplicate-title pages produced same stable key %q", documents[0].StableFactKey)
	}
	for _, envelope := range envelopes {
		if envelope.SchemaVersion != facts.DocumentationFactSchemaVersion {
			t.Fatalf("fact %q SchemaVersion = %q, want %q", envelope.FactKind, envelope.SchemaVersion, facts.DocumentationFactSchemaVersion)
		}
		if envelope.CollectorKind != string(scope.CollectorDocumentation) {
			t.Fatalf("fact %q CollectorKind = %q, want documentation", envelope.FactKind, envelope.CollectorKind)
		}
		if envelope.SourceConfidence != facts.SourceConfidenceObserved {
			t.Fatalf("fact %q SourceConfidence = %q, want observed", envelope.FactKind, envelope.SourceConfidence)
		}
	}
}

func TestSourceEmitsDocumentationTruthMentionsAndClaimsWhenExtractorConfigured(t *testing.T) {
	t.Parallel()

	client := &fakeClient{
		space: Space{ID: "100", Key: "PLAT", Name: "Platform"},
		spacePages: []Page{
			confluencePage("123", "Payment Service Deployment", 17, `<p>payment-api deploys from <a href="https://github.com/example/platform-deployments">deployments</a>.</p>`),
		},
	}
	source := Source{
		Client: client,
		Config: SourceConfig{
			BaseURL: "https://example.atlassian.net/wiki",
			SpaceID: "100",
			Now:     fixedNow,
		},
		TruthExtractor: doctruth.NewExtractor([]doctruth.Entity{
			{Kind: "service", ID: "service:payment-api", Aliases: []string{"payment-api"}},
			{Kind: "repository", ID: "repo:platform-deployments", URIs: []string{"https://github.com/example/platform-deployments"}},
		}, doctruth.Options{}),
		TruthClaimHints: func(_ Page, _ facts.DocumentationSectionPayload) []doctruth.ClaimHint {
			return []doctruth.ClaimHint{{
				ClaimID:     "claim:payment-api:deployment",
				ClaimType:   "service_deployment",
				ClaimText:   "payment-api deploys from deployments.",
				SubjectText: "payment-api",
				SubjectKind: "service",
			}}
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
	assertFactCount(t, envelopes, facts.DocumentationEntityMentionFactKind, 2)
	assertFactCount(t, envelopes, facts.DocumentationClaimCandidateFactKind, 1)
	mentions := factsByKind(envelopes, facts.DocumentationEntityMentionFactKind)
	for _, mention := range mentions {
		if got, want := payloadString(mention.Payload, "resolution_status"), facts.DocumentationMentionResolutionExact; got != want {
			t.Fatalf("resolution_status = %q, want %q", got, want)
		}
		if mention.SourceConfidence != facts.SourceConfidenceDerived {
			t.Fatalf("SourceConfidence = %q, want derived", mention.SourceConfidence)
		}
	}
	claim := factsByKind(envelopes, facts.DocumentationClaimCandidateFactKind)[0]
	section := factsByKind(envelopes, facts.DocumentationSectionFactKind)[0]
	if got, want := payloadString(claim.Payload, "excerpt_hash"), payloadString(section.Payload, "excerpt_hash"); got != want {
		t.Fatalf("excerpt_hash = %q, want %q", got, want)
	}
}

func TestSourceSkipsDeletedPagesAndKeepsLatestRevision(t *testing.T) {
	t.Parallel()

	client := &fakeClient{
		space: Space{ID: "100", Key: "PLAT", Name: "Platform"},
		spacePages: []Page{
			confluencePage("123", "Payment Service Deployment", 16, `<p>old</p>`),
			confluencePage("123", "Payment Service Deployment", 17, `<p>new</p>`),
			withStatus(confluencePage("999", "Deleted Page", 5, `<p>deleted</p>`), "trashed"),
		},
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

	documents := factsByKind(drainFacts(t, collected.Facts), facts.DocumentationDocumentFactKind)
	if got, want := len(documents), 1; got != want {
		t.Fatalf("documentation_document count = %d, want %d", got, want)
	}
	if got, want := payloadString(documents[0].Payload, "revision_id"), "17"; got != want {
		t.Fatalf("revision_id = %q, want %q", got, want)
	}
}

func TestSourceEnrichesSpacePagesBeforeEmittingFacts(t *testing.T) {
	t.Parallel()

	listedPage := confluencePage("123", "Payment Service Deployment", 17, `<p>listed body</p>`)
	listedPage.Labels = nil
	enrichedPage := confluencePage("123", "Payment Service Deployment", 18, `<p>enriched body</p>`)
	enrichedPage.Labels = []Label{{Name: "payments"}, {Name: "runbook"}}
	client := &fakeClient{
		space:      Space{ID: "100", Key: "PLAT", Name: "Platform"},
		spacePages: []Page{listedPage},
		pagesByID:  map[string]Page{"123": enrichedPage},
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

	documents := factsByKind(drainFacts(t, collected.Facts), facts.DocumentationDocumentFactKind)
	if got, want := len(documents), 1; got != want {
		t.Fatalf("documentation_document count = %d, want %d", got, want)
	}
	if got, want := payloadString(documents[0].Payload, "revision_id"), "18"; got != want {
		t.Fatalf("revision_id = %q, want enriched revision %q", got, want)
	}
	if got, want := payloadStrings(documents[0].Payload, "labels"), []string{"payments", "runbook"}; !equalStrings(got, want) {
		t.Fatalf("labels = %#v, want %#v", got, want)
	}
}

func TestSourceContinuesPastPermissionGapsInPageTree(t *testing.T) {
	t.Parallel()

	client := &fakeClient{
		treePageIDs: []string{"root", "child-visible", "child-hidden"},
		pagesByID: map[string]Page{
			"root":          confluencePage("root", "Root", 1, `<p>root</p>`),
			"child-visible": confluencePage("child-visible", "Visible Child", 2, `<p>visible</p>`),
		},
		forbiddenPageIDs: map[string]struct{}{"child-hidden": {}},
	}
	source := Source{
		Client: client,
		Config: SourceConfig{
			BaseURL:    "https://example.atlassian.net/wiki",
			RootPageID: "root",
			Now:        fixedNow,
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
	assertFactCount(t, envelopes, facts.DocumentationDocumentFactKind, 2)
	sourceFact := factsByKind(envelopes, facts.DocumentationSourceFactKind)[0]
	metadata := payloadMap(sourceFact.Payload, "source_metadata")
	if got, want := payloadInt(metadata, "failure_count"), 1; got != want {
		t.Fatalf("failure_count = %d, want %d", got, want)
	}
	if got, want := payloadString(metadata, "sync_status"), "partial"; got != want {
		t.Fatalf("sync_status = %q, want %q", got, want)
	}
}

func TestSourceSyncLogDoesNotIncludePageTitlesOrExcerpts(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	client := &fakeClient{
		space: Space{ID: "100", Key: "PLAT", Name: "Platform"},
		spacePages: []Page{
			confluencePage("123", "Secret Deployment Runbook", 17, `<p>private-token-value deploys from a sensitive path.</p>`),
		},
	}
	source := Source{
		Client: client,
		Config: SourceConfig{
			BaseURL: "https://example.atlassian.net/wiki",
			SpaceID: "100",
			Now:     fixedNow,
		},
		Logger: slog.New(slog.NewJSONHandler(&logs, nil)),
	}

	_, ok, err := source.Next(context.Background())
	if err != nil {
		t.Fatalf("Next() error = %v, want nil", err)
	}
	if !ok {
		t.Fatal("Next() ok = false, want true")
	}
	logText := logs.String()
	for _, leaked := range []string{"Secret Deployment Runbook", "private-token-value", "sensitive path"} {
		if strings.Contains(logText, leaked) {
			t.Fatalf("log leaked %q: %s", leaked, logText)
		}
	}
}

func TestSourceUsesPerPageCanonicalURIFallbackWhenWebUILinkIsMissing(t *testing.T) {
	t.Parallel()

	page := confluencePage("root", "Root", 1, `<p>root</p>`)
	page.Links.WebUI = ""
	client := &fakeClient{
		treePageIDs: []string{"root"},
		pagesByID:   map[string]Page{"root": page},
	}
	source := Source{
		Client: client,
		Config: SourceConfig{
			BaseURL:    "https://example.atlassian.net/wiki",
			RootPageID: "root",
			Now:        fixedNow,
		},
	}

	collected, ok, err := source.Next(context.Background())
	if err != nil {
		t.Fatalf("Next() error = %v, want nil", err)
	}
	if !ok {
		t.Fatal("Next() ok = false, want true")
	}

	document := factsByKind(drainFacts(t, collected.Facts), facts.DocumentationDocumentFactKind)[0]
	if got, want := payloadString(document.Payload, "canonical_uri"), "https://example.atlassian.net/wiki/api/v2/pages/root"; got != want {
		t.Fatalf("canonical_uri = %q, want %q", got, want)
	}
	if got, want := document.SourceRef.SourceURI, "https://example.atlassian.net/wiki/api/v2/pages/root"; got != want {
		t.Fatalf("SourceURI = %q, want %q", got, want)
	}
}

func TestSourceReturnsEmptySpaceGeneration(t *testing.T) {
	t.Parallel()

	source := Source{
		Client: &fakeClient{
			space: Space{ID: "100", Key: "EMPTY", Name: "Empty"},
		},
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

	envelopes := drainFacts(t, collected.Facts)
	assertFactCount(t, envelopes, facts.DocumentationSourceFactKind, 1)
	assertFactCount(t, envelopes, facts.DocumentationDocumentFactKind, 0)
	sourceFact := factsByKind(envelopes, facts.DocumentationSourceFactKind)[0]
	metadata := payloadMap(sourceFact.Payload, "source_metadata")
	if got, want := payloadInt(metadata, "page_count"), 0; got != want {
		t.Fatalf("page_count = %d, want %d", got, want)
	}
}

func TestPayloadToMapReturnsErrorForUnmarshalablePayload(t *testing.T) {
	t.Parallel()

	_, err := payloadToMap(func() {})
	if err == nil {
		t.Fatal("payloadToMap() error = nil, want marshal error")
	}
}

func fixedNow() time.Time {
	return time.Date(2026, time.May, 9, 12, 0, 0, 0, time.UTC)
}

func confluencePage(id string, title string, version int, body string) Page {
	return Page{
		ID:       id,
		Status:   "current",
		Title:    title,
		SpaceID:  "100",
		ParentID: "root",
		OwnerID:  "user:owner",
		Version: PageVersion{
			Number:    version,
			CreatedAt: "2026-05-09T11:00:00Z",
		},
		Body: PageBody{
			Storage: BodyRepresentation{Value: body, Representation: "storage"},
		},
		Labels: []Label{{Name: "payments"}, {Name: "deployment"}},
		Links: Links{
			Base:  "https://example.atlassian.net/wiki",
			WebUI: "/spaces/PLAT/pages/" + id,
		},
	}
}

func withStatus(page Page, status string) Page {
	page.Status = status
	return page
}
