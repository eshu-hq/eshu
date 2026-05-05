package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// TestGoCollectorContentFactsProduceEvidenceDuringIngestion verifies that the
// Go collector's content fact format (content_path / content_body) flows
// through evidence discovery during CommitScopeGeneration. This is the
// integration complement to the unit-level TestDiscoverEvidenceFromGoCollector*
// tests in the relationships package.
func TestGoCollectorContentFactsProduceEvidenceDuringIngestion(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 15, 10, 0, 0, 0, time.UTC)
	db := newProofDomainDB(t, now)

	// Step 1: Ingest a target repository so the catalog has an entry to match.
	targetScope := scope.IngestionScope{
		ScopeID:       "scope-target-go",
		SourceSystem:  "git",
		ScopeKind:     scope.KindRepository,
		CollectorKind: scope.CollectorGit,
		PartitionKey:  "repo-target-go",
		Metadata:      map[string]string{"repo_id": "repo-target-go"},
	}
	targetGeneration := scope.ScopeGeneration{
		GenerationID: "generation-target-go",
		ScopeID:      targetScope.ScopeID,
		ObservedAt:   time.Date(2026, time.April, 15, 9, 0, 0, 0, time.UTC),
		IngestedAt:   now,
		Status:       scope.GenerationStatusPending,
		TriggerKind:  scope.TriggerKindSnapshot,
	}
	targetFacts := []facts.Envelope{
		{
			FactID:        "target-repo-go",
			ScopeID:       targetScope.ScopeID,
			GenerationID:  targetGeneration.GenerationID,
			FactKind:      "repository",
			StableFactKey: "repository:repo-target-go",
			ObservedAt:    targetGeneration.ObservedAt,
			Payload: map[string]any{
				"graph_id":   "repo-target-go",
				"graph_kind": "repository",
				"name":       "order-service",
				"repo_id":    "repo-target-go",
				"repo_slug":  "order-service",
			},
			SourceRef: facts.Ref{
				SourceSystem: "git",
				FactKey:      "repo-target-go",
			},
		},
	}

	// Step 2: Ingest an infrastructure repository with Go collector content
	// facts that reference the target repository. The Go collector emits
	// content_path / content_body instead of relative_path / content.
	infraScope := scope.IngestionScope{
		ScopeID:       "scope-infra-go",
		SourceSystem:  "git",
		ScopeKind:     scope.KindRepository,
		CollectorKind: scope.CollectorGit,
		PartitionKey:  "repo-infra-go",
		Metadata:      map[string]string{"repo_id": "repo-infra-go"},
	}
	infraGeneration := scope.ScopeGeneration{
		GenerationID: "generation-infra-go",
		ScopeID:      infraScope.ScopeID,
		ObservedAt:   time.Date(2026, time.April, 15, 9, 30, 0, 0, time.UTC),
		IngestedAt:   now,
		Status:       scope.GenerationStatusPending,
		TriggerKind:  scope.TriggerKindSnapshot,
	}
	infraFacts := []facts.Envelope{
		{
			FactID:        "infra-repo-go",
			ScopeID:       infraScope.ScopeID,
			GenerationID:  infraGeneration.GenerationID,
			FactKind:      "repository",
			StableFactKey: "repository:repo-infra-go",
			ObservedAt:    infraGeneration.ObservedAt,
			Payload: map[string]any{
				"graph_id":   "repo-infra-go",
				"graph_kind": "repository",
				"name":       "infra-go-repo",
				"repo_id":    "repo-infra-go",
			},
			SourceRef: facts.Ref{
				SourceSystem: "git",
				FactKey:      "repo-infra-go",
			},
		},
		{
			FactID:        "infra-tf-go",
			ScopeID:       infraScope.ScopeID,
			GenerationID:  infraGeneration.GenerationID,
			FactKind:      "content",
			StableFactKey: "content:deploy/main.tf",
			ObservedAt:    infraGeneration.ObservedAt,
			// Go collector format: content_path / content_body
			Payload: map[string]any{
				"content_path":   "deploy/main.tf",
				"content_body":   `module "order" { source = "git::https://github.com/example/order-service.git?ref=v1.2.0" }`,
				"content_digest": "sha256:abc123",
				"repo_id":        "repo-infra-go",
				"language":       "hcl",
				"artifact_type":  "terraform_hcl",
			},
			SourceRef: facts.Ref{
				SourceSystem: "git",
				FactKey:      "deploy/main.tf",
			},
		},
	}

	ingestionStore := NewIngestionStore(db)
	ingestionStore.Now = func() time.Time { return now }

	collectorCtx, cancelCollector := context.WithCancel(context.Background())
	defer cancelCollector()

	collectorService := collector.Service{
		Source: &proofCollectorSource{
			collected: []collector.CollectedGeneration{
				collector.FactsFromSlice(targetScope, targetGeneration, targetFacts),
				collector.FactsFromSlice(infraScope, infraGeneration, infraFacts),
			},
		},
		Committer: collectorCommitterFunc(func(
			ctx context.Context,
			scopeValue scope.IngestionScope,
			generationValue scope.ScopeGeneration,
			factStream <-chan facts.Envelope,
		) error {
			defer cancelCollector()
			return ingestionStore.CommitScopeGeneration(ctx, scopeValue, generationValue, factStream)
		}),
		PollInterval: time.Millisecond,
	}
	if err := collectorService.Run(collectorCtx); err != nil {
		t.Fatalf("collector service Run() error = %v, want nil", err)
	}

	// Verify evidence facts were discovered from Go collector content format.
	if got := len(db.state.evidenceFacts); got == 0 {
		t.Fatal("evidence facts = 0, want >= 1: Go collector content_path/content_body format was not processed by evidence discovery")
	}

	// Find the evidence record and verify it references the target repo.
	var found bool
	for _, record := range db.state.evidenceFacts {
		if record.generationID != infraGeneration.GenerationID {
			continue
		}
		if record.targetRepoID != "repo-target-go" {
			t.Errorf("evidence target = %q, want %q", record.targetRepoID, "repo-target-go")
			continue
		}
		found = true

		// Verify evidence details include path from the Go collector format.
		if path, ok := record.details["path"]; !ok || path != "deploy/main.tf" {
			t.Errorf("evidence details path = %v, want %q", path, "deploy/main.tf")
		}
	}
	if !found {
		t.Fatal("no evidence fact found targeting repo-target-go from Go collector content format")
	}
}

