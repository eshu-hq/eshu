// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"path/filepath"

	"github.com/eshu-hq/eshu/go/internal/facts"
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
const (
	shellExecFamilyOduName         = "odu:ifa-shell-exec-family"
	shellExecFamilyScopeID         = "scope-ifa-shell-exec-family"
	shellExecFamilyGenerationID    = "gen-ifa-shell-exec-family-1"
	shellExecFamilyRepoID          = "repository:r_9f3ce6a1"
	shellExecFamilySourceRunID     = "run-ifa-shell-exec-family-1"
	shellExecFamilyLocalPath       = "/repo-shell-exec"
	shellExecFamilyCassetteRelPath = "testdata/cassettes/shellexec/ifa-shell-exec-family.json"
	shellExecExpectedEdgesRelPath  = "go/internal/ifa/testdata/shellexec/ifa-shell-exec-family-expected-edges.json"
	shellExecFamily                = "shell_exec"

	shellExecFamilyDeployPath  = "services/deploy/deploy.py"
	shellExecFamilyCleanupPath = "services/deploy/cleanup.py"
	shellExecFamilyOrphanPath  = "services/deploy/orphan.py"
	shellExecFamilySilentPath  = "services/deploy/silent.py"

	shellExecFamilyDeployFunctionName  = "deploy_service"
	shellExecFamilyDeployFunctionLine  = 4
	shellExecFamilyCleanupFunctionName = "cleanup_workspace"
	shellExecFamilyCleanupFunctionLine = 3
	shellExecFamilyOrphanFunctionName  = "report_status"
	shellExecFamilyOrphanFunctionLine  = 3
	shellExecFamilySilentFunctionName  = "noop_task"
	shellExecFamilySilentFunctionLine  = 2

	// shellExecFamilyDeployFunctionUID etc. are
	// content.CanonicalEntityID(shellExecFamilyRepoID, <path>, "Function",
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
	shellExecFamilyDeployFunctionUID  = "content-entity:e_8dccb4300a1b"
	shellExecFamilyCleanupFunctionUID = "content-entity:e_79ad678937d5"
	shellExecFamilyOrphanFunctionUID  = "content-entity:e_508a059a3742"
	shellExecFamilySilentFunctionUID  = "content-entity:e_0bde1f6623cb"

	// shellExecFamilyDeployTarget1/2 are the ShellCommand target uids
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
	shellExecFamilyDeployTarget1 = "shell-command:684dbafc339b684757e594dddd2c1b58a5e6613885d9506e94b9cb02258efd1a" // deploy.py line 5, api=os.system
	shellExecFamilyDeployTarget2 = "shell-command:c61db6da0b0b274841061584a6e9fe62f1290fd66a8fe9d2f85216eb52c11b92" // deploy.py line 6, api=subprocess.run
)

// shellExecFamilyCassetteFullPath joins repoRoot onto the live-drive cassette
// path.
func shellExecFamilyCassetteFullPath(repoRoot string) string {
	return filepath.Join(repoRoot, shellExecFamilyCassetteRelPath)
}

// shellExecFamilyExpectedEdgesPath joins repoRoot onto the hand-derived
// expected-edge-set fixture. It lives outside testdata/cassettes/ (like the
// SQL, documentation, and rationale families' fixtures) because it is a gate
// ASSERTION file, and the offline cassette validator globs every
// testdata/cassettes/*/*.json as a replay cassette.
func shellExecFamilyExpectedEdgesPath(repoRoot string) string {
	return filepath.Join(repoRoot, shellExecExpectedEdgesRelPath)
}

