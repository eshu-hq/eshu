// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

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
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

const shellExecEvidenceSource = "reducer/shell-exec"

// ShellExecIntentWriter persists durable shared-projection intents for shell
// execution edge materialization.
type ShellExecIntentWriter interface {
	UpsertIntents(ctx context.Context, rows []SharedProjectionIntentRow) error
}

// ShellExecMaterializationHandler reduces parser command-call evidence into
// durable shared-projection intents for Function-[:EXECUTES_SHELL]->ShellCommand.
type ShellExecMaterializationHandler struct {
	FactLoader   FactLoader
	IntentWriter ShellExecIntentWriter
}

// Handle executes shell execution materialization.
func (h ShellExecMaterializationHandler) Handle(
	ctx context.Context,
	intent Intent,
) (Result, error) {
	if intent.Domain != DomainShellExecMaterialization {
		return Result{}, fmt.Errorf("shell exec materialization handler does not accept domain %q", intent.Domain)
	}
	if h.FactLoader == nil {
		return Result{}, fmt.Errorf("shell exec materialization fact loader is required")
	}
	if h.IntentWriter == nil {
		return Result{}, fmt.Errorf("shell exec materialization intent writer is required")
	}

	slog.InfoContext(
		ctx, "shell exec materialization started",
		log.ScopeID(intent.ScopeID),
		log.GenerationID(intent.GenerationID),
		log.Domain(string(intent.Domain)),
	)

	envelopes, err := loadShellExecMaterializationFacts(ctx, h.FactLoader, intent.ScopeID, intent.GenerationID)
	if err != nil {
		return Result{}, fmt.Errorf("load facts for shell exec materialization: %w", err)
	}

	deltaScope := buildSQLRelationshipDeltaScope(envelopes)
	repositoryIDs, edgeRows := ExtractShellExecRows(envelopes)
	repositoryIDs = mergeSQLRelationshipRepositoryIDs(repositoryIDs, deltaScope.repositoryIDs)
	contextByRepoID := buildCodeCallProjectionContexts(envelopes, intent.GenerationID)
	if len(repositoryIDs) == 0 || len(contextByRepoID) == 0 {
		return Result{
			IntentID:        intent.IntentID,
			Domain:          DomainShellExecMaterialization,
			Status:          ResultStatusSucceeded,
			EvidenceSummary: "no repositories available for shell exec materialization",
		}, nil
	}

	createdAt := intent.EnqueuedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}

	intentRows := buildShellExecSharedIntentRows(edgeRows, deltaScope, repositoryIDs, contextByRepoID, createdAt)
	if len(intentRows) > 0 {
		if err := h.IntentWriter.UpsertIntents(ctx, intentRows); err != nil {
			return Result{}, fmt.Errorf("write shell exec intents: %w", err)
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

	return Result{
		IntentID: intent.IntentID,
		Domain:   DomainShellExecMaterialization,
		Status:   ResultStatusSucceeded,
		EvidenceSummary: fmt.Sprintf(
			"emitted %d durable shell exec intents across %d repositories",
			len(intentRows),
			len(repositoryIDs),
		),
		CanonicalWrites: len(intentRows),
	}, nil
}

func loadShellExecMaterializationFacts(
	ctx context.Context,
	loader FactLoader,
	scopeID string,
	generationID string,
) ([]facts.Envelope, error) {
	return loadFactsForKinds(ctx, loader, scopeID, generationID, []string{factKindRepository, factKindFile})
}

// ExtractShellExecRows builds canonical shell execution edge rows from file
// parser payloads. It records command-construction presence, never raw command
// text or arguments.
//
// "repository" and "file" fact identity (repo_id, parsed_file_data) is decoded
// through the codegraph contracts seam (decodeCodegraphRepository,
// decodeCodegraphFile, Contract System v1 Wave 4f S2, issue #4754) rather than
// raw semanticPayloadString/payloadMap lookups. A fact whose payload is
// missing a required identity field is skipped, matching this function's
// pre-existing "skip and continue" shape for an absent/blank repo_id or
// parsed_file_data (the same "skip, do not join under an empty identity"
// contract code_import_repo_edge_identity.go's decodeCodegraphFileImportIdentity
// establishes for the same fact kinds).
func ExtractShellExecRows(envelopes []facts.Envelope) ([]string, []map[string]any) {
	if len(envelopes) == 0 {
		return nil, nil
	}
	repoSet := make(map[string]struct{})
	var rows []map[string]any
	seenEdges := make(map[string]struct{})

	for _, env := range envelopes {
		if env.FactKind == factKindRepository {
			repository, err := decodeCodegraphRepository(env)
			if err != nil {
				continue
			}
			if repoID := strings.TrimSpace(repository.RepoID); repoID != "" {
				repoSet[repoID] = struct{}{}
			}
			continue
		}
		if env.FactKind != factKindFile || env.IsTombstone {
			continue
		}
		file, err := decodeCodegraphFile(env)
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
		// precedent in code_call_materialization_intents.go), so it is read
		// raw here only to preserve behavior for callers (and fixtures) that
		// carry the source path under the top-level "path" key.
		sourcePath := semanticPayloadString(env.Payload, "path")
		if sourcePath == "" {
			sourcePath = semanticPayloadString(parsedFileData, "path")
		}
		if repoID == "" || sourcePath == "" {
			continue
		}
		repoSet[repoID] = struct{}{}
		functionIDs := embeddedSQLFunctionIDsByNameLine(parsedFileData)
		for _, command := range mapSlice(parsedFileData["embedded_shell_commands"]) {
			functionName := anyToString(command["function_name"])
			functionLine := codeCallInt(command["function_line_number"])
			lineNumber := codeCallInt(command["line_number"])
			api := anyToString(command["api"])
			if functionName == "" || functionLine <= 0 || lineNumber <= 0 || api == "" {
				continue
			}
			functionEntityID := functionIDs[embeddedSQLFunctionKey(functionName, functionLine)]
			if functionEntityID == "" {
				continue
			}
			targetID := shellExecTargetID(repoID, sourcePath, functionEntityID, lineNumber, api)
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
				"language":           anyToString(command["language"]),
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
		left := anyToString(rows[i]["repo_id"]) + ":" + anyToString(rows[i]["source_path"]) + ":" +
			anyToString(rows[i]["source_entity_id"]) + ":" + anyToString(rows[i]["target_entity_id"])
		right := anyToString(rows[j]["repo_id"]) + ":" + anyToString(rows[j]["source_path"]) + ":" +
			anyToString(rows[j]["source_entity_id"]) + ":" + anyToString(rows[j]["target_entity_id"])
		return left < right
	})
	return repoIDs, rows
}

func shellExecTargetID(repoID, sourcePath, functionEntityID string, lineNumber int, api string) string {
	hash := sha256.New()
	for _, part := range []string{repoID, sourcePath, functionEntityID, anyToString(lineNumber), strings.TrimSpace(api)} {
		hash.Write([]byte(part))
		hash.Write([]byte{0})
	}
	return "shell-command:" + hex.EncodeToString(hash.Sum(nil))
}
