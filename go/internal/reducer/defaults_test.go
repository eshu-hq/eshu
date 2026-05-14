package reducer

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/correlation/drift/tfconfigstate"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/relationships"
	"github.com/eshu-hq/eshu/go/internal/relationships/tfstatebackend"
	"github.com/eshu-hq/eshu/go/internal/truth"
)

func TestNewDefaultRuntimeUsesDefaultDomainHandlers(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 15, 12, 0, 0, 0, time.UTC)
	codeCallLoader := &stubFactLoader{
		envelopes: []facts.Envelope{
			{
				FactKind: "repository",
				Payload: map[string]any{
					"repo_id":       "repo-code",
					"source_run_id": "run-code",
				},
			},
			{
				FactKind: "file",
				Payload: map[string]any{
					"repo_id":       "repo-code",
					"relative_path": "caller.py",
					"parsed_file_data": map[string]any{
						"path": "caller.py",
						"functions": []any{
							map[string]any{
								"name":        "handle",
								"line_number": 3,
								"uid":         "entity:handle",
							},
						},
						"function_calls_scip": []any{
							map[string]any{
								"caller_file": "caller.py",
								"caller_line": 3,
								"callee_file": "callee.py",
								"callee_line": 1,
							},
						},
					},
				},
			},
			{
				FactKind: "file",
				Payload: map[string]any{
					"repo_id":       "repo-code",
					"relative_path": "callee.py",
					"parsed_file_data": map[string]any{
						"path": "callee.py",
						"functions": []any{
							map[string]any{
								"name":        "callee",
								"line_number": 1,
								"uid":         "entity:callee",
							},
						},
					},
				},
			},
		},
	}
	codeCallWriter := &recordingCodeCallIntentWriter{}

	runtime, err := NewDefaultRuntime(DefaultHandlers{
		WorkloadIdentityWriter: &recordingWorkloadIdentityWriter{
			result: WorkloadIdentityWriteResult{
				CanonicalWrites: 1,
			},
		},
		CloudAssetResolutionWriter: &recordingCloudAssetResolutionWriter{
			result: CloudAssetResolutionWriteResult{
				CanonicalWrites: 1,
			},
		},
		PlatformMaterializationWriter: &recordingPlatformMaterializationWriter{
			result: PlatformMaterializationWriteResult{
				CanonicalWrites: 1,
			},
		},
		FactLoader:           codeCallLoader,
		CodeCallIntentWriter: codeCallWriter,
	})
	if err != nil {
		t.Fatalf("NewDefaultRuntime() error = %v, want nil", err)
	}

	workloadResult, err := runtime.Execute(context.Background(), Intent{
		IntentID:        "intent-1",
		ScopeID:         "scope-123",
		GenerationID:    "generation-456",
		SourceSystem:    "git",
		Domain:          DomainWorkloadIdentity,
		Cause:           "shared identity follow-up required",
		EntityKeys:      []string{"workload:eshu"},
		RelatedScopeIDs: []string{"scope-123"},
		EnqueuedAt:      time.Date(2026, time.April, 12, 12, 0, 0, 0, time.UTC),
		AvailableAt:     time.Date(2026, time.April, 12, 12, 0, 0, 0, time.UTC),
		Status:          IntentStatusClaimed,
	})
	if err != nil {
		t.Fatalf("runtime.Execute(workload) error = %v, want nil", err)
	}
	if got, want := workloadResult.Status, ResultStatusSucceeded; got != want {
		t.Fatalf("runtime.Execute(workload).Status = %q, want %q", got, want)
	}

	cloudAssetResult, err := runtime.Execute(context.Background(), Intent{
		IntentID:        "intent-7",
		ScopeID:         "scope-123",
		GenerationID:    "generation-456",
		SourceSystem:    "git",
		Domain:          DomainCloudAssetResolution,
		Cause:           "shared cloud asset follow-up required",
		EntityKeys:      []string{"aws:s3:bucket:logs-prod"},
		RelatedScopeIDs: []string{"scope-123"},
		EnqueuedAt:      time.Date(2026, time.April, 12, 12, 0, 0, 0, time.UTC),
		AvailableAt:     time.Date(2026, time.April, 12, 12, 0, 0, 0, time.UTC),
		Status:          IntentStatusClaimed,
	})
	if err != nil {
		t.Fatalf("runtime.Execute(cloud_asset) error = %v, want nil", err)
	}
	if got, want := cloudAssetResult.Status, ResultStatusSucceeded; got != want {
		t.Fatalf("runtime.Execute(cloud_asset).Status = %q, want %q", got, want)
	}

	deploymentResult, err := runtime.Execute(context.Background(), Intent{
		IntentID:        "intent-pm-1",
		ScopeID:         "scope-123",
		GenerationID:    "generation-456",
		SourceSystem:    "git",
		Domain:          DomainDeploymentMapping,
		Cause:           "platform binding discovered",
		EntityKeys:      []string{"platform:kubernetes:aws:prod-cluster"},
		RelatedScopeIDs: []string{"scope-123"},
		EnqueuedAt:      time.Date(2026, time.April, 12, 12, 0, 0, 0, time.UTC),
		AvailableAt:     time.Date(2026, time.April, 12, 12, 0, 0, 0, time.UTC),
		Status:          IntentStatusClaimed,
	})
	if err != nil {
		t.Fatalf("runtime.Execute(deployment_mapping) error = %v, want nil", err)
	}
	if got, want := deploymentResult.Status, ResultStatusSucceeded; got != want {
		t.Fatalf("runtime.Execute(deployment_mapping).Status = %q, want %q", got, want)
	}

	codeCallResult, err := runtime.Execute(context.Background(), Intent{
		IntentID:        "intent-code-call-1",
		ScopeID:         "scope-123",
		GenerationID:    "generation-456",
		SourceSystem:    "git",
		Domain:          DomainCodeCallMaterialization,
		Cause:           "parser follow-up required",
		EntityKeys:      []string{"repo-code"},
		RelatedScopeIDs: []string{"scope-123"},
		EnqueuedAt:      now,
		AvailableAt:     now,
		Status:          IntentStatusClaimed,
	})
	if err != nil {
		t.Fatalf("runtime.Execute(code_call) error = %v, want nil", err)
	}
	if got, want := codeCallResult.Status, ResultStatusSucceeded; got != want {
		t.Fatalf("runtime.Execute(code_call).Status = %q, want %q", got, want)
	}
	if got, want := codeCallResult.CanonicalWrites, 2; got != want {
		t.Fatalf("runtime.Execute(code_call).CanonicalWrites = %d, want %d", got, want)
	}
	if len(codeCallWriter.rows) != 2 {
		t.Fatalf("code call writer rows = %d, want 2", len(codeCallWriter.rows))
	}

	_, err = runtime.Execute(context.Background(), Intent{
		IntentID:        "intent-2",
		ScopeID:         "scope-123",
		GenerationID:    "generation-456",
		SourceSystem:    "git",
		Domain:          DomainGovernance,
		Cause:           "shared governance follow-up required",
		EntityKeys:      []string{"policy:default"},
		RelatedScopeIDs: []string{"scope-123"},
		EnqueuedAt:      time.Date(2026, time.April, 12, 12, 0, 0, 0, time.UTC),
		AvailableAt:     time.Date(2026, time.April, 12, 12, 0, 0, 0, time.UTC),
		Status:          IntentStatusClaimed,
	})
	if err == nil {
		t.Fatal("runtime.Execute(governance) error = nil, want non-nil")
	}
	if got, want := err.Error(), `domain "governance" is not registered`; got != want {
		t.Fatalf("runtime.Execute(governance) error = %q, want %q", got, want)
	}
}

