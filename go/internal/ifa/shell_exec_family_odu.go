// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"fmt"
	"path/filepath"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	codegraphv1 "github.com/eshu-hq/eshu/sdk/go/factschema/codegraph/v1"
)

// The shell_exec family Odù (#6001, under the #5543 umbrella).
//
// reducer.ExtractShellExecRows (go/internal/reducer/shell_exec_materialization.go)
// projects one Function-[:EXECUTES_SHELL]->ShellCommand edge per distinct
// (function, line_number, api) triple carried in a file's parser-emitted
// parsed_file_data.embedded_shell_commands. The fixture below exercises every
// clause that decides whether an edge exists: a valid pair of distinct
// commands off the same function (plus an exact duplicate, proving dedup), a
// function with commands that fail each of the extractor's four field checks
// in isolation (blank function_name, non-positive function_line_number,
// non-positive line_number, blank api), a command naming a function that does
// not exist in the file (the functionEntityID lookup-miss branch, distinct
// from the four field checks), and a function with no embedded_shell_commands
// at all. Synthetic malformed-envelope guards live in reducer unit tests
// rather than this live Odù, matching the rationale and SQL families'
// convention.
//
// ShellExecFamilyOduName, ShellExecFamilyRepoID, ShellExecFamilyLocalPath,
// the four ShellExecFamily*Path file-path constants, the eight
// function-name/line constants, the four *FunctionUID canonical-entity-id
// literals, the two *Target ShellCommand uid literals,
// ShellExecFamilyCassetteFullPath, and ShellExecFamilyExpectedEdgesPath are
// exported (#6053/#6199): the
// materialized_edges_shell_exec.go guard and its tests moved to
// go/internal/ifa/materializededges because they exercise this Odù and
// independently reproduce its canonical-id/target-hash literals against the
// real content.CanonicalEntityID and shellExecTargetID algorithms, and that
// package can only do so by reading these identifiers from here, not a
// second copy of them -- a stale copy of a reference-side identity like this
// would fail open (compare equal to itself, never to production truth).
const (
	// ShellExecFamilyOduName is this Odù's catalog name.
	ShellExecFamilyOduName      = "odu:ifa-shell-exec-family"
	shellExecFamilyScopeID      = "scope-ifa-shell-exec-family"
	shellExecFamilyGenerationID = "gen-ifa-shell-exec-family-1"
	// ShellExecFamilyRepoID is this Odù's repository ID.
	ShellExecFamilyRepoID      = "repository:r_9f3ce6a1"
	shellExecFamilySourceRunID = "run-ifa-shell-exec-family-1"
	// ShellExecFamilyLocalPath is this Odù's repository local_path.
	ShellExecFamilyLocalPath       = "/repo-shell-exec"
	shellExecFamilyCassetteRelPath = "testdata/cassettes/shellexec/ifa-shell-exec-family.json"
	shellExecExpectedEdgesRelPath  = "go/internal/ifa/testdata/shellexec/ifa-shell-exec-family-expected-edges.json"

	// ShellExecFamilyDeployPath, ShellExecFamilyCleanupPath,
	// ShellExecFamilyOrphanPath, and ShellExecFamilySilentPath are the four
	// fixture files' relative_path values.
	ShellExecFamilyDeployPath  = "services/deploy/deploy.py"
	ShellExecFamilyCleanupPath = "services/deploy/cleanup.py"
	ShellExecFamilyOrphanPath  = "services/deploy/orphan.py"
	ShellExecFamilySilentPath  = "services/deploy/silent.py"

	// ShellExecFamilyDeployFunctionName/Line, ShellExecFamilyCleanupFunctionName/Line,
	// ShellExecFamilyOrphanFunctionName/Line, and
	// ShellExecFamilySilentFunctionName/Line are the (name, start line) pair
	// for the one Function entity each fixture file declares.
	ShellExecFamilyDeployFunctionName  = "deploy_service"
	ShellExecFamilyDeployFunctionLine  = 4
	ShellExecFamilyCleanupFunctionName = "cleanup_workspace"
	ShellExecFamilyCleanupFunctionLine = 3
	ShellExecFamilyOrphanFunctionName  = "report_status"
	ShellExecFamilyOrphanFunctionLine  = 3
	ShellExecFamilySilentFunctionName  = "noop_task"
	ShellExecFamilySilentFunctionLine  = 2

	// ShellExecFamilyDeployFunctionUID etc. are
	// content.CanonicalEntityID(ShellExecFamilyRepoID, <path>, "Function",
	// <name>, <line>) (go/internal/content/writer.go): the canonical graph uid
	// projector.canonicalGraphEntityID's canonicalNamePathLineEntityLabels set
	// derives for a "Function"-labeled node, IGNORING whatever entity_id a
	// content_entity fact supplies (README.md's #5351 live-proof Gotcha).
	// EXECUTES_SHELL's source endpoint is this Function node, so both the
	// content_entity fact below AND parsed_file_data.functions[].uid for the
	// same function must carry this SAME precomputed value, or the edge
	// write's source MATCH silently no-ops against a real backend even though
	// ExtractShellExecRows still (correctly) derives the row. Pinned as a
	// literal and independently reproduced by
	// TestShellExecCanonicalEntityIDLiterals
	// (materialized_edges_shell_exec_test.go), mirroring
	// TestRationaleCanonicalTargetIDLiterals.
	ShellExecFamilyDeployFunctionUID  = "content-entity:e_8dccb4300a1b"
	ShellExecFamilyCleanupFunctionUID = "content-entity:e_79ad678937d5"
	ShellExecFamilyOrphanFunctionUID  = "content-entity:e_508a059a3742"
	ShellExecFamilySilentFunctionUID  = "content-entity:e_0bde1f6623cb"

	// ShellExecFamilyDeployTarget1/2 are the ShellCommand target uids
	// edge_writer_shell_exec.go's buildShellExecRowMap/shellExecTargetID
	// derives: sha256(repo_id, source_path, function_entity_id, line_number,
	// api), each field NUL-terminated, hex-encoded and prefixed
	// "shell-command:". source_path here is parsed_file_data.path (the full
	// local-path-prefixed path), NOT the file fact's relative_path:
	// ExtractShellExecRows reads the top-level envelope "path" first and
	// falls back to parsed_file_data["path"] only, and this fixture's file
	// facts carry no top-level "path" key, so the fallback always fires.
	// Independently reproduced (not hand-typed) by
	// TestShellExecTargetIDLiteralsMatchTheWriterFunction, which holds a copy
	// of the same hashing algorithm read directly off
	// go/internal/reducer/shell_exec_materialization.go's shellExecTargetID.
	ShellExecFamilyDeployTarget1 = "shell-command:684dbafc339b684757e594dddd2c1b58a5e6613885d9506e94b9cb02258efd1a" // deploy.py line 5, api=os.system
	ShellExecFamilyDeployTarget2 = "shell-command:c61db6da0b0b274841061584a6e9fe62f1290fd66a8fe9d2f85216eb52c11b92" // deploy.py line 6, api=subprocess.run
)