// TestGoCollectorContentFactsBackfillEvidenceWhenTargetArrivesLater verifies
// that relationship evidence is discovered even when the source repository is
// ingested before the target repository exists in the repository catalog.
func TestGoCollectorContentFactsBackfillEvidenceWhenTargetArrivesLater(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 15, 10, 15, 0, 0, time.UTC)
	db := newProofDomainDB(t, now)

	sourceScope := scope.IngestionScope{
		ScopeID:       "scope-infra-go-late-target",
		SourceSystem:  "git",
		ScopeKind:     scope.KindRepository,
		CollectorKind: scope.CollectorGit,
		PartitionKey:  "repo-infra-go-late-target",
		Metadata:      map[string]string{"repo_id": "repo-infra-go-late-target"},
	}
	sourceGeneration := scope.ScopeGeneration{
		GenerationID: "generation-infra-go-late-target",
		ScopeID:      sourceScope.ScopeID,
		ObservedAt:   time.Date(2026, time.April, 15, 9, 15, 0, 0, time.UTC),
		IngestedAt:   now,
		Status:       scope.GenerationStatusPending,
		TriggerKind:  scope.TriggerKindSnapshot,
	}
	sourceFacts := []facts.Envelope{
		{
			FactID:        "infra-repo-go-late-target",
			ScopeID:       sourceScope.ScopeID,
			GenerationID:  sourceGeneration.GenerationID,
			FactKind:      "repository",
			StableFactKey: "repository:repo-infra-go-late-target",
			ObservedAt:    sourceGeneration.ObservedAt,
			Payload: map[string]any{
				"graph_id":   "repo-infra-go-late-target",
				"graph_kind": "repository",
				"name":       "infra-go-repo-late-target",
				"repo_id":    "repo-infra-go-late-target",
			},
			SourceRef: facts.Ref{SourceSystem: "git", FactKey: "repo-infra-go-late-target"},
		},
		{
			FactID:        "infra-tf-go-late-target",
			ScopeID:       sourceScope.ScopeID,
			GenerationID:  sourceGeneration.GenerationID,
			FactKind:      "content",
			StableFactKey: "content:deploy/main.tf",
			ObservedAt:    sourceGeneration.ObservedAt,
			Payload: map[string]any{
				"content_path":   "deploy/main.tf",
				"content_body":   `module "order" { source = "git::https://github.com/example/order-service.git?ref=v1.2.0" }`,
				"content_digest": "sha256:late123",
				"repo_id":        "repo-infra-go-late-target",
				"language":       "hcl",
				"artifact_type":  "terraform_hcl",
			},
			SourceRef: facts.Ref{SourceSystem: "git", FactKey: "deploy/main.tf"},
		},
	}

	targetScope := scope.IngestionScope{
		ScopeID:       "scope-target-go-late",
		SourceSystem:  "git",
		ScopeKind:     scope.KindRepository,
		CollectorKind: scope.CollectorGit,
		PartitionKey:  "repo-target-go-late",
		Metadata:      map[string]string{"repo_id": "repo-target-go-late"},
	}
	targetGeneration := scope.ScopeGeneration{
		GenerationID: "generation-target-go-late",
		ScopeID:      targetScope.ScopeID,
		ObservedAt:   time.Date(2026, time.April, 15, 9, 45, 0, 0, time.UTC),
		IngestedAt:   now,
		Status:       scope.GenerationStatusPending,
		TriggerKind:  scope.TriggerKindSnapshot,
	}
	targetFacts := []facts.Envelope{
		{
			FactID:        "target-repo-go-late",
			ScopeID:       targetScope.ScopeID,
			GenerationID:  targetGeneration.GenerationID,
			FactKind:      "repository",
			StableFactKey: "repository:repo-target-go-late",
			ObservedAt:    targetGeneration.ObservedAt,
			Payload: map[string]any{
				"graph_id":   "repo-target-go-late",
				"graph_kind": "repository",
				"name":       "order-service",
				"repo_id":    "repo-target-go-late",
				"repo_slug":  "order-service",
			},
			SourceRef: facts.Ref{SourceSystem: "git", FactKey: "repo-target-go-late"},
		},
	}

	ingestionStore := NewIngestionStore(db)
	ingestionStore.Now = func() time.Time { return now }

	collectorCtx, cancelCollector := context.WithCancel(context.Background())
	defer cancelCollector()

	collectorService := collector.Service{
		Source: &proofCollectorSource{
			collected: []collector.CollectedGeneration{
				collector.FactsFromSlice(sourceScope, sourceGeneration, sourceFacts),
				collector.FactsFromSlice(targetScope, targetGeneration, targetFacts),
			},
		},
		Committer: collectorCommitterFunc(func(
			ctx context.Context,
			scopeValue scope.IngestionScope,
			generationValue scope.ScopeGeneration,
			factStream <-chan facts.Envelope,
		) error {
			defer cancelCollector()
			return ingestionStore.CommitScopeGeneration(ctx, scopeValue, generationValue, factStream)
		}),
		PollInterval: time.Millisecond,
	}
	if err := collectorService.Run(collectorCtx); err != nil {
		t.Fatalf("collector service Run() error = %v, want nil", err)
	}

	var found bool
	for _, record := range db.state.evidenceFacts {
		if record.targetRepoID != "repo-target-go-late" {
			continue
		}
		if path, ok := record.details["path"]; !ok || path != "deploy/main.tf" {
			t.Errorf("evidence details path = %v, want %q", path, "deploy/main.tf")
		}
		found = true
	}
	if !found {
		t.Fatal("no evidence fact found for repo-target-go-late when the target repository arrived after the source repository")
	}
}

