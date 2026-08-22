// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	codegraphv1 "github.com/eshu-hq/eshu/sdk/go/factschema/codegraph/v1"
)

// The symbol-runtime family Odù (#5995/#6000/#5997, under the #5543
// umbrella): one repository, one cassette, one shared handler pass driving
// all three HANDLES_ROUTE/RUNS_IN/INVOKES_CLOUD_ACTION domains
// (reducer.ExtractSymbolRuntimeIntentRows). All three domains read the same
// "repository"/"file" facts through the same entry point
// (CodeCallMaterializationHandler.Handle), so one fixture proves all three --
// mirroring the build plan's "ONE cassette drives all three families" design.
//
// Fixture shape (see .trio-notes/family-semantics.md and
// .trio-notes/seam-and-feasibility.md §C5 for the full derivation):
//   - a repository fact carrying BOTH the typed (repo_id, source_run_id)
//     identity buildCodeCallProjectionContexts reads AND the (graph_id, name)
//     pair reducer.ExtractWorkloadCandidates reads -- one fact serves both
//     readers because codegraphv1.Repository's GraphID/Name fields encode to
//     exactly those payload keys;
//   - a Dockerfile file fact carrying a workload signal
//     (dockerfile_stages+language=dockerfile) so ExtractWorkloadCandidates
//     yields a WorkloadCandidate for the repository, at
//     DefaultWorkloadSignalConfidence.ConfidenceFor(SignalDockerfileRuntime)
//     = 0.88, above the 0.82 BuildProjectionRows admission floor;
//   - a server file fact carrying framework_semantics.route_entries (read by
//     BOTH the workload-side APIEndpointRow derivation and the code-side
//     HANDLES_ROUTE/RUNS_IN handler resolution) and function_calls (read by
//     INVOKES_CLOUD_ACTION), plus a functions[] bucket whose uid fields are
//     precomputed content.CanonicalEntityID literals;
//   - a matching content_entity Function fact per function, whose own
//     relative_path/entity_name/start_line must reproduce the SAME canonical
//     id the file fact's functions[].uid carries (README.md's #5351 Gotcha:
//     the projector re-derives the graph uid from the content_entity fact,
//     ignoring the file fact's uid field verbatim, so a mismatch here would
//     make the live MERGE a silent no-op even though this pure Odù's
//     extraction still succeeds);
//   - two shared_followup facts, one per domain family
//     (workload_materialization drives the :Endpoint/:Workload write,
//     code_call_materialization drives HANDLES_ROUTE/RUNS_IN/
//     INVOKES_CLOUD_ACTION) -- without both, a live gate cannot enqueue
//     either handler at all.
const (
	// SymbolRuntimeFamilyOduName is this Odù's catalog name.
	SymbolRuntimeFamilyOduName      = "odu:ifa-symbol-runtime-family"
	symbolRuntimeFamilyScopeID      = "scope-ifa-symbol-runtime-family"
	symbolRuntimeFamilyGenerationID = "gen-ifa-symbol-runtime-family-1"
	// SymbolRuntimeFamilyRepoID is this Odù's repository ID.
	SymbolRuntimeFamilyRepoID      = "repo-ifa-symbol-runtime-family"
	symbolRuntimeFamilyRepoName    = "symbolruntime"
	symbolRuntimeFamilySourceRunID = "run-ifa-symbol-runtime-family-1"
	// SymbolRuntimeFamilyLocalPath is this Odù's repository local_path.
	SymbolRuntimeFamilyLocalPath       = "/repo-ifa-symbol-runtime"
	symbolRuntimeFamilyCassetteRelPath = "testdata/cassettes/symbolruntime/ifa-symbol-runtime-family.json"

	handlesRouteExpectedEdgesRelPath       = "go/internal/ifa/testdata/handlesroute/ifa-handles-route-family-expected-edges.json"
	runsInExpectedEdgesRelPath             = "go/internal/ifa/testdata/runsin/ifa-runs-in-family-expected-edges.json"
	invokesCloudActionExpectedEdgesRelPath = "go/internal/ifa/testdata/invokescloudaction/ifa-invokes-cloud-action-family-expected-edges.json"

	// SymbolRuntimeFamilyDockerfilePath and SymbolRuntimeFamilyServerPath are
	// the two fixture files' relative_path values.
	SymbolRuntimeFamilyDockerfilePath = "Dockerfile"
	SymbolRuntimeFamilyServerPath     = "server.go"

	// SymbolRuntimeFamilyHandlerFunctionName/Line and
	// SymbolRuntimeFamilyCallerFunctionName/Line are the (name, start line)
	// pair for the two Function entities this Odù declares:
	// HandleWidgets resolves the HANDLES_ROUTE/RUNS_IN route entry, InvokeAWS
	// contains the two INVOKES_CLOUD_ACTION call sites (one catalog hit, one
	// non-catalog miss).
	SymbolRuntimeFamilyHandlerFunctionName = "HandleWidgets"
	SymbolRuntimeFamilyHandlerFunctionLine = 10
	SymbolRuntimeFamilyHandlerFunctionEnd  = 20
	SymbolRuntimeFamilyCallerFunctionName  = "InvokeAWS"
	SymbolRuntimeFamilyCallerFunctionLine  = 30
	SymbolRuntimeFamilyCallerFunctionEnd   = 40

	// SymbolRuntimeFamilyHandlerFunctionUID and
	// SymbolRuntimeFamilyCallerFunctionUID are
	// content.CanonicalEntityID(SymbolRuntimeFamilyRepoID,
	// SymbolRuntimeFamilyServerPath, "Function", <name>, <line>): the
	// canonical graph uid the projector's canonicalNamePathLineEntityLabels
	// path re-derives for a "Function"-labeled node, IGNORING whatever
	// entity_id a content_entity fact supplies (README.md's #5351 Gotcha).
	// HANDLES_ROUTE/RUNS_IN/INVOKES_CLOUD_ACTION's source endpoint is this
	// Function node, so both the content_entity fact below AND
	// parsed_file_data.functions[].uid for the same function must carry this
	// SAME precomputed value, or the edge write's source MATCH silently
	// no-ops against a real backend even though the pure extractor still
	// (correctly) derives the row. Pinned as a literal and independently
	// reproduced by TestSymbolRuntimeFamilyCanonicalEntityIDLiterals,
	// mirroring ShellExecFamilyDeployFunctionUID.
	SymbolRuntimeFamilyHandlerFunctionUID = "content-entity:e_b802e874e4a3"
	SymbolRuntimeFamilyCallerFunctionUID  = "content-entity:e_952ea1843457"

	// SymbolRuntimeFamilyRoutePath and SymbolRuntimeFamilyRouteFramework are
	// the single route entry HandleWidgets resolves, read identically by the
	// workload-side APIEndpointRow derivation and the code-side
	// handlesRouteEntries.
	SymbolRuntimeFamilyRoutePath      = "/widgets"
	SymbolRuntimeFamilyRouteMethod    = "GET"
	SymbolRuntimeFamilyRouteFramework = "net_http"

	// SymbolRuntimeFamilyCloudActionService/Method are the one (service,
	// method) call InvokeAWS makes that IS in the closed
	// cloudActionByServiceMethod table; SymbolRuntimeFamilyNonCatalogService/
	// Method is a second call on the SAME function that is NOT in the table
	// and must yield zero INVOKES_CLOUD_ACTION rows.
	SymbolRuntimeFamilyCloudActionService  = "s3"
	SymbolRuntimeFamilyCloudActionMethod   = "PutObject"
	SymbolRuntimeFamilyCloudActionCallLine = 32
	SymbolRuntimeFamilyNonCatalogService   = "widget"
	SymbolRuntimeFamilyNonCatalogMethod    = "Frobnicate"
	SymbolRuntimeFamilyNonCatalogCallLine  = 36
	// SymbolRuntimeFamilyCloudAction is the resolved catalog action for the
	// (s3, PutObject) call.
	SymbolRuntimeFamilyCloudAction = "s3:putobject"

	// SymbolRuntimeFamilyWorkloadID is the deterministic (non-hashed)
	// workload id BuildProjectionRows derives:
	// fmt.Sprintf("workload:%s", workloadName), where workloadName falls
	// back to the repository's Name (WorkloadName is never set here).
	SymbolRuntimeFamilyWorkloadID = "workload:" + symbolRuntimeFamilyRepoName

	// SymbolRuntimeFamilyEndpointID is
	// stableAPIEndpointID(SymbolRuntimeFamilyRepoID,
	// SymbolRuntimeFamilyWorkloadID, SymbolRuntimeFamilyRoutePath):
	// "endpoint:" + hex(sha256(repo_id+"|"+workload_id+"|"+path))[:24 hex
	// chars] (go/internal/reducer/projection_helpers.go). stableAPIEndpointID
	// itself is unexported; this literal is independently reproduced (not
	// hand-typed) by TestSymbolRuntimeFamilyEndpointIDLiteral, which computes
	// the same sha256 over the same three inputs.
	SymbolRuntimeFamilyEndpointID = "endpoint:f8c7b9f9a7ebdd90ea596f67"

	// SymbolRuntimeFamilyActionID is cloudActionIDPrefix + the resolved
	// action: "cloud-action:" + SymbolRuntimeFamilyCloudAction.
	SymbolRuntimeFamilyActionID = "cloud-action:" + SymbolRuntimeFamilyCloudAction
)

