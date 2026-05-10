package projector

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

func TestBuildCanonicalMaterializationExtractsTerraformStateRows(t *testing.T) {
	t.Parallel()

	sc := terraformStateScope()
	gen := terraformStateGeneration()
	result := buildCanonicalMaterialization(sc, gen, terraformStateFacts())

	if got, want := len(result.TerraformStateResources), 1; got != want {
		t.Fatalf("len(TerraformStateResources) = %d, want %d", got, want)
	}
	resource := result.TerraformStateResources[0]
	if got, want := resource.Address, "module.app.aws_instance.web"; got != want {
		t.Fatalf("resource Address = %q, want %q", got, want)
	}
	if got, want := resource.ResourceType, "aws_instance"; got != want {
		t.Fatalf("resource ResourceType = %q, want %q", got, want)
	}
	if got, want := resource.ModuleAddress, "module.app"; got != want {
		t.Fatalf("resource ModuleAddress = %q, want %q", got, want)
	}
	if got, want := resource.ProviderAddress, "provider[\"registry.terraform.io/hashicorp/aws\"]"; got != want {
		t.Fatalf("resource ProviderAddress = %q, want %q", got, want)
	}
	if got, want := resource.Lineage, "lineage-123"; got != want {
		t.Fatalf("resource Lineage = %q, want %q", got, want)
	}
	if got, want := resource.Serial, int64(17); got != want {
		t.Fatalf("resource Serial = %d, want %d", got, want)
	}
	if got, want := resource.SourceConfidence, facts.SourceConfidenceObserved; got != want {
		t.Fatalf("resource SourceConfidence = %q, want %q", got, want)
	}
	if got, want := resource.CorrelationAnchors[0], "arn:anchor-hash-1"; got != want {
		t.Fatalf("resource CorrelationAnchors[0] = %q, want %q", got, want)
	}
	if got, want := resource.TagKeyHashes[0], "tag-key-hash-1"; got != want {
		t.Fatalf("resource TagKeyHashes[0] = %q, want %q", got, want)
	}

	if got, want := len(result.TerraformStateModules), 1; got != want {
		t.Fatalf("len(TerraformStateModules) = %d, want %d", got, want)
	}
	module := result.TerraformStateModules[0]
	if got, want := module.ModuleAddress, "module.app"; got != want {
		t.Fatalf("module ModuleAddress = %q, want %q", got, want)
	}
	if got, want := module.ResourceCount, int64(1); got != want {
		t.Fatalf("module ResourceCount = %d, want %d", got, want)
	}

	if got, want := len(result.TerraformStateOutputs), 1; got != want {
		t.Fatalf("len(TerraformStateOutputs) = %d, want %d", got, want)
	}
	output := result.TerraformStateOutputs[0]
	if got, want := output.Name, "web_instance_id"; got != want {
		t.Fatalf("output Name = %q, want %q", got, want)
	}
	if !output.Sensitive {
		t.Fatal("output Sensitive = false, want true")
	}
	if got, want := output.ValueShape, "redacted_scalar"; got != want {
		t.Fatalf("output ValueShape = %q, want %q", got, want)
	}
}

func TestBuildCanonicalMaterializationAggregatesTerraformStateModuleObservations(t *testing.T) {
	t.Parallel()

	sc := terraformStateScope()
	gen := terraformStateGeneration()
	input := terraformStateFacts()
	observedAt := gen.ObservedAt.Add(time.Second)
	input = append(input, facts.Envelope{
		FactID:           "tf-module-2",
		ScopeID:          "tf-scope-1",
		GenerationID:     "tf-generation-1",
		FactKind:         facts.TerraformStateModuleFactKind,
		StableFactKey:    "terraform_state_module:module:module.app:resource:module.app.aws_security_group.web",
		SchemaVersion:    facts.TerraformStateModuleSchemaVersion,
		CollectorKind:    string(scope.CollectorTerraformState),
		SourceConfidence: facts.SourceConfidenceObserved,
		ObservedAt:       observedAt,
		Payload: map[string]any{
			"module_address": "module.app",
			"resource_count": int64(1),
		},
		SourceRef: facts.Ref{
			SourceSystem:   string(scope.CollectorTerraformState),
			ScopeID:        "tf-scope-1",
			GenerationID:   "tf-generation-1",
			FactKey:        "terraform_state_module:module:module.app:resource:module.app.aws_security_group.web",
			SourceRecordID: "module.app:resource:module.app.aws_security_group.web",
		},
	})

	result := buildCanonicalMaterialization(sc, gen, input)

	if got, want := len(result.TerraformStateModules), 1; got != want {
		t.Fatalf("len(TerraformStateModules) = %d, want %d", got, want)
	}
	module := result.TerraformStateModules[0]
	if got, want := module.ModuleAddress, "module.app"; got != want {
		t.Fatalf("ModuleAddress = %q, want %q", got, want)
	}
	if got, want := module.ResourceCount, int64(2); got != want {
		t.Fatalf("ResourceCount = %d, want %d", got, want)
	}
	if got, want := module.SourceFactID, "tf-module-2"; got != want {
		t.Fatalf("SourceFactID = %q, want %q", got, want)
	}
}