// shellExecFamilyOdu carries one repository fact and four file facts wired so
// reducer.ExtractShellExecRows derives exactly two EXECUTES_SHELL edges, both
// from services/deploy/deploy.py's deploy_service function, while the other
// three files pin every exclusion clause to no edges.
func shellExecFamilyOdu() CatalogOdu {
	odu := Odu{
		Name: shellExecFamilyOduName,
		Facts: []facts.Envelope{
			shellExecFamilyRepositoryFact(),
			shellExecFamilyDeployFileFact(),
			shellExecFamilyCleanupFileFact(),
			shellExecFamilyOrphanFileFact(),
			shellExecFamilySilentFileFact(),
			shellExecFamilyFunctionEntity(shellExecFamilyDeployPath, shellExecFamilyDeployFunctionName, shellExecFamilyDeployFunctionUID, shellExecFamilyDeployFunctionLine),
			shellExecFamilyFunctionEntity(shellExecFamilyCleanupPath, shellExecFamilyCleanupFunctionName, shellExecFamilyCleanupFunctionUID, shellExecFamilyCleanupFunctionLine),
			shellExecFamilyFunctionEntity(shellExecFamilyOrphanPath, shellExecFamilyOrphanFunctionName, shellExecFamilyOrphanFunctionUID, shellExecFamilyOrphanFunctionLine),
			shellExecFamilyFunctionEntity(shellExecFamilySilentPath, shellExecFamilySilentFunctionName, shellExecFamilySilentFunctionUID, shellExecFamilySilentFunctionLine),
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

// shellExecFamilyRepositoryFact carries repo_id, source_run_id, and
// local_path -- the same minimal shape sqlFamilyRepositoryFact uses.
// source_run_id is required for buildCodeCallProjectionContexts to yield a
// projection context for this repository at Handle() time; local_path
// qualifies every entity node's path property the same way it does for the
// SQL family (sqlFamilyLocalPath's doc comment, #5549 P1a).
func shellExecFamilyRepositoryFact() facts.Envelope {
	return facts.Envelope{
		ScopeID:      shellExecFamilyScopeID,
		GenerationID: shellExecFamilyGenerationID,
		FactKind:     repositoryFactKind,
		Payload: map[string]any{
			"repo_id":       shellExecFamilyRepoID,
			"source_run_id": shellExecFamilySourceRunID,
			"local_path":    shellExecFamilyLocalPath,
		},
	}
}

// shellExecFamilyFunctionEntity carries the content_entity fact that actually
// materializes a graph Function node at entityUID (the
// parsed_file_data.functions[] entry alone does NOT -- that array is read
// only by this package's embeddedSQLFunctionIDsByNameLine convenience
// lookup, not by the projector's canonical entity writer; see this file's
// shellExecFamilyDeployFunctionUID doc comment). It is inert for the pure
// vacuity guard (ExtractShellExecRows never reads content_entity facts) but
// keeps the fixture live-gate-ready.
func shellExecFamilyFunctionEntity(relativePath, entityName, entityUID string, line int) facts.Envelope {
	return facts.Envelope{
		ScopeID:      shellExecFamilyScopeID,
		GenerationID: shellExecFamilyGenerationID,
		FactKind:     contentEntityFactKind,
		Payload: map[string]any{
			"repo_id":       shellExecFamilyRepoID,
			"entity_id":     entityUID,
			"entity_type":   "Function",
			"entity_name":   entityName,
			"relative_path": relativePath,
			"start_line":    line,
			"end_line":      line + 1,
		},
	}
}

// shellExecFamilyDeployFileFact carries deploy_service, whose three raw
// embedded_shell_commands entries collapse to two EXECUTES_SHELL edges: the
// first two are an EXACT duplicate (same function, same line_number, same
// api) that must dedup to one row, the third is a distinct command on the
// same function.
func shellExecFamilyDeployFileFact() facts.Envelope {
	return shellExecFamilyFileFact(shellExecFamilyDeployPath, []map[string]any{
		shellExecFamilyFunctionEntry(shellExecFamilyDeployFunctionName, shellExecFamilyDeployFunctionUID, shellExecFamilyDeployFunctionLine),
	}, []map[string]any{
		shellExecFamilyCommand(shellExecFamilyDeployFunctionName, shellExecFamilyDeployFunctionLine, 5, "os.system", "python"),
		shellExecFamilyCommand(shellExecFamilyDeployFunctionName, shellExecFamilyDeployFunctionLine, 5, "os.system", "python"), // exact duplicate -> dedup
		shellExecFamilyCommand(shellExecFamilyDeployFunctionName, shellExecFamilyDeployFunctionLine, 6, "subprocess.run", "python"),
	})
}

// shellExecFamilyCleanupFileFact carries cleanup_workspace with two commands
// that each fail exactly one of ExtractShellExecRows's four field checks: a
// non-positive line_number, and a blank api.
func shellExecFamilyCleanupFileFact() facts.Envelope {
	return shellExecFamilyFileFact(shellExecFamilyCleanupPath, []map[string]any{
		shellExecFamilyFunctionEntry(shellExecFamilyCleanupFunctionName, shellExecFamilyCleanupFunctionUID, shellExecFamilyCleanupFunctionLine),
	}, []map[string]any{
		shellExecFamilyCommand(shellExecFamilyCleanupFunctionName, shellExecFamilyCleanupFunctionLine, 0, "os.system", "python"), // line_number <= 0
		shellExecFamilyCommand(shellExecFamilyCleanupFunctionName, shellExecFamilyCleanupFunctionLine, 5, "", "python"),          // blank api
	})
}

// shellExecFamilyOrphanFileFact carries report_status with three commands
// pinning the remaining exclusions: a function_name naming a function this
// file never declares (the functionEntityID lookup-miss branch, reached only
// once the four field checks pass), a blank function_name, and a
// non-positive function_line_number for the function that DOES exist.
func shellExecFamilyOrphanFileFact() facts.Envelope {
	return shellExecFamilyFileFact(shellExecFamilyOrphanPath, []map[string]any{
		shellExecFamilyFunctionEntry(shellExecFamilyOrphanFunctionName, shellExecFamilyOrphanFunctionUID, shellExecFamilyOrphanFunctionLine),
	}, []map[string]any{
		shellExecFamilyCommand("ghost_helper", shellExecFamilyOrphanFunctionLine, 5, "os.system", "python"), // no function named ghost_helper in this file
		shellExecFamilyCommand("", shellExecFamilyOrphanFunctionLine, 6, "os.system", "python"),             // blank function_name
		shellExecFamilyCommand(shellExecFamilyOrphanFunctionName, 0, 7, "os.system", "python"),              // function_line_number <= 0
	})
}

// shellExecFamilySilentFileFact carries noop_task with zero
// embedded_shell_commands: the baseline "function exists, nothing to derive"
// case.
func shellExecFamilySilentFileFact() facts.Envelope {
	return shellExecFamilyFileFact(shellExecFamilySilentPath, []map[string]any{
		shellExecFamilyFunctionEntry(shellExecFamilySilentFunctionName, shellExecFamilySilentFunctionUID, shellExecFamilySilentFunctionLine),
	}, nil)
}

// shellExecFamilyFileFact builds the "file" envelope carrying functions and
// embedded_shell_commands in parsed_file_data, mirroring
// sqlFamilyFileWithEmbeddedQuery's shape: parsed_file_data.path (not the
// top-level payload) is what ExtractShellExecRows's sourcePath fallback
// reads, matching the real fileFactEnvelope emitter.
func shellExecFamilyFileFact(relativePath string, functions, commands []map[string]any) facts.Envelope {
	return facts.Envelope{
		ScopeID:      shellExecFamilyScopeID,
		GenerationID: shellExecFamilyGenerationID,
		FactKind:     fileFactKind,
		Payload: map[string]any{
			"repo_id":       shellExecFamilyRepoID,
			"relative_path": relativePath,
			"parsed_file_data": map[string]any{
				"path":                    shellExecFamilyLocalPath + "/" + relativePath,
				"functions":               functions,
				"embedded_shell_commands": commands,
			},
		},
	}
}

// shellExecFamilyFunctionEntry builds one parsed_file_data.functions[] entry.
// Its "uid" MUST be the same canonical Function uid the content_entity fact
// for this function carries (see shellExecFamilyDeployFunctionUID's doc
// comment) -- embeddedSQLFunctionIDsByNameLine reads this field verbatim as
// the row's source_entity_id.
func shellExecFamilyFunctionEntry(name, uid string, line int) map[string]any {
	return map[string]any{
		"name":        name,
		"uid":         uid,
		"line_number": line,
		"end_line":    line + 1,
	}
}

// shellExecFamilyCommand builds one parsed_file_data.embedded_shell_commands[]
// entry.
func shellExecFamilyCommand(functionName string, functionLine, lineNumber int, api, language string) map[string]any {
	return map[string]any{
		"function_name":        functionName,
		"function_line_number": functionLine,
		"line_number":          lineNumber,
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
	return facts.Envelope{
		ScopeID:      shellExecFamilyScopeID,
		GenerationID: shellExecFamilyGenerationID,
		FactKind:     sharedFollowupFactKind,
		Payload: map[string]any{
			"reducer_domain": "shell_exec_materialization",
			"entity_key":     "shell:" + filepath.Base(shellExecFamilyLocalPath),
			"reason":         "repository snapshot emitted shell execution materialization follow-up",
			"repo_id":        shellExecFamilyRepoID,
		},
	}
}
