// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package servicecatalog

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func cortexContext() FixtureContext {
	return FixtureContext{
		ScopeID:             "git://github.com/eshu-hq/checkout-api",
		GenerationID:        "gen-1",
		CollectorInstanceID: "fixture-cortex",
		FencingToken:        9,
		ObservedAt:          time.Date(2026, 5, 31, 3, 30, 0, 0, time.UTC),
		SourceURI:           "cortex.yaml",
	}
}

func TestCortexManifestEnvelopesEmitsTypedContract(t *testing.T) {
	t.Parallel()

	raw := readFixture(t, "testdata/cortex_catalog.yaml")
	envelopes, err := CortexManifestEnvelopes(raw, cortexContext())
	if err != nil {
		t.Fatalf("CortexManifestEnvelopes() error = %v", err)
	}

	byKind := envelopesByKind(envelopes)
	// Four parseable service documents -> four entity facts.
	assertKindCount(t, byKind, facts.ServiceCatalogEntityFactKind, 4)
	// Every entity declares an owner -> four ownership facts.
	assertKindCount(t, byKind, facts.ServiceCatalogOwnershipFactKind, 4)
	// checkout-api (exact), ledger (derived), search (name-only) declare a
	// repository link; notifications declares none.
	assertKindCount(t, byKind, facts.ServiceCatalogRepositoryLinkFactKind, 3)
	// checkout-api dependsOn ledger + payments-db.
	assertKindCount(t, byKind, facts.ServiceCatalogDependencyFactKind, 2)
	// checkout-api runbook link is a clean operational link; the PagerDuty link
	// carries a token query string and is redacted to a warning instead.
	assertKindCount(t, byKind, facts.ServiceCatalogOperationalLinkFactKind, 1)
	assertWarningReason(t, byKind, "operational_link_redacted")

	entity := findEntity(t, envelopes, "service:cortex/checkout-api")
	assertPayload(t, entity.Payload, "provider", string(ProviderCortex))
	assertPayload(t, entity.Payload, "entity_ref", "service:cortex/checkout-api")
	assertPayload(t, entity.Payload, "entity_type", "service")
	assertPayload(t, entity.Payload, "display_name", "Checkout API")
	assertPayload(t, entity.Payload, "tier", "tier-1")

	// Hard producer constraint: never mint canonical identity from catalog text.
	assertBlank(t, entity.Payload, "service_id")
	assertBlank(t, entity.Payload, "workload_id")

	link := findRepositoryLink(t, envelopes, "service:cortex/checkout-api")
	assertPayload(t, link.Payload, "repository_url", "https://github.com/eshu-hq/checkout-api")
	assertBlank(t, link.Payload, "repository_id")
	assertBlank(t, link.Payload, "service_id")
	assertBlank(t, link.Payload, "workload_id")

	owner := findOwnership(t, envelopes, "service:cortex/checkout-api")
	assertPayload(t, owner.Payload, "owner_ref", "team-payments")

	// Search declares an unknown/self-hosted git provider, which cannot be
	// expanded into a URL. It must surface as a name-only locator, never a
	// fabricated URL, so the reducer can reject it.
	searchLink := findRepositoryLink(t, envelopes, "service:cortex/search")
	assertPayload(t, searchLink.Payload, "repository_name", "search")
	assertBlank(t, searchLink.Payload, "repository_url")

	// Every emitted service-catalog envelope must carry the shared schema
	// version, or the projector silently rejects the reducer intent.
	for _, envelope := range envelopes {
		if _, ok := facts.ServiceCatalogSchemaVersion(envelope.FactKind); !ok {
			t.Fatalf("emitted unexpected fact kind %q", envelope.FactKind)
		}
		if envelope.SchemaVersion != facts.ServiceCatalogSchemaVersionV1 {
			t.Fatalf("fact %q schema_version = %q, want %q", envelope.FactKind, envelope.SchemaVersion, facts.ServiceCatalogSchemaVersionV1)
		}
		if envelope.CollectorKind != CollectorKind {
			t.Fatalf("fact %q collector_kind = %q, want %q", envelope.FactKind, envelope.CollectorKind, CollectorKind)
		}
		if envelope.SourceConfidence != facts.SourceConfidenceObserved {
			t.Fatalf("fact %q source_confidence = %q, want observed", envelope.FactKind, envelope.SourceConfidence)
		}
		if envelope.ScopeID != cortexContext().ScopeID || envelope.GenerationID != cortexContext().GenerationID {
			t.Fatalf("fact %q scope/generation = %q/%q, want fixture boundary", envelope.FactKind, envelope.ScopeID, envelope.GenerationID)
		}
	}
}