func TestDefaultDomainDefinitionsMatchImplementedRuntimeCatalog(t *testing.T) {
	t.Parallel()

	got := DefaultDomainDefinitions()
	if len(got) != 8 {
		t.Fatalf("len(DefaultDomainDefinitions()) = %d, want 8", len(got))
	}
	if got[0].Domain != DomainWorkloadIdentity {
		t.Fatalf("DefaultDomainDefinitions()[0].Domain = %q, want %q", got[0].Domain, DomainWorkloadIdentity)
	}
	if got[0].TruthContract.CanonicalKind != "workload_identity" {
		t.Fatalf("DefaultDomainDefinitions()[0].TruthContract.CanonicalKind = %q, want %q", got[0].TruthContract.CanonicalKind, "workload_identity")
	}
	if !got[0].TruthContract.Supports(truth.LayerSourceDeclaration) {
		t.Fatal("DefaultDomainDefinitions()[0].TruthContract.Supports(source_declaration) = false, want true")
	}
	if got[1].Domain != DomainCloudAssetResolution {
		t.Fatalf("DefaultDomainDefinitions()[1].Domain = %q, want %q", got[1].Domain, DomainCloudAssetResolution)
	}
	if got[1].TruthContract.CanonicalKind != "cloud_asset" {
		t.Fatalf("DefaultDomainDefinitions()[1].TruthContract.CanonicalKind = %q, want %q", got[1].TruthContract.CanonicalKind, "cloud_asset")
	}
	if !got[1].TruthContract.Supports(truth.LayerAppliedDeclaration) {
		t.Fatal("DefaultDomainDefinitions()[1].TruthContract.Supports(applied_declaration) = false, want true")
	}
	if !got[1].TruthContract.Supports(truth.LayerObservedResource) {
		t.Fatal("DefaultDomainDefinitions()[1].TruthContract.Supports(observed_resource) = false, want true")
	}
	if got[3].Domain != DomainCodeCallMaterialization {
		t.Fatalf("DefaultDomainDefinitions()[3].Domain = %q, want %q", got[3].Domain, DomainCodeCallMaterialization)
	}
	if got[3].TruthContract.CanonicalKind != "code_call_materialization" {
		t.Fatalf("DefaultDomainDefinitions()[3].TruthContract.CanonicalKind = %q, want %q", got[3].TruthContract.CanonicalKind, "code_call_materialization")
	}
	if !got[3].TruthContract.Supports(truth.LayerSourceDeclaration) {
		t.Fatal("DefaultDomainDefinitions()[3].TruthContract.Supports(source_declaration) = false, want true")
	}
	if got[4].Domain != DomainWorkloadMaterialization {
		t.Fatalf("DefaultDomainDefinitions()[4].Domain = %q, want %q", got[4].Domain, DomainWorkloadMaterialization)
	}
	if got[5].Domain != DomainSemanticEntityMaterialization {
		t.Fatalf("DefaultDomainDefinitions()[5].Domain = %q, want %q", got[5].Domain, DomainSemanticEntityMaterialization)
	}
	if got[5].TruthContract.CanonicalKind != "semantic_entity_materialization" {
		t.Fatalf("DefaultDomainDefinitions()[5].TruthContract.CanonicalKind = %q, want %q", got[5].TruthContract.CanonicalKind, "semantic_entity_materialization")
	}
}