// ShellExecFamilyCassetteFullPath joins repoRoot onto the live-drive cassette
// path.
func ShellExecFamilyCassetteFullPath(repoRoot string) string {
	return filepath.Join(repoRoot, shellExecFamilyCassetteRelPath)
}

// ShellExecFamilyExpectedEdgesPath joins repoRoot onto the hand-derived
// expected-edge-set fixture. It lives outside testdata/cassettes/ (like the
// SQL, documentation, and rationale families' fixtures) because it is a gate
// ASSERTION file, and the offline cassette validator globs every
// testdata/cassettes/*/*.json as a replay cassette.
func ShellExecFamilyExpectedEdgesPath(repoRoot string) string {
	return filepath.Join(repoRoot, shellExecExpectedEdgesRelPath)
}

// shellExecFamilyOdu carries one repository fact and four file facts wired so
// reducer.ExtractShellExecRows derives exactly two EXECUTES_SHELL edges, both
// from services/deploy/deploy.py's deploy_service function, while the other
// three files pin every exclusion clause to no edges.
func shellExecFamilyOdu() CatalogOdu {
	sourceRunID := shellExecFamilySourceRunID
	localPath := ShellExecFamilyLocalPath
	odu := Odu{
		Name: ShellExecFamilyOduName,
		Facts: []facts.Envelope{
			shellExecFamilyRepositoryFact(codegraphv1.Repository{
				RepoID:      ShellExecFamilyRepoID,
				SourceRunID: &sourceRunID,
				LocalPath:   &localPath,
			}),
			shellExecFamilyDeployFileFact(),
			shellExecFamilyCleanupFileFact(),
			shellExecFamilyOrphanFileFact(),
			shellExecFamilySilentFileFact(),
			shellExecFamilyFunctionEntity(ShellExecFamilyDeployPath, ShellExecFamilyDeployFunctionName, ShellExecFamilyDeployFunctionUID, ShellExecFamilyDeployFunctionLine),
			shellExecFamilyFunctionEntity(ShellExecFamilyCleanupPath, ShellExecFamilyCleanupFunctionName, ShellExecFamilyCleanupFunctionUID, ShellExecFamilyCleanupFunctionLine),
			shellExecFamilyFunctionEntity(ShellExecFamilyOrphanPath, ShellExecFamilyOrphanFunctionName, ShellExecFamilyOrphanFunctionUID, ShellExecFamilyOrphanFunctionLine),
			shellExecFamilyFunctionEntity(ShellExecFamilySilentPath, ShellExecFamilySilentFunctionName, ShellExecFamilySilentFunctionUID, ShellExecFamilySilentFunctionLine),
			shellExecFamilyFollowupFact(),
		},
	}
	return CatalogOdu{
		Odu: odu,
		Detail: "one repository, four Python files: deploy.py derives exactly two EXECUTES_SHELL edges off one function (plus an exact-duplicate command proving dedup); " +
			"cleanup.py pins the non-positive-line_number and blank-api exclusions; orphan.py pins the unmatched-function, blank-function_name, and non-positive-function_line_number " +
			"exclusions; silent.py pins the no-commands-at-all baseline",
	}
}