// SymbolRuntimeFamilyCassetteFullPath joins repoRoot onto the live-drive
// cassette path.
func SymbolRuntimeFamilyCassetteFullPath(repoRoot string) string {
	return filepath.Join(repoRoot, symbolRuntimeFamilyCassetteRelPath)
}

// HandlesRouteFamilyExpectedEdgesPath joins repoRoot onto the HANDLES_ROUTE
// hand-derived expected-edge-set fixture. It lives outside
// testdata/cassettes/ (like every sibling family's fixture) because it is a
// gate ASSERTION file, and the offline cassette validator globs every
// testdata/cassettes/*/*.json as a replay cassette.
func HandlesRouteFamilyExpectedEdgesPath(repoRoot string) string {
	return filepath.Join(repoRoot, handlesRouteExpectedEdgesRelPath)
}

// RunsInFamilyExpectedEdgesPath joins repoRoot onto the RUNS_IN hand-derived
// expected-edge-set fixture.
func RunsInFamilyExpectedEdgesPath(repoRoot string) string {
	return filepath.Join(repoRoot, runsInExpectedEdgesRelPath)
}

// InvokesCloudActionFamilyExpectedEdgesPath joins repoRoot onto the
// INVOKES_CLOUD_ACTION hand-derived expected-edge-set fixture.
func InvokesCloudActionFamilyExpectedEdgesPath(repoRoot string) string {
	return filepath.Join(repoRoot, invokesCloudActionExpectedEdgesRelPath)
}

