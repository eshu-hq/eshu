// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package shell

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/code/call"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
	"github.com/eshu-hq/eshu/go/internal/reducer/schemadecode"
	"github.com/eshu-hq/eshu/go/internal/reducer/sharedintent"
	"github.com/eshu-hq/eshu/go/internal/reducer/sqlrelationship"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

const evidenceSource = "reducer/shell-exec"

// IntentWriter persists durable shared-projection intents for shell
// execution edge materialization.
type IntentWriter interface {
	UpsertIntents(ctx context.Context, rows []sharedintent.Row) error
}

// Handler reduces parser command-call evidence into
// durable shared-projection intents for Function-[:EXECUTES_SHELL]->ShellCommand.
type Handler struct {
	FactLoader   factload.FactLoader
	IntentWriter IntentWriter
}

// Handle executes shell execution materialization.
func (h Handler) Handle(
	ctx context.Context,
	intent reducercontract.Intent,
) (reducercontract.Result, error) {
	if intent.Domain != reducercontract.DomainShellExecMaterialization {
		return reducercontract.Result{}, fmt.Errorf("shell exec materialization handler does not accept domain %q", intent.Domain)
	}
	if h.FactLoader == nil {
		return reducercontract.Result{}, fmt.Errorf("shell exec materialization fact loader is required")
	}
	if h.IntentWriter == nil {
		return reducercontract.Result{}, fmt.Errorf("shell exec materialization intent writer is required")
	}

	slog.InfoContext(
		ctx, "shell exec materialization started",
		log.ScopeID(intent.ScopeID),
		log.GenerationID(intent.GenerationID),
		log.Domain(string(intent.Domain)),
	)

	envelopes, err := LoadMaterializationFacts(ctx, h.FactLoader, intent.ScopeID, intent.GenerationID)
	if err != nil {
		return reducercontract.Result{}, fmt.Errorf("load facts for shell exec materialization: %w", err)
	}

	// Shell exec reuses the SQL-relationship family's delta scope and repo-ID
	// merge rather than duplicating them: both families derive the same
	// per-repository delta_generation/delta_relative_paths shape from the same
	// "repository" facts (issue #6061).
	deltaScope := sqlrelationship.BuildDeltaScope(envelopes)
	repositoryIDs, edgeRows := ExtractExecRows(envelopes)
	repositoryIDs = sqlrelationship.MergeRepositoryIDs(repositoryIDs, deltaScope.RepositoryIDs)
	contextByRepoID := schemadecode.BuildProjectionContexts(envelopes, intent.GenerationID)
	if len(repositoryIDs) == 0 || len(contextByRepoID) == 0 {
		return reducercontract.Result{
			IntentID:        intent.IntentID,
			Domain:          reducercontract.DomainShellExecMaterialization,
			Status:          reducercontract.ResultStatusSucceeded,
			EvidenceSummary: "no repositories available for shell exec materialization",
		}, nil
	}

	createdAt := intent.EnqueuedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}

	intentRows := BuildSharedIntentRows(edgeRows, deltaScope, repositoryIDs, contextByRepoID, createdAt)
	if len(intentRows) > 0 {
		if err := h.IntentWriter.UpsertIntents(ctx, intentRows); err != nil {
			return reducercontract.Result{}, fmt.Errorf("write shell exec intents: %w", err)
		}
	}

	slog.InfoContext(
		ctx, "shell exec materialization completed",
		log.ScopeID(intent.ScopeID),
		log.GenerationID(intent.GenerationID),
		slog.Int("intent_count", len(intentRows)),
		slog.Int("edge_count", len(edgeRows)),
		slog.Int("repo_count", len(repositoryIDs)),
	)

	return reducercontract.Result{
		IntentID: intent.IntentID,
		Domain:   reducercontract.DomainShellExecMaterialization,
		Status:   reducercontract.ResultStatusSucceeded,
		EvidenceSummary: fmt.Sprintf(
			"emitted %d durable shell exec intents across %d repositories",
			len(intentRows),
			len(repositoryIDs),
		),
		CanonicalWrites: len(intentRows),
	}, nil
}

// materializationFactKinds is the single source for the kind set
// MaterializationFactKinds/LoadMaterializationFacts requests.
var materializationFactKinds = []string{factload.FactKindRepository, factload.FactKindFile}

// MaterializationFactKinds returns the fact kinds shell-exec materialization
// requests, as a fresh copy so a caller cannot mutate the package's backing
// slice. Exported: the reducer root's factload_materialization_bench_test.go
// corpus-coverage guard reads it through the shellExecMaterializationFactKinds
// compat forwarder.
func MaterializationFactKinds() []string {
	out := make([]string, len(materializationFactKinds))
	copy(out, materializationFactKinds)
	return out
}

// LoadMaterializationFacts loads the fact kinds shell-exec materialization
// needs for one scope generation.
func LoadMaterializationFacts(
	ctx context.Context,
	loader factload.FactLoader,
	scopeID string,
	generationID string,
) ([]facts.Envelope, error) {
	return factload.LoadFactsForKinds(ctx, loader, scopeID, generationID, materializationFactKinds)
}