func TestCortexGitProviderHostDerivation(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		provider string
		project  string
		repo     string
		wantURL  string
		wantName string
	}{
		{"github", "github", "", "eshu-hq/checkout-api", "https://github.com/eshu-hq/checkout-api", ""},
		{"gitlab", "gitlab", "", "group/sub/project", "https://gitlab.com/group/sub/project", ""},
		{"bitbucket", "bitbucket", "", "team/repo", "https://bitbucket.org/team/repo", ""},
		{"azure", "azure", "myproject", "myrepo", "https://dev.azure.com/myproject/_git/myrepo", ""},
		{"unknown_provider", "self-hosted", "", "team/repo", "", "team/repo"},
		{"slug_without_path", "github", "", "barename", "", "barename"},
		{"azure_missing_project", "azure", "", "myrepo", "", "myrepo"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			git := cortexGit{Providers: map[string]cortexGitProvider{
				tc.provider: {Project: tc.project, Repository: tc.repo},
			}}
			gotURL, gotName := git.repositoryLocator()
			if gotURL != tc.wantURL {
				t.Fatalf("repositoryLocator() url = %q, want %q", gotURL, tc.wantURL)
			}
			if gotName != tc.wantName {
				t.Fatalf("repositoryLocator() name = %q, want %q", gotName, tc.wantName)
			}
		})
	}
}

func TestCortexGitMultiProviderIsDeterministic(t *testing.T) {
	t.Parallel()

	// A descriptor declaring more than one provider must resolve deterministically
	// (sorted provider order), or stable fact ids would drift across emissions.
	git := cortexGit{Providers: map[string]cortexGitProvider{
		"gitlab": {Repository: "group/project"},
		"github": {Repository: "eshu-hq/checkout-api"},
	}}
	wantURL := "https://github.com/eshu-hq/checkout-api"
	for i := 0; i < 50; i++ {
		gotURL, gotName := git.repositoryLocator()
		if gotURL != wantURL || gotName != "" {
			t.Fatalf("multi-provider repositoryLocator() = (%q, %q), want (%q, \"\")", gotURL, gotName, wantURL)
		}
	}
}

func TestCortexGitMultiProviderPrefersResolvableURL(t *testing.T) {
	t.Parallel()

	// A descriptor can declare more than one git provider. A provider that cannot
	// be expanded into a URL (an Azure entry missing its project, or an
	// unknown/self-hosted provider, or a bare slug) MUST NOT block a later provider
	// in the same x-cortex-git block from yielding a resolvable repository_url.
	// Otherwise a resolvable repository is silently downgraded to a name-only
	// locator and the reducer rejects it.
	cases := []struct {
		name      string
		providers map[string]cortexGitProvider
		wantURL   string
		wantName  string
	}{
		{
			// "azure" sorts before "github"; the Azure entry has no project so it
			// cannot expand, but github must still win.
			name: "azure_missing_project_then_github",
			providers: map[string]cortexGitProvider{
				"azure":  {Repository: "myrepo"},
				"github": {Repository: "eshu-hq/checkout-api"},
			},
			wantURL:  "https://github.com/eshu-hq/checkout-api",
			wantName: "",
		},
		{
			// "aaa-self-hosted" sorts before "gitlab"; an unknown provider must not
			// shadow the resolvable gitlab entry.
			name: "unknown_provider_then_gitlab",
			providers: map[string]cortexGitProvider{
				"aaa-self-hosted": {Repository: "team/repo"},
				"gitlab":          {Repository: "group/project"},
			},
			wantURL:  "https://gitlab.com/group/project",
			wantName: "",
		},
		{
			// A bare slug (no namespace path) on a known provider cannot form a URL;
			// a later provider with a path-shaped slug must still resolve.
			name: "bare_slug_then_pathed_provider",
			providers: map[string]cortexGitProvider{
				"github": {Repository: "barename"},
				"gitlab": {Repository: "group/project"},
			},
			wantURL:  "https://gitlab.com/group/project",
			wantName: "",
		},
		{
			// When NO provider can expand, fall back to a name-only locator chosen
			// deterministically (first in sorted provider order) so the reducer can
			// reject it without fabricating a URL.
			name: "no_resolvable_provider_falls_back_to_name",
			providers: map[string]cortexGitProvider{
				"azure":           {Repository: "myrepo"},
				"zzz-self-hosted": {Repository: "team/repo"},
			},
			wantURL:  "",
			wantName: "myrepo",
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			git := cortexGit{Providers: tc.providers}
			// Repeat to also assert determinism across map-iteration orderings.
			for i := 0; i < 50; i++ {
				gotURL, gotName := git.repositoryLocator()
				if gotURL != tc.wantURL || gotName != tc.wantName {
					t.Fatalf("repositoryLocator() = (%q, %q), want (%q, %q)", gotURL, gotName, tc.wantURL, tc.wantName)
				}
			}
		})
	}
}