func TestRuntimeProjectRejectsUnknownTerraformStateSchemaVersion(t *testing.T) {
	t.Parallel()

	runtime := Runtime{
		CanonicalWriter: &recordingCanonicalWriter{},
		ContentWriter:   &recordingContentWriter{},
	}

	_, err := runtime.Project(
		context.Background(),
		terraformStateScope(),
		terraformStateGeneration(),
		[]facts.Envelope{{
			FactID:        "tf-resource-1",
			ScopeID:       "tf-scope-1",
			GenerationID:  "tf-generation-1",
			FactKind:      facts.TerraformStateResourceFactKind,
			SchemaVersion: "2.0.0",
			Payload: map[string]any{
				"address": "aws_instance.web",
			},
		}},
	)
	if err == nil {
		t.Fatal("Project() error = nil, want non-nil")
	}
}

func TestRuntimeProjectPublishesTerraformStateCanonicalCheckpoints(t *testing.T) {
	t.Parallel()

	canonicalWriter := &recordingCanonicalWriter{}
	publisher := &recordingGraphProjectionPhasePublisher{}
	runtime := Runtime{
		CanonicalWriter: canonicalWriter,
		ContentWriter:   &recordingContentWriter{},
		PhasePublisher:  publisher,
	}

	_, err := runtime.Project(context.Background(), terraformStateScope(), terraformStateGeneration(), terraformStateFacts())
	if err != nil {
		t.Fatalf("Project() error = %v, want nil", err)
	}
	if got, want := len(canonicalWriter.calls), 1; got != want {
		t.Fatalf("canonical writer calls = %d, want %d", got, want)
	}
	if got, want := len(publisher.calls), 1; got != want {
		t.Fatalf("publisher calls = %d, want %d", got, want)
	}

	gotKeyspaces := map[reducer.GraphProjectionKeyspace]bool{}
	for _, row := range publisher.calls[0] {
		gotKeyspaces[row.Key.Keyspace] = true
		if got, want := row.Phase, reducer.GraphProjectionPhaseCanonicalNodesCommitted; got != want {
			t.Fatalf("published phase = %q, want %q", got, want)
		}
		if got, want := row.Key.AcceptanceUnitID, "tf-scope-1"; got != want {
			t.Fatalf("published acceptance unit = %q, want %q", got, want)
		}
	}
	for _, want := range []reducer.GraphProjectionKeyspace{
		reducer.GraphProjectionKeyspaceTerraformResourceUID,
		reducer.GraphProjectionKeyspaceTerraformModuleUID,
	} {
		if !gotKeyspaces[want] {
			t.Fatalf("published keyspaces = %#v, missing %q", gotKeyspaces, want)
		}
	}
}

func terraformStateScope() scope.IngestionScope {
	return scope.IngestionScope{
		ScopeID:       "tf-scope-1",
		SourceSystem:  string(scope.CollectorTerraformState),
		ScopeKind:     scope.KindStateSnapshot,
		CollectorKind: scope.CollectorTerraformState,
		PartitionKey:  "terraform_state:s3:locator-hash-1",
		Metadata: map[string]string{
			"backend_kind": "s3",
			"locator_hash": "locator-hash-1",
		},
	}
}

func terraformStateGeneration() scope.ScopeGeneration {
	return scope.ScopeGeneration{
		GenerationID: "tf-generation-1",
		ScopeID:      "tf-scope-1",
		ObservedAt:   time.Date(2026, time.May, 10, 12, 0, 0, 0, time.UTC),
		IngestedAt:   time.Date(2026, time.May, 10, 12, 1, 0, 0, time.UTC),
		Status:       scope.GenerationStatusPending,
		TriggerKind:  scope.TriggerKindSnapshot,
	}
}