// TestGoCollectorHelmFactsProduceEvidenceDuringIngestion verifies that Helm
// chart references using Go collector content_path / content_body format
// are discovered during ingestion.
func TestGoCollectorHelmFactsProduceEvidenceDuringIngestion(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 15, 11, 0, 0, 0, time.UTC)
	db := newProofDomainDB(t, now)

	targetScope := scope.IngestionScope{
		ScopeID:       "scope-helm-target",
		SourceSystem:  "git",
		ScopeKind:     scope.KindRepository,
		CollectorKind: scope.CollectorGit,
		PartitionKey:  "repo-helm-target",
		Metadata:      map[string]string{"repo_id": "repo-helm-target"},
	}
	targetGeneration := scope.ScopeGeneration{
		GenerationID: "gen-helm-target",
		ScopeID:      targetScope.ScopeID,
		ObservedAt:   time.Date(2026, time.April, 15, 10, 0, 0, 0, time.UTC),
		IngestedAt:   now,
		Status:       scope.GenerationStatusPending,
		TriggerKind:  scope.TriggerKindSnapshot,
	}
	targetFacts := []facts.Envelope{
		{
			FactID:        "helm-target-repo",
			ScopeID:       targetScope.ScopeID,
			GenerationID:  targetGeneration.GenerationID,
			FactKind:      "repository",
			StableFactKey: "repository:repo-helm-target",
			ObservedAt:    targetGeneration.ObservedAt,
			Payload: map[string]any{
				"graph_id":   "repo-helm-target",
				"graph_kind": "repository",
				"name":       "auth-service",
				"repo_id":    "repo-helm-target",
				"repo_slug":  "auth-service",
			},
			SourceRef: facts.Ref{SourceSystem: "git", FactKey: "repo-helm-target"},
		},
	}

	helmScope := scope.IngestionScope{
		ScopeID:       "scope-helm-deploy",
		SourceSystem:  "git",
		ScopeKind:     scope.KindRepository,
		CollectorKind: scope.CollectorGit,
		PartitionKey:  "repo-helm-deploy",
		Metadata:      map[string]string{"repo_id": "repo-helm-deploy"},
	}
	helmGeneration := scope.ScopeGeneration{
		GenerationID: "gen-helm-deploy",
		ScopeID:      helmScope.ScopeID,
		ObservedAt:   time.Date(2026, time.April, 15, 10, 30, 0, 0, time.UTC),
		IngestedAt:   now,
		Status:       scope.GenerationStatusPending,
		TriggerKind:  scope.TriggerKindSnapshot,
	}
	helmFacts := []facts.Envelope{
		{
			FactID:        "helm-deploy-repo",
			ScopeID:       helmScope.ScopeID,
			GenerationID:  helmGeneration.GenerationID,
			FactKind:      "repository",
			StableFactKey: "repository:repo-helm-deploy",
			ObservedAt:    helmGeneration.ObservedAt,
			Payload: map[string]any{
				"graph_id":   "repo-helm-deploy",
				"graph_kind": "repository",
				"name":       "deploy-charts",
				"repo_id":    "repo-helm-deploy",
			},
			SourceRef: facts.Ref{SourceSystem: "git", FactKey: "repo-helm-deploy"},
		},
		{
			FactID:        "helm-values-fact",
			ScopeID:       helmScope.ScopeID,
			GenerationID:  helmGeneration.GenerationID,
			FactKind:      "content",
			StableFactKey: "content:charts/auth/values.yaml",
			ObservedAt:    helmGeneration.ObservedAt,
			Payload: map[string]any{
				"content_path":   "charts/auth/values.yaml",
				"content_body":   "image:\n  repository: auth-service\n  tag: latest\n",
				"content_digest": "sha256:def456",
				"repo_id":        "repo-helm-deploy",
			},
			SourceRef: facts.Ref{SourceSystem: "git", FactKey: "charts/auth/values.yaml"},
		},
	}

	ingestionStore := NewIngestionStore(db)
	ingestionStore.Now = func() time.Time { return now }

	collectorCtx, cancelCollector := context.WithCancel(context.Background())
	defer cancelCollector()

	collectorService := collector.Service{
		Source: &proofCollectorSource{
			collected: []collector.CollectedGeneration{
				collector.FactsFromSlice(targetScope, targetGeneration, targetFacts),
				collector.FactsFromSlice(helmScope, helmGeneration, helmFacts),
			},
		},
		Committer: collectorCommitterFunc(func(
			ctx context.Context,
			scopeValue scope.IngestionScope,
			generationValue scope.ScopeGeneration,
			factStream <-chan facts.Envelope,
		) error {
			defer cancelCollector()
			return ingestionStore.CommitScopeGeneration(ctx, scopeValue, generationValue, factStream)
		}),
		PollInterval: time.Millisecond,
	}
	if err := collectorService.Run(collectorCtx); err != nil {
		t.Fatalf("collector service Run() error = %v, want nil", err)
	}

	if got := len(db.state.evidenceFacts); got == 0 {
		t.Fatal("evidence facts = 0, want >= 1: Helm values with Go collector content_path/content_body format was not processed")
	}

	var found bool
	for _, record := range db.state.evidenceFacts {
		if record.generationID != helmGeneration.GenerationID {
			continue
		}
		if record.targetRepoID != "repo-helm-target" {
			continue
		}
		found = true
		if path, ok := record.details["path"]; !ok || path != "charts/auth/values.yaml" {
			t.Errorf("evidence details path = %v, want %q", path, "charts/auth/values.yaml")
		}
	}
	if !found {
		t.Fatal("no evidence fact found targeting repo-helm-target from Go collector Helm values format")
	}
}