// shellExecFamilyRepositoryFact encodes the public repository contract.
// source_run_id is required for buildCodeCallProjectionContexts to yield a
// projection context for this repository at Handle() time; local_path
// qualifies every entity node's path property the same way it does for the
// SQL family (sqlFamilyLocalPath's doc comment, #5549 P1a).
func shellExecFamilyRepositoryFact(repository codegraphv1.Repository) facts.Envelope {
	payload, err := factschema.EncodeCodegraphRepository(repository)
	if err != nil {
		panic(fmt.Sprintf("ifa: encode shell-exec catalog repository %q: %v", repository.RepoID, err))
	}
	return shellExecFamilyEnvelope(
		factschema.FactKindCodegraphRepository,
		"repository:"+repository.RepoID,
		payload,
	)
}

// shellExecFamilyFunctionEntity carries the content_entity fact that actually
// materializes a graph Function node at entityUID (the
// parsed_file_data.functions[] entry alone does NOT -- that array is read
// only by sqlrelationship.EmbeddedSQLFunctionIDsByNameLine, the convenience
// lookup, not by the projector's canonical entity writer; see this file's
// ShellExecFamilyDeployFunctionUID doc comment). It is inert for the pure
// vacuity guard (ExtractShellExecRows never reads content_entity facts) but
// keeps the fixture live-gate-ready.
func shellExecFamilyFunctionEntity(relativePath, entityName, entityUID string, line int) facts.Envelope {
	return shellExecFamilyEnvelope(
		contentEntityFactKind,
		"content_entity:"+entityUID,
		map[string]any{
			"repo_id":       ShellExecFamilyRepoID,
			"entity_id":     entityUID,
			"entity_type":   "Function",
			"entity_name":   entityName,
			"relative_path": relativePath,
			"start_line":    float64(line),
			"end_line":      float64(line + 1),
		},
	)
}