func TestCortexDependencyRefIsEntityRefShaped(t *testing.T) {
	t.Parallel()

	// Cortex x-cortex-dependency carries only a tag. The producer must anchor the
	// dependency target in the same `type:cortex/<tag>` ref shape the entity
	// producer mints for an untyped entity, so a future reducer join can correlate
	// a dependency to the emitted entity by provider plus ref. Raw tags would not
	// join.
	raw := readFixture(t, "testdata/cortex_catalog.yaml")
	envelopes, err := CortexManifestEnvelopes(raw, cortexContext())
	if err != nil {
		t.Fatalf("CortexManifestEnvelopes() error = %v", err)
	}

	wantRefs := map[string]bool{
		"service:cortex/ledger":      false,
		"service:cortex/payments-db": false,
	}
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.ServiceCatalogDependencyFactKind {
			continue
		}
		assertPayload(t, envelope.Payload, "entity_ref", "service:cortex/checkout-api")
		ref, _ := envelope.Payload["depends_on_ref"].(string)
		if _, ok := wantRefs[ref]; !ok {
			t.Fatalf("dependency depends_on_ref = %q, want an entity-ref-shaped cortex target", ref)
		}
		wantRefs[ref] = true
	}
	for ref, seen := range wantRefs {
		if !seen {
			t.Fatalf("expected dependency fact with depends_on_ref %q was not emitted", ref)
		}
	}
}

// TestCortexManifestReducerRoundTrip is the payload-key-fidelity contract test:
// emitted envelopes flow through the real reducer index and must reach the
// intended outcomes. It imports the reducer in test code only.
func TestCortexManifestReducerRoundTrip(t *testing.T) {
	t.Parallel()

	raw := readFixture(t, "testdata/cortex_catalog.yaml")
	catalog, err := CortexManifestEnvelopes(raw, cortexContext())
	if err != nil {
		t.Fatalf("CortexManifestEnvelopes() error = %v", err)
	}

	// Synthetic active repository facts the reducer correlates against. Only
	// checkout-api and ledger have a real active match.
	repos := []facts.Envelope{
		activeRepositoryFact("repo-checkout", "https://github.com/eshu-hq/checkout-api", false),
		activeRepositoryFact("repo-ledger", "https://github.com/eshu-hq/ledger.git", false),
	}

	decisions := reducer.BuildServiceCatalogCorrelationDecisions(append(catalog, repos...))
	byEntity := decisionsByEntity(decisions)

	assertOutcome(t, byEntity, "service:cortex/checkout-api", reducer.ServiceCatalogCorrelationExact)
	assertOutcome(t, byEntity, "service:cortex/ledger", reducer.ServiceCatalogCorrelationDerived)
	assertOutcome(t, byEntity, "service:cortex/notifications", reducer.ServiceCatalogCorrelationUnresolved)
	assertOutcome(t, byEntity, "service:cortex/search", reducer.ServiceCatalogCorrelationRejected)

	// Non-over-admission: provenance-only entities never carry canonical ids.
	for _, ref := range []string{"service:cortex/notifications", "service:cortex/search"} {
		decision := byEntity[ref]
		if decision.ServiceID != "" || decision.WorkloadID != "" || decision.RepositoryID != "" {
			t.Fatalf("entity %q over-admitted: service=%q workload=%q repo=%q", ref, decision.ServiceID, decision.WorkloadID, decision.RepositoryID)
		}
		if !decision.ProvenanceOnly {
			t.Fatalf("entity %q must remain provenance-only", ref)
		}
	}

	// Owners are recorded as provenance even when unresolved.
	if got := byEntity["service:cortex/notifications"].OwnerRef; got != "team-platform" {
		t.Fatalf("notifications owner_ref = %q, want team-platform", got)
	}

	// Dependency facts are carried but must not change the entity outcome.
	if byEntity["service:cortex/checkout-api"].Outcome != reducer.ServiceCatalogCorrelationExact {
		t.Fatalf("dependency facts must not alter checkout-api outcome")
	}
}