// ExtractExecRows builds canonical shell execution edge rows from file
// parser payloads. It records command-construction presence, never raw command
// text or arguments.
//
// "repository" and "file" fact identity (repo_id, parsed_file_data) is decoded
// through the codegraph contracts seam (schemadecode.DecodeCodegraphRepository,
// schemadecode.DecodeCodegraphFile, Contract System v1 Wave 4f S2, issue #4754)
// rather than raw payloadcore.SemanticPayloadString/PayloadMap lookups. A fact
// whose payload is missing a required identity field is skipped, matching this
// function's pre-existing "skip and continue" shape for an absent/blank
// repo_id or parsed_file_data (the same "skip, do not join under an empty
// identity" contract code_import_repo_edge_identity.go's decode establishes
// for the same fact kinds).
func ExtractExecRows(envelopes []facts.Envelope) ([]string, []map[string]any) {
	if len(envelopes) == 0 {
		return nil, nil
	}
	repoSet := make(map[string]struct{})
	var rows []map[string]any
	seenEdges := make(map[string]struct{})

	for _, env := range envelopes {
		if env.FactKind == factload.FactKindRepository {
			repository, err := schemadecode.DecodeCodegraphRepository(env)
			if err != nil {
				continue
			}
			if repoID := strings.TrimSpace(repository.RepoID); repoID != "" {
				repoSet[repoID] = struct{}{}
			}
			continue
		}
		if env.FactKind != factload.FactKindFile || env.IsTombstone {
			continue
		}
		file, err := schemadecode.DecodeCodegraphFile(env)
		if err != nil {
			continue
		}
		parsedFileData := file.ParsedFileData
		if parsedFileData == nil {
			continue
		}
		repoID := strings.TrimSpace(file.RepoID)
		// "path" is read raw off the top-level envelope first, then falls
		// back to parsed_file_data's own "path" key, preserving the exact
		// pre-Contract-System precedence: "path" is NOT a typed
		// codegraphv1.File field (fileFactEnvelope never writes it to the
		// payload in production — it routes the checkout path to
		// SourceRef.SourceURI, per codegraphv1.Repository's LocalPath
		// precedent in code/call/intents.go), so it is read
		// raw here only to preserve behavior for callers (and fixtures) that
		// carry the source path under the top-level "path" key.
		sourcePath := payloadcore.SemanticPayloadString(env.Payload, "path")
		if sourcePath == "" {
			sourcePath = payloadcore.SemanticPayloadString(parsedFileData, "path")
		}
		if repoID == "" || sourcePath == "" {
			continue
		}
		repoSet[repoID] = struct{}{}
		functionIDs := sqlrelationship.EmbeddedSQLFunctionIDsByNameLine(parsedFileData)
		for _, command := range payloadcore.MapSlice(parsedFileData["embedded_shell_commands"]) {
			functionName := payloadcore.AnyToString(command["function_name"])
			functionLine := call.PayloadInt(command["function_line_number"])
			lineNumber := call.PayloadInt(command["line_number"])
			api := payloadcore.AnyToString(command["api"])
			if functionName == "" || functionLine <= 0 || lineNumber <= 0 || api == "" {
				continue
			}
			functionEntityID := functionIDs[sqlrelationship.EmbeddedSQLFunctionKey(functionName, functionLine)]
			if functionEntityID == "" {
				continue
			}
			targetID := shellCommandTargetID(repoID, sourcePath, functionEntityID, lineNumber, api)
			edgeKey := functionEntityID + "->EXECUTES_SHELL->" + targetID
			if _, seen := seenEdges[edgeKey]; seen {
				continue
			}
			seenEdges[edgeKey] = struct{}{}
			rows = append(rows, map[string]any{
				"source_entity_id":   functionEntityID,
				"target_entity_id":   targetID,
				"source_entity_type": "Function",
				"target_entity_type": "ShellCommand",
				"source_path":        sourcePath,
				"repo_id":            repoID,
				"relationship_type":  "EXECUTES_SHELL",
				"api":                api,
				"language":           payloadcore.AnyToString(command["language"]),
				"line_number":        lineNumber,
			})
		}
	}

	repoIDs := make([]string, 0, len(repoSet))
	for repoID := range repoSet {
		repoIDs = append(repoIDs, repoID)
	}
	sort.Strings(repoIDs)
	sort.Slice(rows, func(i, j int) bool {
		left := payloadcore.AnyToString(rows[i]["repo_id"]) + ":" + payloadcore.AnyToString(rows[i]["source_path"]) + ":" +
			payloadcore.AnyToString(rows[i]["source_entity_id"]) + ":" + payloadcore.AnyToString(rows[i]["target_entity_id"])
		right := payloadcore.AnyToString(rows[j]["repo_id"]) + ":" + payloadcore.AnyToString(rows[j]["source_path"]) + ":" +
			payloadcore.AnyToString(rows[j]["source_entity_id"]) + ":" + payloadcore.AnyToString(rows[j]["target_entity_id"])
		return left < right
	})
	return repoIDs, rows
}

func shellCommandTargetID(repoID, sourcePath, functionEntityID string, lineNumber int, api string) string {
	hash := sha256.New()
	for _, part := range []string{repoID, sourcePath, functionEntityID, payloadcore.AnyToString(lineNumber), strings.TrimSpace(api)} {
		hash.Write([]byte(part))
		hash.Write([]byte{0})
	}
	return "shell-command:" + hex.EncodeToString(hash.Sum(nil))
}