// symbolRuntimeFamilyOdu carries one repository, one Dockerfile file (the
// workload signal), one server file (route entries + function calls +
// functions), two content_entity Function facts, and two shared_followup
// facts -- wired so reducer.ExtractSymbolRuntimeIntentRows derives exactly
// one HANDLES_ROUTE row, one RUNS_IN row, and one INVOKES_CLOUD_ACTION row
// (the second, non-catalog function call yields nothing), and
// reducer.ExtractWorkloadCandidates/BuildProjectionRows derive exactly one
// Workload and one Endpoint for the same repository.
func symbolRuntimeFamilyOdu() CatalogOdu {
	sourceRunID := symbolRuntimeFamilySourceRunID
	localPath := SymbolRuntimeFamilyLocalPath
	repoName := symbolRuntimeFamilyRepoName
	repoID := SymbolRuntimeFamilyRepoID
	graphKind := "repository"
	dockerfileLanguage := "dockerfile"
	odu := Odu{
		Name: SymbolRuntimeFamilyOduName,
		Facts: []facts.Envelope{
			symbolRuntimeFamilyRepositoryFact(codegraphv1.Repository{
				RepoID:      SymbolRuntimeFamilyRepoID,
				GraphID:     &repoID,
				GraphKind:   &graphKind,
				Name:        &repoName,
				LocalPath:   &localPath,
				SourceRunID: &sourceRunID,
			}),
			symbolRuntimeFamilyDockerfileFileFact(dockerfileLanguage),
			symbolRuntimeFamilyServerFileFact(),
			symbolRuntimeFamilyFunctionEntity(SymbolRuntimeFamilyHandlerFunctionName, SymbolRuntimeFamilyHandlerFunctionUID, SymbolRuntimeFamilyHandlerFunctionLine),
			symbolRuntimeFamilyFunctionEntity(SymbolRuntimeFamilyCallerFunctionName, SymbolRuntimeFamilyCallerFunctionUID, SymbolRuntimeFamilyCallerFunctionLine),
			symbolRuntimeFamilyFollowupFact("workload_materialization", "workload:"+repoName),
			symbolRuntimeFamilyFollowupFact("code_call_materialization", "code-call:"+repoName),
		},
	}
	return CatalogOdu{
		Odu: odu,
		Detail: "one repository, a Dockerfile file (workload signal) and a server.go file (one net_http route entry serving GET /widgets via HandleWidgets, " +
			"plus two function_calls on InvokeAWS: one (s3, PutObject) catalog hit and one (widget, Frobnicate) non-catalog miss), two content_entity Function facts, " +
			"and two shared_followup facts (workload_materialization, code_call_materialization) -- driving exactly one HANDLES_ROUTE row, one RUNS_IN row, " +
			"one INVOKES_CLOUD_ACTION row, one Workload, and one Endpoint.",
	}
}

