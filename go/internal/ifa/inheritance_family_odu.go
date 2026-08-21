// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/content"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	codegraphv1 "github.com/eshu-hq/eshu/sdk/go/factschema/codegraph/v1"
)

// The inheritance_edges family Odù (#5996, under the #5543 umbrella).
//
// reducer.ExtractInheritanceRows (inheritance_materialization.go) derives
// INHERITS from a Class/Struct/Enum/Trait/Protocol/Interface entity's
// entity_metadata.bases, IMPLEMENTS from a Class/Struct/Enum entity's
// entity_metadata.implemented_interfaces naming an Interface/Protocol entity,
// and OVERRIDES/ALIASES from a Class entity's entity_metadata.trait_adaptations
// PHP "insteadof"/"as" strings -- the ALIASES type has two distinct producing
// paths (a class-to-trait edge from inheritanceTraitAliasTargets, AND a
// method-to-method edge from inheritanceTraitAliasMapping when both the
// aliasing and aliased methods are separately present as Function content
// entities). This fixture exercises every one of those five derivation paths
// so the exact-set expected-edges fixture cannot pass by asserting only a
// coincidentally-matching subset.
//
// InheritanceFamilyOduName, InheritanceFamilyRepoID, and
// InheritanceFamilyCassetteRelPath are exported (#6053/#6199): the
// materialized_edges_inheritance.go guard and its test moved to
// go/internal/ifa/materializededges because they exercise this Odù, and that
// package can only look up this catalog entry, cross-check the fixture's
// repository identity, and locate the committed cassette by reading these
// identifiers from here, not a second copy of them.
const (
	// InheritanceFamilyOduName is this Odù's catalog name.
	InheritanceFamilyOduName      = "odu:ifa-inheritance-family"
	inheritanceFamilyScopeID      = "scope-ifa-inheritance-family"
	inheritanceFamilyGenerationID = "gen-ifa-inheritance-family-1"
	// InheritanceFamilyRepoID is this Odù's repository ID.
	InheritanceFamilyRepoID    = "repository:r_c3a17f92"
	inheritanceFamilyRemoteURL = "https://github.com/eshu-hq/ifa-inheritance-fixture"
	// InheritanceFamilyCassetteRelPath is the repo-root-relative path to this
	// family's committed live-drive cassette.
	InheritanceFamilyCassetteRelPath = "testdata/cassettes/inheritance/ifa-inheritance-family.json"
	inheritanceFamilySourceRunID     = "run-ifa-inheritance-family-1"
	inheritanceFamilyLocalPath       = "/repo-inheritance"

	inheritanceFamilyAnimalPath = "models/animal.py"
	inheritanceFamilyDogPath    = "models/dog.py"
	inheritanceFamilyIfacePath  = "models/runnable.py"
	inheritanceFamilyCatPath    = "models/cat.py"
	inheritanceFamilyLoggerPath = "traits/logger_trait.py"
	inheritanceFamilyDebugPath  = "traits/debug_trait.py"
	inheritanceFamilyWorkerPath = "services/worker.py"
)

