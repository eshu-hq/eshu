package postgres

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestFactStoreListActiveContainerImageIdentityFactsUsesActiveOCIGenerations(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{
				rows: [][]any{{
					"fact-oci-tag-1",
					"oci-registry://registry.example.com/team/api",
					"generation-oci",
					"oci_registry.image_tag_observation",
					"oci-tag:team-api:prod",
					"1.0.0",
					"oci_registry",
					int64(0),
					"reported",
					"oci_registry",
					"oci-tag:team-api:prod",
					"oci://registry.example.com/team/api:prod",
					"team/api:prod",
					time.Date(2026, time.May, 15, 10, 0, 0, 0, time.UTC),
					false,
					[]byte(`{"registry":"registry.example.com","repository":"team/api","tag":"prod","resolved_digest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`),
				}},
			},
		},
	}
	store := NewFactStore(db)

	loaded, err := store.ListActiveContainerImageIdentityFacts(context.Background())
	if err != nil {
		t.Fatalf("ListActiveContainerImageIdentityFacts() error = %v, want nil", err)
	}
	if got, want := len(loaded), 1; got != want {
		t.Fatalf("ListActiveContainerImageIdentityFacts() len = %d, want %d", got, want)
	}
	if got, want := loaded[0].FactKind, "oci_registry.image_tag_observation"; got != want {
		t.Fatalf("FactKind = %q, want %q", got, want)
	}
	query := db.queries[0].query
	for _, want := range []string{
		"scope.active_generation_id = fact.generation_id",
		"generation.status = 'active'",
		"fact.fact_kind = ANY($1::text[])",
		"fact.source_system = 'oci_registry'",
		"ORDER BY fact.observed_at ASC, fact.fact_id ASC",
		"LIMIT $4",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("query missing %q:\n%s", want, query)
		}
	}
}
