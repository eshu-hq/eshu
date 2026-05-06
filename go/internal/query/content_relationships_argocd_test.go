package query

import (
	"context"
	"database/sql/driver"
	"reflect"
	"testing"
)

func TestBuildContentRelationshipSetArgoCDApplicationPromotesSourceAndDestination(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, nil)
	reader := NewContentReader(db)
	application := EntityContent{
		EntityID:   "argocd-app-1",
		RepoID:     "repo-1",
		EntityType: "ArgoCDApplication",
		EntityName: "payments-app",
		Metadata: map[string]any{
			"source_repo":    "https://github.com/myorg/payments-service.git",
			"source_path":    "deploy/overlays/prod",
			"dest_server":    "https://kubernetes.default.svc",
			"dest_namespace": "payments",
		},
	}

	relationships, err := buildContentRelationshipSet(context.Background(), reader, application)
	if err != nil {
		t.Fatalf("buildContentRelationshipSet() error = %v, want nil", err)
	}
	if len(relationships.outgoing) != 2 {
		t.Fatalf("len(relationships.outgoing) = %d, want 2", len(relationships.outgoing))
	}

	if got, want := relationships.outgoing[0]["type"], "DEPLOYS_FROM"; got != want {
		t.Fatalf("relationships.outgoing[0][type] = %#v, want %#v", got, want)
	}
	if got, want := relationships.outgoing[0]["target_name"], "https://github.com/myorg/payments-service.git"; got != want {
		t.Fatalf("relationships.outgoing[0][target_name] = %#v, want %#v", got, want)
	}
	if got, want := relationships.outgoing[0]["reason"], "argocd_application_source"; got != want {
		t.Fatalf("relationships.outgoing[0][reason] = %#v, want %#v", got, want)
	}

	if got, want := relationships.outgoing[1]["type"], "RUNS_ON"; got != want {
		t.Fatalf("relationships.outgoing[1][type] = %#v, want %#v", got, want)
	}
	if got, want := relationships.outgoing[1]["target_name"], "https://kubernetes.default.svc"; got != want {
		t.Fatalf("relationships.outgoing[1][target_name] = %#v, want %#v", got, want)
	}
	if got, want := relationships.outgoing[1]["reason"], "argocd_destination_server"; got != want {
		t.Fatalf("relationships.outgoing[1][reason] = %#v, want %#v", got, want)
	}
}

func TestBuildContentRelationshipSetArgoCDApplicationSetPromotesDiscoveryDeployAndDestination(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, nil)
	reader := NewContentReader(db)
	applicationSet := EntityContent{
		EntityID:   "argocd-appset-1",
		RepoID:     "repo-1",
		EntityType: "ArgoCDApplicationSet",
		EntityName: "platform-appset",
		Metadata: map[string]any{
			"generator_source_repos": "https://github.com/myorg/platform-config.git",
			"generator_source_paths": "argocd/platform/*/config.yaml",
			"template_source_repos":  "https://github.com/myorg/platform-runtime.git",
			"template_source_paths":  "deploy/overlays/prod",
			"dest_server":            "https://kubernetes.default.svc",
			"dest_namespace":         "platform",
		},
	}

	relationships, err := buildContentRelationshipSet(context.Background(), reader, applicationSet)
	if err != nil {
		t.Fatalf("buildContentRelationshipSet() error = %v, want nil", err)
	}
	if len(relationships.outgoing) != 3 {
		t.Fatalf("len(relationships.outgoing) = %d, want 3", len(relationships.outgoing))
	}

	if got, want := relationships.outgoing[0]["type"], "DISCOVERS_CONFIG_IN"; got != want {
		t.Fatalf("relationships.outgoing[0][type] = %#v, want %#v", got, want)
	}
	if got, want := relationships.outgoing[0]["target_name"], "https://github.com/myorg/platform-config.git"; got != want {
		t.Fatalf("relationships.outgoing[0][target_name] = %#v, want %#v", got, want)
	}
	if got, want := relationships.outgoing[0]["reason"], "argocd_applicationset_generator"; got != want {
		t.Fatalf("relationships.outgoing[0][reason] = %#v, want %#v", got, want)
	}

	if got, want := relationships.outgoing[1]["type"], "DEPLOYS_FROM"; got != want {
		t.Fatalf("relationships.outgoing[1][type] = %#v, want %#v", got, want)
	}
	if got, want := relationships.outgoing[1]["target_name"], "https://github.com/myorg/platform-runtime.git"; got != want {
		t.Fatalf("relationships.outgoing[1][target_name] = %#v, want %#v", got, want)
	}
	if got, want := relationships.outgoing[1]["reason"], "argocd_applicationset_template"; got != want {
		t.Fatalf("relationships.outgoing[1][reason] = %#v, want %#v", got, want)
	}

	if got, want := relationships.outgoing[2]["type"], "RUNS_ON"; got != want {
		t.Fatalf("relationships.outgoing[2][type] = %#v, want %#v", got, want)
	}
	if got, want := relationships.outgoing[2]["target_name"], "https://kubernetes.default.svc"; got != want {
		t.Fatalf("relationships.outgoing[2][target_name] = %#v, want %#v", got, want)
	}
	if got, want := relationships.outgoing[2]["reason"], "argocd_destination_server"; got != want {
		t.Fatalf("relationships.outgoing[2][reason] = %#v, want %#v", got, want)
	}
}