// inheritanceFamilyOdu carries nine typed entities across three inheritance
// mechanisms in one repository:
//
//   - Dog (Class, entity_metadata.bases=["Animal"]) derives one INHERITS edge
//     to the Animal Class.
//   - Cat (Class, entity_metadata.implemented_interfaces=["Runnable"]) derives
//     one IMPLEMENTS edge to the Runnable Interface.
//   - Worker (Class, entity_metadata.trait_adaptations=[
//     "LoggerTrait::log insteadof DebugTrait", "LoggerTrait::log as debugLog"])
//     derives an OVERRIDES edge to the DebugTrait Trait from the first
//     adaptation's "insteadof" clause, and from the second adaptation's "as"
//     clause BOTH an ALIASES edge Worker->LoggerTrait (the class-to-trait path,
//     inheritanceTraitAliasTargets) and a second, distinct ALIASES edge from
//     Worker's own "debugLog" Function entity to LoggerTrait's "log" Function
//     entity (the method-to-method path, inheritanceTraitAliasMapping) --
//     proving both ALIASES derivation paths fire off the SAME adaptation
//     string without one masking the other.
//
// Every content_entity carries a matching file fact for its relative_path
// (README.md's #5351 live-proof Gotcha 1: an unmatched relative_path silently
// zeroes the whole batch's graph-node writes), and every entity_id is
// precomputed via content.CanonicalEntityID -- Class, Interface, Trait, and
// Function are all in projector.canonicalNamePathLineEntityLabels
// (go/internal/projector/canonical_entity_identity.go:12), so the projector
// ignores whatever entity_id a content_entity fact supplies for these labels
// and re-derives the same canonical hash from (repo_id, relative_path,
// entity_type, entity_name, start_line) -- Gotcha 2. Using any other value here
// would make the edge writer's endpoint MATCH silently no-op against a live
// backend even though this pure fixture's own row derivation still "worked".
func inheritanceFamilyOdu() CatalogOdu {
	animalID := inheritanceFamilyEntityID(inheritanceFamilyAnimalPath, "Class", "Animal", 1)
	dogID := inheritanceFamilyEntityID(inheritanceFamilyDogPath, "Class", "Dog", 1)
	runnableID := inheritanceFamilyEntityID(inheritanceFamilyIfacePath, "Interface", "Runnable", 1)
	catID := inheritanceFamilyEntityID(inheritanceFamilyCatPath, "Class", "Cat", 1)
	loggerTraitID := inheritanceFamilyEntityID(inheritanceFamilyLoggerPath, "Trait", "LoggerTrait", 1)
	loggerLogMethodID := inheritanceFamilyEntityID(inheritanceFamilyLoggerPath, "Function", "log", 2)
	debugTraitID := inheritanceFamilyEntityID(inheritanceFamilyDebugPath, "Trait", "DebugTrait", 1)
	workerID := inheritanceFamilyEntityID(inheritanceFamilyWorkerPath, "Class", "Worker", 1)
	workerDebugLogID := inheritanceFamilyEntityID(inheritanceFamilyWorkerPath, "Function", "debugLog", 2)

	entities := []struct {
		entityID  string
		name      string
		path      string
		entityTyp string
		startLine int
		endLine   int
		metadata  map[string]any
	}{
		{animalID, "Animal", inheritanceFamilyAnimalPath, "Class", 1, 4, nil},
		{dogID, "Dog", inheritanceFamilyDogPath, "Class", 1, 4, map[string]any{
			"bases": []any{"Animal"},
		}},
		{runnableID, "Runnable", inheritanceFamilyIfacePath, "Interface", 1, 3, nil},
		{catID, "Cat", inheritanceFamilyCatPath, "Class", 1, 4, map[string]any{
			"implemented_interfaces": []any{"Runnable"},
		}},
		{loggerTraitID, "LoggerTrait", inheritanceFamilyLoggerPath, "Trait", 1, 6, nil},
		{loggerLogMethodID, "log", inheritanceFamilyLoggerPath, "Function", 2, 3, map[string]any{
			"class_context": "LoggerTrait",
		}},
		{debugTraitID, "DebugTrait", inheritanceFamilyDebugPath, "Trait", 1, 4, nil},
		{workerID, "Worker", inheritanceFamilyWorkerPath, "Class", 1, 6, map[string]any{
			"trait_adaptations": []any{
				"LoggerTrait::log insteadof DebugTrait",
				"LoggerTrait::log as debugLog",
			},
		}},
		{workerDebugLogID, "debugLog", inheritanceFamilyWorkerPath, "Function", 2, 3, map[string]any{
			"class_context": "Worker",
		}},
	}

	uniquePaths := make([]string, 0, len(entities))
	seenPaths := make(map[string]struct{}, len(entities))
	for _, e := range entities {
		if _, ok := seenPaths[e.path]; ok {
			continue
		}
		seenPaths[e.path] = struct{}{}
		uniquePaths = append(uniquePaths, e.path)
	}

	factsForOdu := make([]facts.Envelope, 0, len(uniquePaths)+len(entities)+2)
	factsForOdu = append(factsForOdu, inheritanceFamilyRepositoryFact())
	for _, path := range uniquePaths {
		factsForOdu = append(factsForOdu, inheritanceFamilyFileFact(path))
	}
	for _, e := range entities {
		factsForOdu = append(factsForOdu, inheritanceFamilyContentEntity(
			e.entityID, e.name, e.path, e.entityTyp, e.startLine, e.endLine, e.metadata,
		))
	}
	factsForOdu = append(factsForOdu, inheritanceFamilyFollowupFact())

	return CatalogOdu{
		Odu: Odu{Name: InheritanceFamilyOduName, Facts: factsForOdu},
		Detail: "one typed repository, seven typed Python files, and nine valid content entities " +
			"deriving one INHERITS, one IMPLEMENTS, one OVERRIDES, and two ALIASES edges " +
			"(class-to-trait and method-to-method) across all five ExtractInheritanceRows derivation paths",
	}
}