// SymbolRuntimeFamilyOdu returns the compiled family fixture used by the
// catalog and by the materializededges package's guard tests.
func SymbolRuntimeFamilyOdu() CatalogOdu {
	return symbolRuntimeFamilyOdu()
}

// symbolRuntimeFamilyRepositoryFact encodes the public repository contract.
// It carries BOTH graph_id/name (ExtractWorkloadCandidates' read) and
// repo_id/source_run_id (buildCodeCallProjectionContexts' read) in the SAME
// fact, matching repoDependencyFamilyRepository's precedent for a family that
// needs both readers satisfied by one repository fact.
func symbolRuntimeFamilyRepositoryFact(repository codegraphv1.Repository) facts.Envelope {
	payload, err := factschema.EncodeCodegraphRepository(repository)
	if err != nil {
		panic(fmt.Sprintf("ifa: encode symbol-runtime catalog repository %q: %v", repository.RepoID, err))
	}
	return symbolRuntimeFamilyEnvelope(
		factschema.FactKindCodegraphRepository,
		"repository:"+repository.RepoID,
		payload,
	)
}

// symbolRuntimeFamilyDockerfileFileFact carries the single workload signal
// (dockerfile_stages, with language "dockerfile" set at the top-level file
// payload, not inside parsed_file_data) that makes
// reducer.ExtractWorkloadCandidates yield exactly one WorkloadCandidate for
// this repository, at DefaultWorkloadSignalConfidence's dockerfile-runtime
// tier (0.88), above BuildProjectionRows' 0.82 admission floor.
func symbolRuntimeFamilyDockerfileFileFact(language string) facts.Envelope {
	return symbolRuntimeFamilyFileFact(codegraphv1.File{
		RepoID:       SymbolRuntimeFamilyRepoID,
		RelativePath: SymbolRuntimeFamilyDockerfilePath,
		Language:     &language,
		ParsedFileData: map[string]any{
			"path": SymbolRuntimeFamilyLocalPath + "/" + SymbolRuntimeFamilyDockerfilePath,
			"dockerfile_stages": []any{
				map[string]any{"name": "runtime"},
			},
		},
	})
}