func TestParseDomainAcceptsDeployableUnitCorrelation(t *testing.T) {
	t.Parallel()

	got, err := ParseDomain(" deployable_unit_correlation ")
	if err != nil {
		t.Fatalf("ParseDomain() error = %v, want nil", err)
	}
	if got != DomainDeployableUnitCorrelation {
		t.Fatalf("ParseDomain() = %q, want %q", got, DomainDeployableUnitCorrelation)
	}
}

func TestDefaultHandlersWiresCrossRepoResolver(t *testing.T) {
	t.Parallel()

	intentWriter := &recordingRepoDependencyIntentWriter{}
	evidenceLoader := &fakeEvidenceFactLoader{
		facts: []relationships.EvidenceFact{
			{
				EvidenceKind:     relationships.EvidenceKindTerraformAppRepo,
				RelationshipType: relationships.RelProvisionsDependencyFor,
				SourceRepoID:     "infra-repo",
				TargetRepoID:     "app-repo",
				Confidence:       0.99,
				Rationale:        "Terraform app_repo reference",
			},
		},
	}

	runtime, err := NewDefaultRuntime(DefaultHandlers{
		WorkloadIdentityWriter: &recordingWorkloadIdentityWriter{
			result: WorkloadIdentityWriteResult{CanonicalWrites: 1},
		},
		CloudAssetResolutionWriter: &recordingCloudAssetResolutionWriter{
			result: CloudAssetResolutionWriteResult{CanonicalWrites: 1},
		},
		PlatformMaterializationWriter: &recordingPlatformMaterializationWriter{
			result: PlatformMaterializationWriteResult{CanonicalWrites: 1},
		},
		EvidenceFactLoader:         evidenceLoader,
		RepoDependencyIntentWriter: intentWriter,
	})
	if err != nil {
		t.Fatalf("NewDefaultRuntime() error = %v, want nil", err)
	}

	result, err := runtime.Execute(context.Background(), Intent{
		IntentID:        "intent-cross-repo",
		ScopeID:         "scope-1",
		GenerationID:    "gen-1",
		SourceSystem:    "git",
		Domain:          DomainDeploymentMapping,
		Cause:           "platform binding discovered",
		EntityKeys:      []string{"platform:kubernetes:aws:prod-cluster"},
		RelatedScopeIDs: []string{"scope-1"},
		EnqueuedAt:      time.Date(2026, time.April, 13, 12, 0, 0, 0, time.UTC),
		AvailableAt:     time.Date(2026, time.April, 13, 12, 0, 0, 0, time.UTC),
		Status:          IntentStatusClaimed,
	})
	if err != nil {
		t.Fatalf("runtime.Execute() error = %v, want nil", err)
	}
	if got, want := result.Status, ResultStatusSucceeded; got != want {
		t.Fatalf("result.Status = %q, want %q", got, want)
	}

	// Platform write (1) + cross-repo resolution (1) = 2.
	if got, want := result.CanonicalWrites, 2; got != want {
		t.Fatalf("result.CanonicalWrites = %d, want %d", got, want)
	}

	if len(intentWriter.rows) == 0 {
		t.Fatal("expected repo-dependency intent writes from cross-repo resolution")
	}
}