// TestGoCollectorArgoCDFactsProduceEvidenceDuringIngestion verifies that
// ArgoCD Application specs using Go collector content_path / content_body
// format are discovered during ingestion.
func TestGoCollectorArgoCDFactsProduceEvidenceDuringIngestion(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 15, 12, 0, 0, 0, time.UTC)
	db := newProofDomainDB(t, now)

	targetScope := scope.IngestionScope{
		ScopeID:       "scope-argocd-target",
		SourceSystem:  "git",
		ScopeKind:     scope.KindRepository,
		CollectorKind: scope.CollectorGit,
		PartitionKey:  "repo-argocd-target",
		Metadata:      map[string]string{"repo_id": "repo-argocd-target"},
	}
	targetGeneration := scope.ScopeGeneration{
		GenerationID: "gen-argocd-target",
		ScopeID:      targetScope.ScopeID,
		ObservedAt:   time.Date(2026, time.April, 15, 11, 0, 0, 0, time.UTC),
		IngestedAt:   now,
		Status:       scope.GenerationStatusPending,
		TriggerKind:  scope.TriggerKindSnapshot,
	}
	targetFacts := []facts.Envelope{
		{
			FactID:        "argocd-target-repo",
			ScopeID:       targetScope.ScopeID,
			GenerationID:  targetGeneration.GenerationID,
			FactKind:      "repository",
			StableFactKey: "repository:repo-argocd-target",
			ObservedAt:    targetGeneration.ObservedAt,
			Payload: map[string]any{
				"graph_id":   "repo-argocd-target",
				"graph_kind": "repository",
				"name":       "billing-api",
				"repo_id":    "repo-argocd-target",
				"repo_slug":  "billing-api",
			},
			SourceRef: facts.Ref{SourceSystem: "git", FactKey: "repo-argocd-target"},
		},
	}

	argoScope := scope.IngestionScope{
		ScopeID:       "scope-argocd-infra",
		SourceSystem:  "git",
		ScopeKind:     scope.KindRepository,
		CollectorKind: scope.CollectorGit,
		PartitionKey:  "repo-argocd-infra",
		Metadata:      map[string]string{"repo_id": "repo-argocd-infra"},
	}
	argoGeneration := scope.ScopeGeneration{
		GenerationID: "gen-argocd-infra",
		ScopeID:      argoScope.ScopeID,
		ObservedAt:   time.Date(2026, time.April, 15, 11, 30, 0, 0, time.UTC),
		IngestedAt:   now,
		Status:       scope.GenerationStatusPending,
		TriggerKind:  scope.TriggerKindSnapshot,
	}
	argoFacts := []facts.Envelope{
		{
			FactID:        "argocd-infra-repo",
			ScopeID:       argoScope.ScopeID,
			GenerationID:  argoGeneration.GenerationID,
			FactKind:      "repository",
			StableFactKey: "repository:repo-argocd-infra",
			ObservedAt:    argoGeneration.ObservedAt,
			Payload: map[string]any{
				"graph_id":   "repo-argocd-infra",
				"graph_kind": "repository",
				"name":       "gitops-config",
				"repo_id":    "repo-argocd-infra",
			},
			SourceRef: facts.Ref{SourceSystem: "git", FactKey: "repo-argocd-infra"},
		},
		{
			FactID:        "argocd-app-fact",
			ScopeID:       argoScope.ScopeID,
			GenerationID:  argoGeneration.GenerationID,
			FactKind:      "content",
			StableFactKey: "content:apps/billing.yaml",
			ObservedAt:    argoGeneration.ObservedAt,
			Payload: map[string]any{
				"content_path":   "apps/billing.yaml",
				"content_body":   "apiVersion: argoproj.io/v1alpha1\nkind: Application\nmetadata:\n  name: billing-api\nspec:\n  source:\n    repoURL: https://github.com/example/billing-api.git\n    path: deploy\n",
				"content_digest": "sha256:ghi789",
				"repo_id":        "repo-argocd-infra",
			},
			SourceRef: facts.Ref{SourceSystem: "git", FactKey: "apps/billing.yaml"},
		},
	}

	ingestionStore := NewIngestionStore(db)
	ingestionStore.Now = func() time.Time { return now }

	collectorCtx, cancelCollector := context.WithCancel(context.Background())
	defer cancelCollector()

	collectorService := collector.Service{
		Source: &proofCollectorSource{
			collected: []collector.CollectedGeneration{
				collector.FactsFromSlice(targetScope, targetGeneration, targetFacts),
				collector.FactsFromSlice(argoScope, argoGeneration, argoFacts),
			},
		},
		Committer: collectorCommitterFunc(func(
			ctx context.Context,
			scopeValue scope.IngestionScope,
			generationValue scope.ScopeGeneration,
			factStream <-chan facts.Envelope,
		) error {
			defer cancelCollector()
			return ingestionStore.CommitScopeGeneration(ctx, scopeValue, generationValue, factStream)
		}),
		PollInterval: time.Millisecond,
	}
	if err := collectorService.Run(collectorCtx); err != nil {
		t.Fatalf("collector service Run() error = %v, want nil", err)
	}

	if got := len(db.state.evidenceFacts); got == 0 {
		t.Fatal("evidence facts = 0, want >= 1: ArgoCD Application with Go collector content_path/content_body was not processed")
	}

	var found bool
	for _, record := range db.state.evidenceFacts {
		if record.generationID != argoGeneration.GenerationID {
			continue
		}
		if record.targetRepoID != "repo-argocd-target" {
			continue
		}
		found = true
		if path, ok := record.details["path"]; !ok || path != "apps/billing.yaml" {
			t.Errorf("evidence details path = %v, want %q", path, "apps/billing.yaml")
		}
	}
	if !found {
		t.Fatal("no evidence fact found targeting repo-argocd-target from Go collector ArgoCD format")
	}
}