// shellExecFamilyDeployFileFact carries deploy_service, whose three raw
// embedded_shell_commands entries collapse to two EXECUTES_SHELL edges: the
// first two are an EXACT duplicate (same function, same line_number, same
// api) that must dedup to one row, the third is a distinct command on the
// same function.
func shellExecFamilyDeployFileFact() facts.Envelope {
	return shellExecFamilyFileFact(codegraphv1.File{
		RepoID:       ShellExecFamilyRepoID,
		RelativePath: ShellExecFamilyDeployPath,
		ParsedFileData: shellExecFamilyParsedFile(ShellExecFamilyDeployPath, []any{
			shellExecFamilyFunctionEntry(ShellExecFamilyDeployFunctionName, ShellExecFamilyDeployFunctionUID, ShellExecFamilyDeployFunctionLine),
		}, []any{
			shellExecFamilyCommand(ShellExecFamilyDeployFunctionName, ShellExecFamilyDeployFunctionLine, 5, "os.system", "python"),
			shellExecFamilyCommand(ShellExecFamilyDeployFunctionName, ShellExecFamilyDeployFunctionLine, 5, "os.system", "python"), // exact duplicate -> dedup
			shellExecFamilyCommand(ShellExecFamilyDeployFunctionName, ShellExecFamilyDeployFunctionLine, 6, "subprocess.run", "python"),
		}),
	})
}

// shellExecFamilyCleanupFileFact carries cleanup_workspace with two commands
// that each fail exactly one of ExtractShellExecRows's four field checks: a
// non-positive line_number, and a blank api.
func shellExecFamilyCleanupFileFact() facts.Envelope {
	return shellExecFamilyFileFact(codegraphv1.File{
		RepoID:       ShellExecFamilyRepoID,
		RelativePath: ShellExecFamilyCleanupPath,
		ParsedFileData: shellExecFamilyParsedFile(ShellExecFamilyCleanupPath, []any{
			shellExecFamilyFunctionEntry(ShellExecFamilyCleanupFunctionName, ShellExecFamilyCleanupFunctionUID, ShellExecFamilyCleanupFunctionLine),
		}, []any{
			shellExecFamilyCommand(ShellExecFamilyCleanupFunctionName, ShellExecFamilyCleanupFunctionLine, 0, "os.system", "python"), // line_number <= 0
			shellExecFamilyCommand(ShellExecFamilyCleanupFunctionName, ShellExecFamilyCleanupFunctionLine, 5, "", "python"),          // blank api
		}),
	})
}

// shellExecFamilyOrphanFileFact carries report_status with three commands
// pinning the remaining exclusions: a function_name naming a function this
// file never declares (the functionEntityID lookup-miss branch, reached only
// once the four field checks pass), a blank function_name, and a
// non-positive function_line_number for the function that DOES exist.
func shellExecFamilyOrphanFileFact() facts.Envelope {
	return shellExecFamilyFileFact(codegraphv1.File{
		RepoID:       ShellExecFamilyRepoID,
		RelativePath: ShellExecFamilyOrphanPath,
		ParsedFileData: shellExecFamilyParsedFile(ShellExecFamilyOrphanPath, []any{
			shellExecFamilyFunctionEntry(ShellExecFamilyOrphanFunctionName, ShellExecFamilyOrphanFunctionUID, ShellExecFamilyOrphanFunctionLine),
		}, []any{
			shellExecFamilyCommand("ghost_helper", ShellExecFamilyOrphanFunctionLine, 5, "os.system", "python"), // no function named ghost_helper in this file
			shellExecFamilyCommand("", ShellExecFamilyOrphanFunctionLine, 6, "os.system", "python"),             // blank function_name
			shellExecFamilyCommand(ShellExecFamilyOrphanFunctionName, 0, 7, "os.system", "python"),              // function_line_number <= 0
		}),
	})
}

// shellExecFamilySilentFileFact carries noop_task with zero
// embedded_shell_commands: the baseline "function exists, nothing to derive"
// case.
func shellExecFamilySilentFileFact() facts.Envelope {
	return shellExecFamilyFileFact(codegraphv1.File{
		RepoID:       ShellExecFamilyRepoID,
		RelativePath: ShellExecFamilySilentPath,
		ParsedFileData: shellExecFamilyParsedFile(ShellExecFamilySilentPath, []any{
			shellExecFamilyFunctionEntry(ShellExecFamilySilentFunctionName, ShellExecFamilySilentFunctionUID, ShellExecFamilySilentFunctionLine),
		}, []any{}),
	})
}

