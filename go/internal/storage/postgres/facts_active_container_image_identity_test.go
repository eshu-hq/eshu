// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestFactStoreListActiveContainerImageIdentityFactsUsesActiveIdentityGenerations(t *testing.T) {
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
				}, {
					"fact-aws-image-1",
					"aws:123456789012:us-east-1:ecr",
					"generation-aws",
					"aws_image_reference",
					"aws-image:team-api",
					"1.0.0",
					"aws_cloud",
					int64(0),
					"reported",
					"aws",
					"aws-image:team-api",
					"arn:aws:ecr:us-east-1:123456789012:repository/team/api",
					"team/api@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
					time.Date(2026, time.May, 15, 10, 0, 1, 0, time.UTC),
					false,
					[]byte(`{"account_id":"123456789012","region":"us-east-1","repository_name":"team/api","image_digest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`),
				}, {
					"fact-git-entity-1",
					"repo:team-api",
					"generation-git",
					"content_entity",
					"content_entity:repo:team-api:deploy",
					"1.0.0",
					"git",
					int64(0),
					"reported",
					"git",
					"content_entity:repo:team-api:deploy",
					"https://example.test/team/api/deploy.yaml",
					"deploy.yaml",
					time.Date(2026, time.May, 15, 10, 0, 2, 0, time.UTC),
					false,
					[]byte(`{"entity_type":"KubernetesResource","entity_metadata":{"container_images":["registry.example.com/team/api:prod"]}}`),
				}, {
					"fact-gcp-image-1",
					"gcp:project:demo:run:resource:global",
					"generation-gcp",
					"gcp_image_reference",
					"gcp-image:team-api",
					"1.0.0",
					"gcp",
					int64(0),
					"reported",
					"gcp",
					"gcp-image:team-api",
					"gcp://cloud-run/demo-service",
					"team/api@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
					time.Date(2026, time.May, 15, 10, 0, 3, 0, time.UTC),
					false,
					[]byte(`{"owning_full_resource_name":"//run.googleapis.com/projects/demo/locations/us-central1/services/api","image_reference":"registry.example.com/team/api:prod","image_digest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`),
				}, {
					"fact-azure-image-1",
					"azure:tenant:subscription:demo:containerapps:global:resources",
					"generation-azure",
					"azure_image_reference",
					"azure-image:team-api",
					"1.0.0",
					"azure",
					int64(0),
					"reported",
					"azure",
					"azure-image:team-api",
					"azure://container-apps/demo-api",
					"team/api@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
					time.Date(2026, time.May, 15, 10, 0, 4, 0, time.UTC),
					false,
					[]byte(`{"owning_arm_resource_id":"/subscriptions/demo/resourceGroups/rg/providers/Microsoft.App/containerApps/api","image_reference":"contoso.azurecr.io/team/api:prod","image_digest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`),
				}},
			},
		},
	}
	store := NewFactStore(db)

	loaded, err := store.ListActiveContainerImageIdentityFacts(context.Background())
	if err != nil {
		t.Fatalf("ListActiveContainerImageIdentityFacts() error = %v, want nil", err)
	}
	if got, want := len(loaded), 5; got != want {
		t.Fatalf("ListActiveContainerImageIdentityFacts() len = %d, want %d", got, want)
	}
	if got, want := loaded[0].FactKind, "oci_registry.image_tag_observation"; got != want {
		t.Fatalf("FactKind = %q, want %q", got, want)
	}
	if got, want := loaded[1].FactKind, "aws_image_reference"; got != want {
		t.Fatalf("FactKind = %q, want %q", got, want)
	}
	if got, want := loaded[2].FactKind, "content_entity"; got != want {
		t.Fatalf("FactKind = %q, want %q", got, want)
	}
	if got, want := loaded[3].FactKind, "gcp_image_reference"; got != want {
		t.Fatalf("FactKind = %q, want %q", got, want)
	}
	if got, want := loaded[4].FactKind, "azure_image_reference"; got != want {
		t.Fatalf("FactKind = %q, want %q", got, want)
	}
	query := db.queries[0].query
	for _, want := range []string{
		"scope.active_generation_id = fact.generation_id",
		"generation.status = 'active'",
		"fact.fact_kind IN ('oci_registry.image_tag_observation', 'oci_registry.image_manifest', 'oci_registry.image_index')",
		"fact.fact_kind = 'aws_image_reference'",
		"fact.fact_kind = 'aws_relationship'",
		"fact.fact_kind = 'gcp_image_reference'",
		"fact.fact_kind = 'azure_image_reference'",
		"fact.fact_kind = 'content_entity'",
		"fact.payload->'entity_metadata' ? 'container_images'",
		"fact.is_tombstone = FALSE",
		"ORDER BY fact.observed_at ASC, fact.fact_id ASC",
		"LIMIT $3",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("query missing %q:\n%s", want, query)
		}
	}
}