func TestCortexManifestStaleAndAmbiguous(t *testing.T) {
	t.Parallel()

	raw := readFixture(t, "testdata/cortex_catalog.yaml")
	catalog, err := CortexManifestEnvelopes(raw, cortexContext())
	if err != nil {
		t.Fatalf("CortexManifestEnvelopes() error = %v", err)
	}

	// ledger matches only a tombstoned repo -> stale. checkout-api matches two
	// active repos with the same remote -> ambiguous.
	repos := []facts.Envelope{
		activeRepositoryFact("repo-checkout-a", "https://github.com/eshu-hq/checkout-api", false),
		activeRepositoryFact("repo-checkout-b", "https://github.com/eshu-hq/checkout-api", false),
		activeRepositoryFact("repo-ledger", "https://github.com/eshu-hq/ledger.git", true),
	}

	decisions := reducer.BuildServiceCatalogCorrelationDecisions(append(catalog, repos...))
	byEntity := decisionsByEntity(decisions)

	assertOutcome(t, byEntity, "service:cortex/checkout-api", reducer.ServiceCatalogCorrelationAmbiguous)
	assertOutcome(t, byEntity, "service:cortex/ledger", reducer.ServiceCatalogCorrelationStale)
}

func TestCortexManifestPartialDocumentsWarnNotDrop(t *testing.T) {
	t.Parallel()

	raw := readFixture(t, "testdata/cortex_partial.yaml")
	envelopes, err := CortexManifestEnvelopes(raw, cortexContext())
	if err != nil {
		t.Fatalf("CortexManifestEnvelopes() error = %v", err)
	}
	byKind := envelopesByKind(envelopes)

	// Unsupported descriptor: entity still emitted plus a warning.
	assertWarningReason(t, byKind, "unsupported_descriptor_version")
	if findEntityOK(envelopes, "service:cortex/future-shape") == nil {
		t.Fatalf("unsupported-version entity must still be emitted, not dropped")
	}
	// Missing tag: no entity, but an invalid_ref warning.
	assertWarningReason(t, byKind, "invalid_ref")
	// Token-bearing dashboard link redacted with a warning.
	assertWarningReason(t, byKind, "operational_link_redacted")
}

func TestCortexManifestDuplicateEntityWarns(t *testing.T) {
	t.Parallel()

	raw := readFixture(t, "testdata/cortex_duplicate.yaml")
	envelopes, err := CortexManifestEnvelopes(raw, cortexContext())
	if err != nil {
		t.Fatalf("CortexManifestEnvelopes() error = %v", err)
	}
	byKind := envelopesByKind(envelopes)

	// First-wins: exactly one billing entity, plus a duplicate_entity warning.
	assertKindCount(t, byKind, facts.ServiceCatalogEntityFactKind, 1)
	assertWarningReason(t, byKind, "duplicate_entity")

	entity := findEntity(t, envelopes, "service:cortex/billing")
	assertPayload(t, entity.Payload, "display_name", "Billing (first)")
}

func TestCortexManifestEmptyInputIsClean(t *testing.T) {
	t.Parallel()

	envelopes, err := CortexManifestEnvelopes([]byte("\n# nothing here\n"), cortexContext())
	if err != nil {
		t.Fatalf("CortexManifestEnvelopes() empty error = %v", err)
	}
	if len(envelopes) != 0 {
		t.Fatalf("empty manifest envelopes = %d, want 0", len(envelopes))
	}
}

func TestCortexManifestIsIdempotent(t *testing.T) {
	t.Parallel()

	raw := readFixture(t, "testdata/cortex_catalog.yaml")
	ctx := cortexContext()
	first, err := CortexManifestEnvelopes(raw, ctx)
	if err != nil {
		t.Fatalf("first emit error = %v", err)
	}
	second, err := CortexManifestEnvelopes(raw, ctx)
	if err != nil {
		t.Fatalf("second emit error = %v", err)
	}
	if len(first) != len(second) {
		t.Fatalf("re-emit count drift: %d vs %d", len(first), len(second))
	}
	firstIDs := factIDSet(first)
	for _, envelope := range second {
		if !firstIDs[envelope.FactID] {
			t.Fatalf("re-emit produced unstable fact id %q for kind %q", envelope.FactID, envelope.FactKind)
		}
	}
}

func TestCortexManifestRequiresContext(t *testing.T) {
	t.Parallel()

	raw := readFixture(t, "testdata/cortex_catalog.yaml")
	if _, err := CortexManifestEnvelopes(raw, FixtureContext{GenerationID: "gen-1", CollectorInstanceID: "x"}); err == nil {
		t.Fatalf("blank scope_id must error")
	}
	if _, err := CortexManifestEnvelopes(raw, FixtureContext{ScopeID: "s", CollectorInstanceID: "x"}); err == nil {
		t.Fatalf("blank generation_id must error")
	}
	if _, err := CortexManifestEnvelopes(raw, FixtureContext{ScopeID: "s", GenerationID: "g"}); err == nil {
		t.Fatalf("blank collector_instance_id must error")
	}
}