// shellExecFamilyFileFact encodes the public file contract before wrapping it
// in the fixture envelope.
func shellExecFamilyFileFact(file codegraphv1.File) facts.Envelope {
	payload, err := factschema.EncodeCodegraphFile(file)
	if err != nil {
		panic(fmt.Sprintf("ifa: encode shell-exec catalog file %q: %v", file.RelativePath, err))
	}
	return shellExecFamilyEnvelope(
		factschema.FactKindCodegraphFile,
		"file:"+file.RepoID+":"+file.RelativePath,
		payload,
	)
}

// shellExecFamilyParsedFile builds the parser-owned data carried by a typed
// codegraph file. parsed_file_data.path (not the top-level payload) is what
// ExtractShellExecRows's sourcePath fallback reads, matching the real file
// fact emitter.
func shellExecFamilyParsedFile(relativePath string, functions, commands []any) map[string]any {
	return map[string]any{
		"path":                    ShellExecFamilyLocalPath + "/" + relativePath,
		"functions":               functions,
		"embedded_shell_commands": commands,
	}
}

// shellExecFamilyFunctionEntry builds one parsed_file_data.functions[] entry.
// Its "uid" MUST be the same canonical Function uid the content_entity fact
// for this function carries (see ShellExecFamilyDeployFunctionUID's doc
// comment) -- sqlrelationship.EmbeddedSQLFunctionIDsByNameLine reads this field verbatim as
// the row's source_entity_id.
func shellExecFamilyFunctionEntry(name, uid string, line int) map[string]any {
	return map[string]any{
		"name":        name,
		"uid":         uid,
		"line_number": float64(line),
		"end_line":    float64(line + 1),
	}
}

// shellExecFamilyCommand builds one parsed_file_data.embedded_shell_commands[]
// entry.
func shellExecFamilyCommand(functionName string, functionLine, lineNumber int, api, language string) map[string]any {
	return map[string]any{
		"function_name":        functionName,
		"function_line_number": float64(functionLine),
		"line_number":          float64(lineNumber),
		"api":                  api,
		"language":             language,
	}
}

// shellExecFamilyFollowupFact is the production trigger fact
// (go/internal/collector/git_followup_facts.go's
// shellExecMaterializationFactEnvelope): the shared_followup fact whose
// reducer_domain: shell_exec_materialization payload key is what
// go/internal/projector's buildReducerIntent turns into a durable
// fact_work_items row under the real pipeline. ExtractShellExecRows itself
// ignores this fact kind (loadShellExecMaterializationFacts only loads
// repository/file), so it is inert for the pure vacuity guard but included
// for fidelity with the live-drive cassette and the real collector's own
// emission shape.
func shellExecFamilyFollowupFact() facts.Envelope {
	return shellExecFamilyEnvelope(
		sharedFollowupFactKind,
		"shared_followup:"+ShellExecFamilyRepoID+":shell_exec_materialization",
		map[string]any{
			"reducer_domain": "shell_exec_materialization",
			"entity_key":     "shell:" + filepath.Base(ShellExecFamilyLocalPath),
			"reason":         "repository snapshot emitted shell execution materialization follow-up",
			"repo_id":        ShellExecFamilyRepoID,
		},
	)
}

// shellExecFamilyEnvelope adds the producer-owned identity and schema fields
// carried by the replay cassette to every compiled fixture fact.
func shellExecFamilyEnvelope(factKind, stableFactKey string, payload map[string]any) facts.Envelope {
	return facts.Envelope{
		ScopeID:          shellExecFamilyScopeID,
		GenerationID:     shellExecFamilyGenerationID,
		FactKind:         factKind,
		StableFactKey:    stableFactKey,
		SchemaVersion:    "1.0.0",
		CollectorKind:    "git",
		SourceConfidence: "observed",
		Payload:          payload,
	}
}