// inheritanceFamilyEntityID precomputes the canonical graph uid for one
// content entity the same way the projector derives it for every
// canonicalNamePathLineEntityLabels-member label
// (go/internal/projector/canonical_entity_identity.go): from
// (repo_id, relative_path, entity_type, entity_name, start_line), never from a
// producer-supplied id.
func inheritanceFamilyEntityID(relativePath, entityType, entityName string, startLine int) string {
	return content.CanonicalEntityID(InheritanceFamilyRepoID, relativePath, entityType, entityName, startLine)
}

// inheritanceFamilyContentEntity builds one live content_entity envelope. The
// payload uses "relative_path", the key contentEntityFactEnvelope
// (go/internal/collector/git_content_fact_envelopes.go) emits and the
// inheritance extractor now reads for child_path and inheritanceEntityRef.path.
// Keeping the real collector key here prevents the vacuity guard from passing
// against a fixture-only "path" field that production never emits.
func inheritanceFamilyContentEntity(entityID, name, relativePath, entityType string, startLine, endLine int, metadata map[string]any) facts.Envelope {
	payload := map[string]any{
		"graph_id":      entityID,
		"graph_kind":    "content_entity",
		"repo_id":       InheritanceFamilyRepoID,
		"entity_id":     entityID,
		"entity_type":   entityType,
		"entity_name":   name,
		"relative_path": relativePath,
		"start_line":    float64(startLine),
		"end_line":      float64(endLine),
		"language":      "python",
		"source_cache":  fmt.Sprintf("# %s\n", name),
		"indexed_at":    "1970-01-01T00:00:00Z",
		"iac_relevant":  false,
	}
	if len(metadata) > 0 {
		payload["entity_metadata"] = metadata
	}
	return inheritanceFamilyFact("content_entity", "content_entity:"+entityID, payload)
}

func inheritanceFamilyRepositoryFact() facts.Envelope {
	sourceRunID := inheritanceFamilySourceRunID
	localPath := inheritanceFamilyLocalPath
	graphID, graphKind, repoName, parsedCount := InheritanceFamilyRepoID, "repository", "repo-inheritance", "7"
	repoSlug, remoteURL := "eshu-hq/ifa-inheritance-fixture", inheritanceFamilyRemoteURL
	isDependency := false
	repository := codegraphv1.Repository{
		RepoID: InheritanceFamilyRepoID, SourceRunID: &sourceRunID, LocalPath: &localPath,
		GraphID: &graphID, GraphKind: &graphKind, Name: &repoName,
		ParsedFileCount: &parsedCount, IsDependency: &isDependency,
		RepoSlug: &repoSlug, RemoteURL: &remoteURL,
	}
	payload, err := factschema.EncodeCodegraphRepository(repository)
	if err != nil {
		panic(fmt.Sprintf("ifa: encode inheritance repository %q: %v", repository.RepoID, err))
	}
	return inheritanceFamilyFact(factschema.FactKindCodegraphRepository, "repository:"+repository.RepoID, payload)
}

func inheritanceFamilyFileFact(relativePath string) facts.Envelope {
	fileGraphID, fileGraphKind, language := InheritanceFamilyRepoID+":"+relativePath, "file", "python"
	isDependency := false
	file := codegraphv1.File{
		RepoID: InheritanceFamilyRepoID, RelativePath: relativePath,
		ParsedFileData: inheritanceFamilyParsedFile(relativePath),
		GraphID:        &fileGraphID, GraphKind: &fileGraphKind,
		IsDependency: &isDependency, Language: &language,
	}
	payload, err := factschema.EncodeCodegraphFile(file)
	if err != nil {
		panic(fmt.Sprintf("ifa: encode inheritance file %q: %v", file.RelativePath, err))
	}
	return inheritanceFamilyFact(factschema.FactKindCodegraphFile, "file:"+file.RepoID+":"+file.RelativePath, payload)
}