// symbolRuntimeFamilyServerFileFact carries the functions bucket, the single
// net_http route entry (read identically by the workload-side
// APIEndpointRow derivation and the code-side handlesRouteEntries), and the
// two function_calls entries INVOKES_CLOUD_ACTION reads (one catalog hit,
// one non-catalog miss on the SAME caller function).
func symbolRuntimeFamilyServerFileFact() facts.Envelope {
	return symbolRuntimeFamilyFileFact(codegraphv1.File{
		RepoID:       SymbolRuntimeFamilyRepoID,
		RelativePath: SymbolRuntimeFamilyServerPath,
		ParsedFileData: map[string]any{
			"path": SymbolRuntimeFamilyLocalPath + "/" + SymbolRuntimeFamilyServerPath,
			"functions": []any{
				symbolRuntimeFamilyFunctionEntry(SymbolRuntimeFamilyHandlerFunctionName, SymbolRuntimeFamilyHandlerFunctionUID, SymbolRuntimeFamilyHandlerFunctionLine, SymbolRuntimeFamilyHandlerFunctionEnd),
				symbolRuntimeFamilyFunctionEntry(SymbolRuntimeFamilyCallerFunctionName, SymbolRuntimeFamilyCallerFunctionUID, SymbolRuntimeFamilyCallerFunctionLine, SymbolRuntimeFamilyCallerFunctionEnd),
			},
			"framework_semantics": map[string]any{
				"frameworks": []any{SymbolRuntimeFamilyRouteFramework},
				SymbolRuntimeFamilyRouteFramework: map[string]any{
					"route_entries": []any{
						map[string]any{
							"method":  SymbolRuntimeFamilyRouteMethod,
							"path":    SymbolRuntimeFamilyRoutePath,
							"handler": SymbolRuntimeFamilyHandlerFunctionName,
						},
					},
				},
			},
			"function_calls": []any{
				map[string]any{
					"receiver_sdk_service": SymbolRuntimeFamilyCloudActionService,
					"name":                 SymbolRuntimeFamilyCloudActionMethod,
					"line_number":          SymbolRuntimeFamilyCloudActionCallLine,
				},
				map[string]any{
					"receiver_sdk_service": SymbolRuntimeFamilyNonCatalogService,
					"name":                 SymbolRuntimeFamilyNonCatalogMethod,
					"line_number":          SymbolRuntimeFamilyNonCatalogCallLine,
				},
			},
		},
	})
}

// symbolRuntimeFamilyFunctionEntry builds one parsed_file_data.functions[]
// entry. Its "uid" MUST be the same canonical Function uid the matching
// content_entity fact carries (see SymbolRuntimeFamilyHandlerFunctionUID's
// doc comment).
func symbolRuntimeFamilyFunctionEntry(name, uid string, line, endLine int) map[string]any {
	return map[string]any{
		"name":        name,
		"uid":         uid,
		"line_number": line,
		"end_line":    endLine,
	}
}

// symbolRuntimeFamilyFunctionEntity carries the content_entity fact that
// actually materializes a graph Function node at entityUID (the
// parsed_file_data.functions[] entry alone does NOT -- README.md's #5351
// Gotcha). It is inert for the pure vacuity guard (none of the three domain
// extractors reads content_entity facts) but keeps the fixture live-gate
// ready.
func symbolRuntimeFamilyFunctionEntity(entityName, entityUID string, line int) facts.Envelope {
	return symbolRuntimeFamilyEnvelope(
		contentEntityFactKind,
		"content_entity:"+entityUID,
		map[string]any{
			"repo_id":       SymbolRuntimeFamilyRepoID,
			"entity_id":     entityUID,
			"entity_type":   "Function",
			"entity_name":   entityName,
			"relative_path": SymbolRuntimeFamilyServerPath,
			"start_line":    float64(line),
			"end_line":      float64(line + 1),
		},
	)
}

// symbolRuntimeFamilyFileFact encodes the public file contract before
// wrapping it in the fixture envelope.
func symbolRuntimeFamilyFileFact(file codegraphv1.File) facts.Envelope {
	payload, err := factschema.EncodeCodegraphFile(file)
	if err != nil {
		panic(fmt.Sprintf("ifa: encode symbol-runtime catalog file %q: %v", file.RelativePath, err))
	}
	return symbolRuntimeFamilyEnvelope(
		factschema.FactKindCodegraphFile,
		"file:"+file.RepoID+":"+file.RelativePath,
		payload,
	)
}

// symbolRuntimeFamilyFollowupFact is the production trigger fact shape
// (go/internal/collector/git_followup_facts.go): the shared_followup fact
// whose reducer_domain payload key is what go/internal/projector's
// buildReducerIntent turns into a durable fact_work_items row. Neither
// reducer.ExtractSymbolRuntimeIntentRows nor
// reducer.ExtractWorkloadCandidates reads this fact kind, so it is inert for
// the pure vacuity guards but included for fidelity with the live-drive
// cassette and the real collector's own emission shape. TWO of these are
// required (one per domain family): without the workload_materialization
// followup the live handler never commits the :Endpoint/:Workload nodes
// HANDLES_ROUTE/RUNS_IN depend on; without code_call_materialization the
// handler that builds all three domains' intents never runs at all.
func symbolRuntimeFamilyFollowupFact(domain, entityKey string) facts.Envelope {
	return symbolRuntimeFamilyEnvelope(
		sharedFollowupFactKind,
		"shared_followup:"+SymbolRuntimeFamilyRepoID+":"+domain,
		map[string]any{
			"reducer_domain": domain,
			"entity_key":     entityKey,
			"reason":         "repository snapshot emitted " + domain + " follow-up",
			"repo_id":        SymbolRuntimeFamilyRepoID,
		},
	)
}