func TestBuildContentRelationshipSetTerraformModulePromotesSource(t *testing.T) {
	t.Parallel()

	relationships, err := buildContentRelationshipSet(context.Background(), nil, EntityContent{
		EntityID:   "tf-module-1",
		RepoID:     "repo-1",
		EntityType: "TerraformModule",
		EntityName: "eks",
		Metadata: map[string]any{
			"source": "tfr:///terraform-aws-modules/eks/aws?version=19.0.0",
		},
	})
	if err != nil {
		t.Fatalf("buildContentRelationshipSet() error = %v, want nil", err)
	}
	if len(relationships.outgoing) != 1 {
		t.Fatalf("len(relationships.outgoing) = %d, want 1", len(relationships.outgoing))
	}
	relationship := relationships.outgoing[0]
	if got, want := relationship["type"], "USES_MODULE"; got != want {
		t.Fatalf("relationship[type] = %#v, want %#v", got, want)
	}
	if got, want := relationship["target_name"], "tfr:///terraform-aws-modules/eks/aws?version=19.0.0"; got != want {
		t.Fatalf("relationship[target_name] = %#v, want %#v", got, want)
	}
	if got, want := relationship["reason"], "terraform_module_source"; got != want {
		t.Fatalf("relationship[reason] = %#v, want %#v", got, want)
	}
}

func TestBuildContentRelationshipSetTerragruntConfigPromotesTerraformSource(t *testing.T) {
	t.Parallel()

	relationships, err := buildContentRelationshipSet(context.Background(), nil, EntityContent{
		EntityID:   "tg-config-1",
		RepoID:     "repo-1",
		EntityType: "TerragruntConfig",
		EntityName: "terragrunt",
		Metadata: map[string]any{
			"terraform_source": "../modules/app",
		},
	})
	if err != nil {
		t.Fatalf("buildContentRelationshipSet() error = %v, want nil", err)
	}
	if len(relationships.outgoing) != 1 {
		t.Fatalf("len(relationships.outgoing) = %d, want 1", len(relationships.outgoing))
	}
	relationship := relationships.outgoing[0]
	if got, want := relationship["type"], "USES_MODULE"; got != want {
		t.Fatalf("relationship[type] = %#v, want %#v", got, want)
	}
	if got, want := relationship["target_name"], "../modules/app"; got != want {
		t.Fatalf("relationship[target_name] = %#v, want %#v", got, want)
	}
	if got, want := relationship["reason"], "terragrunt_terraform_source"; got != want {
		t.Fatalf("relationship[reason] = %#v, want %#v", got, want)
	}
}