// inheritanceFamilyParsedFile builds a minimal-but-schema-complete
// parsed_file_data map. ExtractInheritanceRows reads content_entity facts
// directly, not parsed_file_data, so this exists solely to satisfy the File
// payload schema and the Gotcha-1 file/content_entity relative_path match --
// its classes/functions arrays intentionally stay empty.
func inheritanceFamilyParsedFile(relativePath string) map[string]any {
	parsed := map[string]any{
		"path":          inheritanceFamilyLocalPath + "/" + relativePath,
		"repo_path":     inheritanceFamilyLocalPath,
		"lang":          "python",
		"is_dependency": false,
		"iac_relevant":  false,
		"functions":     []any{}, "classes": []any{}, "embedded_shell_commands": []any{}, "function_calls": []any{},
		"imports": []any{}, "modules": []any{}, "orm_table_mappings": []any{},
		"type_annotations": []any{}, "variables": []any{},
		"framework_semantics": map[string]any{"frameworks": []any{}},
	}
	for _, key := range inheritanceFamilyNilParserKeys {
		parsed[key] = nil
	}
	return parsed
}

var inheritanceFamilyNilParserKeys = []string{
	"analytics_models", "annotations", "argocd_applications", "argocd_applicationsets",
	"atlantis_projects", "atlantis_workflows", "cloudformation_conditions",
	"cloudformation_cross_stack_exports", "cloudformation_cross_stack_imports",
	"cloudformation_outputs", "cloudformation_parameters", "cloudformation_resources",
	"components", "crossplane_claims", "crossplane_compositions", "crossplane_xrds",
	"dashboard_assets", "data_assets", "data_columns", "data_contracts", "data_owners",
	"data_quality_checks", "enums", "flux_buckets", "flux_git_repositories",
	"flux_helm_releases", "flux_helm_repositories", "flux_kustomizations",
	"flux_oci_repositories", "gitlab_jobs", "gitlab_pipelines", "helm_charts",
	"helm_template_value_usages", "helm_value_definitions", "helm_values", "impl_blocks",
	"interfaces", "k8s_resources", "kustomize_overlays", "macros", "pagerduty_declarations",
	"properties", "protocol_implementations", "protocols", "query_executions", "records",
	"sql_columns", "sql_functions", "sql_indexes", "sql_migrations", "sql_tables",
	"sql_triggers", "sql_views", "structs", "terraform_backends", "terraform_blocks",
	"terraform_checks", "terraform_data_sources", "terraform_imports", "terraform_locals",
	"terraform_lock_providers", "terraform_modules", "terraform_moved_blocks",
	"terraform_outputs", "terraform_providers", "terraform_removed_blocks",
	"terraform_resources", "terraform_variables", "terragrunt_configs",
	"terragrunt_dependencies", "terragrunt_inputs", "terragrunt_locals", "traits",
	"type_aliases", "typedefs", "unions",
}

// inheritanceFamilyFollowupFact builds the shared_followup envelope that
// enqueues the inheritance_materialization reducer work item. WITHOUT IT THE
// LIVE GATES CANNOT DRIVE THIS HANDLER AT ALL: reducer.MaterializedEdgeFamilies
// has no dedicated projector fan-out probe for inheritance_edges the way
// go/internal/projector/scope_generation_intents.go's ~36 build*ReducerIntent
// functions cover most domains (grep confirms none named for inheritance);
// production enqueues it exclusively via
// inheritanceMaterializationFactEnvelope
// (go/internal/collector/git_followup_facts.go:279-302), whose payload shape
// this mirrors exactly (#5992 measured the codeowners/documentation analog of
// this same omission producing zero reducer intents against a live stack).
func inheritanceFamilyFollowupFact() facts.Envelope {
	return inheritanceFamilyFact("shared_followup", "shared_followup:"+InheritanceFamilyRepoID+":inheritance_materialization", map[string]any{
		"reducer_domain": "inheritance_materialization",
		"entity_key":     "inheritance:" + strings.TrimPrefix(inheritanceFamilyLocalPath, "/"),
		"reason":         "repository snapshot emitted inheritance materialization follow-up",
		"repo_id":        InheritanceFamilyRepoID,
	})
}

func inheritanceFamilyFact(factKind, stableFactKey string, payload map[string]any) facts.Envelope {
	return facts.Envelope{
		ScopeID:          inheritanceFamilyScopeID,
		GenerationID:     inheritanceFamilyGenerationID,
		FactKind:         factKind,
		StableFactKey:    stableFactKey,
		SchemaVersion:    "1.0.0",
		CollectorKind:    "git",
		SourceConfidence: "observed",
		IsTombstone:      false,
		Payload:          payload,
	}
}