func TestNewDefaultRegistryWiresCrossRepoReadinessDependencies(t *testing.T) {
	t.Parallel()

	readinessLookup := func(GraphProjectionPhaseKey, GraphProjectionPhase) (bool, bool) {
		return false, false
	}
	readinessPrefetch := func(_ context.Context, _ []GraphProjectionPhaseKey, _ GraphProjectionPhase) (GraphProjectionReadinessLookup, error) {
		return readinessLookup, nil
	}

	registry, err := NewDefaultRegistry(DefaultHandlers{
		PlatformMaterializationWriter: &recordingPlatformMaterializationWriter{},
		EvidenceFactLoader:            &fakeEvidenceFactLoader{},
		RepoDependencyIntentWriter:    &recordingRepoDependencyIntentWriter{},
		ReadinessLookup:               readinessLookup,
		ReadinessPrefetch:             readinessPrefetch,
	})
	if err != nil {
		t.Fatalf("NewDefaultRegistry() error = %v", err)
	}

	def, ok := registry.Definition(DomainDeploymentMapping)
	if !ok {
		t.Fatal("deployment mapping definition missing")
	}
	handler, ok := def.Handler.(PlatformMaterializationHandler)
	if !ok {
		t.Fatalf("deployment mapping handler type = %T, want PlatformMaterializationHandler", def.Handler)
	}
	if handler.CrossRepoResolver == nil {
		t.Fatal("CrossRepoResolver = nil, want non-nil")
	}
	if handler.CrossRepoResolver.ReadinessLookup == nil {
		t.Fatal("ReadinessLookup = nil, want non-nil")
	}
	if handler.CrossRepoResolver.ReadinessPrefetch == nil {
		t.Fatal("ReadinessPrefetch = nil, want non-nil")
	}
}

func TestImplementedDefaultDomainDefinitionsOmitsConfigStateDriftWithoutAdapters(t *testing.T) {
	t.Parallel()

	definitions := implementedDefaultDomainDefinitions(DefaultHandlers{})
	for _, def := range definitions {
		if def.Domain == DomainConfigStateDrift {
			t.Fatalf("config_state_drift registered without adapters; want omitted to avoid silent intent drops")
		}
	}
}

func TestImplementedDefaultDomainDefinitionsIncludesConfigStateDriftWhenAdaptersPresent(t *testing.T) {
	t.Parallel()

	resolver := tfstatebackend.NewResolver(nil)
	loader := stubDriftEvidenceLoader{}
	logger := slog.New(slog.DiscardHandler)
	definitions := implementedDefaultDomainDefinitions(DefaultHandlers{
		TerraformBackendResolver: resolver,
		DriftEvidenceLoader:      loader,
		DriftLogger:              logger,
	})
	found := false
	for _, def := range definitions {
		if def.Domain == DomainConfigStateDrift {
			found = true
			if _, ok := def.Handler.(TerraformConfigStateDriftHandler); !ok {
				t.Fatalf("config_state_drift handler type = %T, want TerraformConfigStateDriftHandler", def.Handler)
			}
		}
	}
	if !found {
		t.Fatal("config_state_drift not registered after wiring resolver+loader+logger")
	}
}

func TestImplementedDefaultDomainDefinitionsOmitsAWSCloudRuntimeDriftWithoutAdapters(t *testing.T) {
	t.Parallel()

	definitions := implementedDefaultDomainDefinitions(DefaultHandlers{})
	for _, def := range definitions {
		if def.Domain == DomainAWSCloudRuntimeDrift {
			t.Fatalf("aws_cloud_runtime_drift registered without adapters; want omitted to avoid silent intent drops")
		}
	}
}