func terraformStateFacts() []facts.Envelope {
	observedAt := time.Date(2026, time.May, 10, 12, 0, 0, 0, time.UTC)
	return []facts.Envelope{
		{
			FactID:           "tf-snapshot-1",
			ScopeID:          "tf-scope-1",
			GenerationID:     "tf-generation-1",
			FactKind:         facts.TerraformStateSnapshotFactKind,
			StableFactKey:    "terraform_state_snapshot:snapshot",
			SchemaVersion:    facts.TerraformStateSnapshotSchemaVersion,
			CollectorKind:    string(scope.CollectorTerraformState),
			SourceConfidence: facts.SourceConfidenceObserved,
			ObservedAt:       observedAt,
			Payload: map[string]any{
				"format_version":    "1.9",
				"terraform_version": "1.9.8",
				"serial":            int64(17),
				"lineage":           "lineage-123",
				"backend_kind":      "s3",
				"locator_hash":      "locator-hash-1",
			},
			SourceRef: facts.Ref{
				SourceSystem:   string(scope.CollectorTerraformState),
				ScopeID:        "tf-scope-1",
				GenerationID:   "tf-generation-1",
				FactKey:        "terraform_state_snapshot:snapshot",
				SourceRecordID: "locator-hash-1",
			},
		},
		{
			FactID:           "tf-resource-1",
			ScopeID:          "tf-scope-1",
			GenerationID:     "tf-generation-1",
			FactKind:         facts.TerraformStateResourceFactKind,
			StableFactKey:    "terraform_state_resource:resource:module.app.aws_instance.web",
			SchemaVersion:    facts.TerraformStateResourceSchemaVersion,
			CollectorKind:    string(scope.CollectorTerraformState),
			SourceConfidence: facts.SourceConfidenceObserved,
			ObservedAt:       observedAt,
			Payload: map[string]any{
				"address":  "module.app.aws_instance.web",
				"mode":     "managed",
				"type":     "aws_instance",
				"name":     "web",
				"module":   "module.app",
				"provider": "provider[\"registry.terraform.io/hashicorp/aws\"]",
				"correlation_anchors": []any{
					map[string]any{"anchor_kind": "arn", "value_hash": "anchor-hash-1"},
				},
			},
			SourceRef: facts.Ref{
				SourceSystem:   string(scope.CollectorTerraformState),
				ScopeID:        "tf-scope-1",
				GenerationID:   "tf-generation-1",
				FactKey:        "terraform_state_resource:resource:module.app.aws_instance.web",
				SourceRecordID: "module.app.aws_instance.web",
			},
		},
		{
			FactID:           "tf-module-1",
			ScopeID:          "tf-scope-1",
			GenerationID:     "tf-generation-1",
			FactKind:         facts.TerraformStateModuleFactKind,
			StableFactKey:    "terraform_state_module:module:module.app",
			SchemaVersion:    facts.TerraformStateModuleSchemaVersion,
			CollectorKind:    string(scope.CollectorTerraformState),
			SourceConfidence: facts.SourceConfidenceObserved,
			ObservedAt:       observedAt,
			Payload: map[string]any{
				"module_address": "module.app",
				"resource_count": int64(1),
			},
		},
		{
			FactID:           "tf-tag-1",
			ScopeID:          "tf-scope-1",
			GenerationID:     "tf-generation-1",
			FactKind:         facts.TerraformStateTagObservationFactKind,
			StableFactKey:    "terraform_state_tag_observation:tag_observation:module.app.aws_instance.web:tags:tag-key-hash-1",
			SchemaVersion:    facts.TerraformStateTagObservationSchemaVersion,
			CollectorKind:    string(scope.CollectorTerraformState),
			SourceConfidence: facts.SourceConfidenceObserved,
			ObservedAt:       observedAt,
			Payload: map[string]any{
				"resource_address": "module.app.aws_instance.web",
				"tag_source":       "tags",
				"tag_key_hash":     "tag-key-hash-1",
			},
		},
		{
			FactID:           "tf-output-1",
			ScopeID:          "tf-scope-1",
			GenerationID:     "tf-generation-1",
			FactKind:         facts.TerraformStateOutputFactKind,
			StableFactKey:    "terraform_state_output:output:web_instance_id",
			SchemaVersion:    facts.TerraformStateOutputSchemaVersion,
			CollectorKind:    string(scope.CollectorTerraformState),
			SourceConfidence: facts.SourceConfidenceObserved,
			ObservedAt:       observedAt,
			Payload: map[string]any{
				"name":      "web_instance_id",
				"sensitive": true,
				"value": map[string]any{
					"redacted": true,
					"reason":   "sensitive_output",
				},
			},
		},
	}
}