func TestBuildContentRelationshipSetTerragruntDependencyPromotesConfigPath(t *testing.T) {
	t.Parallel()

	relationships, err := buildContentRelationshipSet(context.Background(), nil, EntityContent{
		EntityID:   "tg-dep-1",
		RepoID:     "repo-1",
		EntityType: "TerragruntDependency",
		EntityName: "vpc",
		Metadata: map[string]any{
			"config_path": "../vpc",
		},
	})
	if err != nil {
		t.Fatalf("buildContentRelationshipSet() error = %v, want nil", err)
	}
	if len(relationships.outgoing) != 1 {
		t.Fatalf("len(relationships.outgoing) = %d, want 1", len(relationships.outgoing))
	}
	relationship := relationships.outgoing[0]
	if got, want := relationship["type"], "DISCOVERS_CONFIG_IN"; got != want {
		t.Fatalf("relationship[type] = %#v, want %#v", got, want)
	}
	if got, want := relationship["target_name"], "../vpc"; got != want {
		t.Fatalf("relationship[target_name] = %#v, want %#v", got, want)
	}
	if got, want := relationship["reason"], "terragrunt_dependency_config_path"; got != want {
		t.Fatalf("relationship[reason] = %#v, want %#v", got, want)
	}
}

func TestMetadataStringSliceSupportsCommaSeparatedStrings(t *testing.T) {
	t.Parallel()

	metadata := map[string]any{
		"template_source_repos": " https://github.com/myorg/platform-runtime.git , https://github.com/myorg/platform-ui.git ",
	}

	got := metadataStringSlice(metadata, "template_source_repos")
	want := []string{
		"https://github.com/myorg/platform-runtime.git",
		"https://github.com/myorg/platform-ui.git",
	}
	if len(got) != len(want) {
		t.Fatalf("len(metadataStringSlice()) = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("metadataStringSlice()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestMetadataStringSliceFiltersNilSentinelValues(t *testing.T) {
	t.Parallel()

	metadata := map[string]any{
		"template_source_paths": []any{"<nil>", "", "overlays/prod"},
	}

	got := metadataStringSlice(metadata, "template_source_paths")
	want := []string{"overlays/prod"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("metadataStringSlice() = %#v, want %#v", got, want)
	}
}

func TestContentEntityTypeForResolveMapsArgoCDAndIaCContentEntities(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		typeName string
		want     string
	}{
		{name: "argocd application", typeName: "argocd_application", want: "ArgoCDApplication"},
		{name: "argocd applicationset", typeName: "argocd_applicationset", want: "ArgoCDApplicationSet"},
		{name: "terraform block", typeName: "terraform_block", want: "TerraformBlock"},
		{name: "kustomize overlay", typeName: "kustomize_overlay", want: "KustomizeOverlay"},
		{name: "kubernetes resource", typeName: "k8s_resource", want: "K8sResource"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := contentEntityTypeForResolve(tt.typeName); got != tt.want {
				t.Fatalf("contentEntityTypeForResolve(%q) = %q, want %q", tt.typeName, got, tt.want)
			}
		})
	}
}

func TestBuildContentRelationshipSetArgoCDMetadataFromMaterializedStrings(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{
				{
					"argocd-appset-1", "repo-1", "argocd/applicationset.yaml", "ArgoCDApplicationSet", "platform-appset",
					int64(1), int64(22), "yaml", "kind: ApplicationSet", []byte(`{"generator_source_repos":"https://github.com/myorg/platform-config.git","template_source_repos":"https://github.com/myorg/platform-runtime.git","dest_server":"https://kubernetes.default.svc"}`),
				},
			},
		},
	})
	reader := NewContentReader(db)

	entity, err := reader.GetEntityContent(context.Background(), "argocd-appset-1")
	if err != nil {
		t.Fatalf("GetEntityContent() error = %v, want nil", err)
	}
	if entity == nil {
		t.Fatal("GetEntityContent() = nil, want entity")
	}

	relationships, err := buildContentRelationshipSet(context.Background(), reader, *entity)
	if err != nil {
		t.Fatalf("buildContentRelationshipSet() error = %v, want nil", err)
	}
	if len(relationships.outgoing) != 3 {
		t.Fatalf("len(relationships.outgoing) = %d, want 3", len(relationships.outgoing))
	}
}