func TestImplementedDefaultDomainDefinitionsIncludesAWSCloudRuntimeDriftWhenAdaptersPresent(t *testing.T) {
	t.Parallel()

	definitions := implementedDefaultDomainDefinitions(DefaultHandlers{
		AWSCloudRuntimeDriftEvidenceLoader: &stubAWSCloudRuntimeDriftEvidenceLoader{},
		AWSCloudRuntimeDriftWriter:         &stubAWSCloudRuntimeDriftFindingWriter{},
	})
	found := false
	for _, def := range definitions {
		if def.Domain == DomainAWSCloudRuntimeDrift {
			found = true
			if _, ok := def.Handler.(AWSCloudRuntimeDriftHandler); !ok {
				t.Fatalf("aws_cloud_runtime_drift handler type = %T, want AWSCloudRuntimeDriftHandler", def.Handler)
			}
		}
	}
	if !found {
		t.Fatal("aws_cloud_runtime_drift not registered after wiring loader+writer")
	}
}

// stubDriftEvidenceLoader is a no-op DriftEvidenceLoader used only to satisfy
// the non-nil gate in implementedDefaultDomainDefinitions.
type stubDriftEvidenceLoader struct{}

// LoadDriftEvidence returns an empty slice; the gating test only checks that
// the loader is wired, not its behavior.
func (stubDriftEvidenceLoader) LoadDriftEvidence(
	_ context.Context,
	_ string,
	_ tfstatebackend.CommitAnchor,
) ([]tfconfigstate.AddressedRow, error) {
	return nil, nil
}

func TestNewDefaultRegistryRegistersDeployableUnitCorrelationAdditively(t *testing.T) {
	t.Parallel()

	handler := HandlerFunc(func(context.Context, Intent) (Result, error) {
		return Result{Status: ResultStatusSucceeded}, nil
	})

	definitions := implementedDefaultDomainDefinitions(DefaultHandlers{
		DeployableUnitCorrelationHandler: handler,
	})
	if got := len(definitions); got != len(DefaultDomainDefinitions())+1 {
		t.Fatalf("len(implementedDefaultDomainDefinitions()) = %d, want %d", got, len(DefaultDomainDefinitions())+1)
	}

	registry, err := NewDefaultRegistry(DefaultHandlers{
		WorkloadIdentityWriter:           &recordingWorkloadIdentityWriter{},
		DeployableUnitCorrelationHandler: handler,
	})
	if err != nil {
		t.Fatalf("NewDefaultRegistry() error = %v, want nil", err)
	}

	def, ok := registry.Definition(DomainDeployableUnitCorrelation)
	if !ok {
		t.Fatal("deployable_unit_correlation definition missing from default registry")
	}
	if def.Domain != DomainDeployableUnitCorrelation {
		t.Fatalf("Definition(DomainDeployableUnitCorrelation).Domain = %q, want %q", def.Domain, DomainDeployableUnitCorrelation)
	}
	if def.Summary == "" {
		t.Fatal("Definition(DomainDeployableUnitCorrelation).Summary = empty, want non-empty")
	}
	if def.Handler == nil {
		t.Fatal("Definition(DomainDeployableUnitCorrelation).Handler = nil, want non-nil")
	}
	if def.TruthContract.CanonicalKind != "deployable_unit_correlation" {
		t.Fatalf("Definition(DomainDeployableUnitCorrelation).TruthContract.CanonicalKind = %q, want %q", def.TruthContract.CanonicalKind, "deployable_unit_correlation")
	}
}

func TestNewDefaultRegistryWiresWorkloadProjectionInputLoader(t *testing.T) {
	t.Parallel()

	inputLoader := &stubWorkloadProjectionInputLoader{}
	platformLookup := &stubInfrastructurePlatformLookup{}

	registry, err := NewDefaultRegistry(DefaultHandlers{
		FactLoader:                    &stubFactLoader{},
		WorkloadMaterializer:          NewWorkloadMaterializer(&recordingCypherExecutor{}),
		WorkloadProjectionInputLoader: inputLoader,
		InfrastructurePlatformLookup:  platformLookup,
	})
	if err != nil {
		t.Fatalf("NewDefaultRegistry() error = %v", err)
	}

	def, ok := registry.Definition(DomainWorkloadMaterialization)
	if !ok {
		t.Fatal("workload materialization definition missing")
	}
	handler, ok := def.Handler.(WorkloadMaterializationHandler)
	if !ok {
		t.Fatalf("handler type = %T, want WorkloadMaterializationHandler", def.Handler)
	}
	if handler.InputLoader != inputLoader {
		t.Fatalf("InputLoader = %T, want %T", handler.InputLoader, inputLoader)
	}
	if handler.InfrastructurePlatformLookup != platformLookup {
		t.Fatalf(
			"InfrastructurePlatformLookup = %T, want %T",
			handler.InfrastructurePlatformLookup,
			platformLookup,
		)
	}
}