// symbolRuntimeFamilyEnvelope adds the producer-owned identity and schema
// fields carried by the replay cassette to every compiled fixture fact.
func symbolRuntimeFamilyEnvelope(factKind, stableFactKey string, payload map[string]any) facts.Envelope {
	return facts.Envelope{
		ScopeID:          symbolRuntimeFamilyScopeID,
		GenerationID:     symbolRuntimeFamilyGenerationID,
		FactKind:         factKind,
		StableFactKey:    stableFactKey,
		SchemaVersion:    "1.0.0",
		CollectorKind:    "git",
		SourceConfidence: "observed",
		Payload:          payload,
	}
}

// symbolRuntimeFamilyCassetteFile mirrors the cassette envelope fields that
// decide whether a fact is accepted at all, matching
// deployableUnitFamilyCassetteFile's projection (#5991 precedent).
type symbolRuntimeFamilyCassetteFile struct {
	Scopes []struct {
		ScopeID      string `json:"scope_id"`
		GenerationID string `json:"generation_id"`
		Facts        []struct {
			FactKind         string         `json:"fact_kind"`
			SchemaVersion    string         `json:"schema_version"`
			StableFactKey    string         `json:"stable_fact_key"`
			CollectorKind    string         `json:"collector_kind"`
			SourceConfidence string         `json:"source_confidence"`
			IsTombstone      bool           `json:"is_tombstone"`
			Payload          map[string]any `json:"payload"`
		} `json:"facts"`
	} `json:"scopes"`
}

// LoadSymbolRuntimeFamilyOdu reads the committed cassette and projects it onto
// the fact envelopes the reducer's extraction seams consume. It is the
// test-side lockstep loader for the committed cassette: a lockstep test in
// materializededges compares this strict cassette projection with the
// compiled symbolRuntimeFamilyOdu() so a one-sided edit fails the focused
// suite.
//
// It fails closed on an empty scope or fact list: an Odù carrying no facts
// would make every downstream assertion vacuous.
func LoadSymbolRuntimeFamilyOdu(cassettePath string) (Odu, error) {
	raw, err := os.ReadFile(cassettePath) // #nosec G304 -- checked-in repo fixture under testdata/, not external input
	if err != nil {
		return Odu{}, fmt.Errorf("ifa: read symbol-runtime cassette %s: %w", cassettePath, err)
	}
	var parsed symbolRuntimeFamilyCassetteFile
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return Odu{}, fmt.Errorf("ifa: parse symbol-runtime cassette %s: %w", cassettePath, err)
	}
	if len(parsed.Scopes) != 1 {
		return Odu{}, fmt.Errorf("ifa: symbol-runtime cassette %s declares %d scopes, want exactly 1; a multi-scope fixture would make the expected-edge set ambiguous about which scope produced an edge", cassettePath, len(parsed.Scopes))
	}
	scope := parsed.Scopes[0]
	if len(scope.Facts) == 0 {
		return Odu{}, fmt.Errorf("ifa: symbol-runtime cassette %s carries no facts; an empty Odù makes every assertion vacuous", cassettePath)
	}

	envelopes := make([]facts.Envelope, 0, len(scope.Facts))
	for _, fact := range scope.Facts {
		envelopes = append(envelopes, facts.Envelope{
			ScopeID:          scope.ScopeID,
			GenerationID:     scope.GenerationID,
			FactKind:         fact.FactKind,
			SchemaVersion:    fact.SchemaVersion,
			StableFactKey:    fact.StableFactKey,
			CollectorKind:    fact.CollectorKind,
			SourceConfidence: fact.SourceConfidence,
			IsTombstone:      fact.IsTombstone,
			Payload:          fact.Payload,
		})
	}
	return Odu{Name: SymbolRuntimeFamilyOduName, Facts: envelopes}, nil
}
