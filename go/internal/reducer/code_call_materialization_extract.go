// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"log/slog"
	"sort"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// ExtractAllCodeRelationshipRows builds both code-call and metaclass edge rows
// from a single entity index pass. This eliminates the duplicate
// buildCodeEntityIndex call that occurs when ExtractCodeCallRows and
// ExtractPythonMetaclassRows are called separately.
func ExtractAllCodeRelationshipRows(envelopes []facts.Envelope) (
	codeCallRepoIDs []string,
	codeCallRows []map[string]any,
	metaclassRepoIDs []string,
	metaclassRows []map[string]any,
) {
	ccRepoIDs, ccRows, mcRepoIDs, mcRows, _, _ := extractAllCodeRelationshipRowsWithIndex(envelopes)
	return ccRepoIDs, ccRows, mcRepoIDs, mcRows
}

// extractAllCodeRelationshipRowsWithIndex builds code-call and metaclass edge
// rows and also returns the shared codeEntityIndex it built, so callers that
// need the index for an additional resolution pass (for example HANDLES_ROUTE
// handler resolution, #2721) reuse one index build instead of paying for a
// second pass over the same envelopes.
//
// It partitions the "file" facts ONCE through the codegraph decode seam before
// any extraction: a fact whose outer envelope is missing a required
// join-identity field (repo_id, relative_path, or parsed_file_data) is
// quarantined and EXCLUDED from every downstream consumer — the shared entity
// index, the code-call extractor, AND the metaclass extractor — so a malformed
// fact cannot be dead-lettered on the code-call path while still emitting
// USES_METACLASS intents on the metaclass path (the quarantine is authoritative
// for the whole code-relationship extraction, not just code-calls). The
// non-"file" envelopes (repository, content_entity, ...) pass through unchanged
// because the other index/import builders consume them.
func extractAllCodeRelationshipRowsWithIndex(envelopes []facts.Envelope) (
	codeCallRepoIDs []string,
	codeCallRows []map[string]any,
	metaclassRepoIDs []string,
	metaclassRows []map[string]any,
	entityIndex codeEntityIndex,
	quarantined []quarantinedFact,
) {
	if len(envelopes) == 0 {
		return nil, nil, nil, nil, codeEntityIndex{}, nil
	}

	validEnvelopes, quarantined := partitionCodegraphFileFacts(envelopes)

	repositoryIDs := collectCodeCallRepositoryIDs(validEnvelopes)
	if len(repositoryIDs) == 0 {
		return nil, nil, nil, nil, codeEntityIndex{}, quarantined
	}

	entityIndex = buildCodeEntityIndex(validEnvelopes)
	repositoryImports := collectCodeCallRepositoryImports(validEnvelopes)
	reexportIndex := buildCodeCallReexportIndex(validEnvelopes)

	ccRepoIDs, ccRows := extractCodeCallRowsWithIndex(validEnvelopes, repositoryIDs, entityIndex, repositoryImports, reexportIndex)
	mcRepoIDs, mcRows := extractPythonMetaclassRowsWithIndex(validEnvelopes, repositoryIDs, entityIndex, repositoryImports)
	return ccRepoIDs, ccRows, mcRepoIDs, mcRows, entityIndex, quarantined
}

// ExtractCodeCallRows builds canonical caller/callee edge rows from repository
// and file facts. A "file" fact whose outer envelope is missing a required
// join-identity field (repo_id, relative_path, or parsed_file_data) is
// excluded rather than contributing an empty-string graph identity; use
// partitionCodegraphFileFacts + extractCodeCallRowsWithIndex directly when the
// caller needs to observe which facts were quarantined.
func ExtractCodeCallRows(envelopes []facts.Envelope) ([]string, []map[string]any) {
	if len(envelopes) == 0 {
		return nil, nil
	}

	validEnvelopes, _ := partitionCodegraphFileFacts(envelopes)
	repositoryIDs := collectCodeCallRepositoryIDs(validEnvelopes)
	if len(repositoryIDs) == 0 {
		return nil, nil
	}

	entityIndex := buildCodeEntityIndex(validEnvelopes)
	repositoryImports := collectCodeCallRepositoryImports(validEnvelopes)
	reexportIndex := buildCodeCallReexportIndex(validEnvelopes)
	repoIDs, rows := extractCodeCallRowsWithIndex(validEnvelopes, repositoryIDs, entityIndex, repositoryImports, reexportIndex)
	return repoIDs, rows
}

// partitionCodegraphFileFacts decodes each "file" fact's outer envelope through
// the codegraph contracts seam (decodeCodegraphFile) and splits the envelope
// list into (valid, quarantined): a "file" fact whose payload is missing a
// required identity field (repo_id, relative_path, or a non-object
// parsed_file_data) is recorded as a quarantinedFact and DROPPED from the valid
// set, while every valid "file" fact and every non-"file" fact is kept in
// order. It is the single decode/quarantine gate for the whole
// code-relationship extraction (Contract System v1 Wave 4f S1, issue #4749), so
// a malformed file is excluded from the shared entity index and both extractors
// at once — before this conversion a missing repo_id/relative_path read through
// payloadStr as "", silently producing no edges or edges under an empty-string
// path segment with no operator signal.
//
// ParsedFileData stays untyped past decode: the returned struct's inner AST
// keys (imports, functions, function_calls, classes, ...) are read exactly as
// before this conversion (issue #4750 defers typing them). A non-"file" fact
// is never decoded here (repository/content_entity facts have their own index
// builders); it passes through unchanged.
func partitionCodegraphFileFacts(envelopes []facts.Envelope) ([]facts.Envelope, []quarantinedFact) {
	valid := make([]facts.Envelope, 0, len(envelopes))
	var quarantined []quarantinedFact

	for _, env := range envelopes {
		if env.FactKind != factKindFile {
			valid = append(valid, env)
			continue
		}

		if _, err := decodeCodegraphFile(env); err != nil {
			// Every decode failure is recorded as a visible quarantine, never
			// silently dropped. partitionDecodeFailures classifies a
			// missing/null required field (repo_id, relative_path,
			// parsed_file_data) as a quarantinable input_invalid; any other
			// decode error (a type mismatch, or an unsupported schema major)
			// is reported through codegraphDecodeQuarantine with the decode
			// error's own classification, so the malformed fact still surfaces
			// on the input_invalid counter and error log rather than vanishing.
			// factschemaEnvelope normalizes the version-less spellings ("" and
			// the persisted "0.0.0" sentinel) to the latest major, so a valid
			// loaded "file" fact decodes and an unsupported major does not occur
			// on the production path for this unregistered kind (registry +
			// schema-version admission deferred to #4752).
			if q, ok, _ := partitionDecodeFailures(env, err); ok {
				quarantined = append(quarantined, q)
			} else {
				quarantined = append(quarantined, codegraphDecodeQuarantine(env, err))
			}
			continue
		}
		valid = append(valid, env)
	}

	return valid, quarantined
}

// extractCodeCallRowsWithIndex builds caller/callee edge rows from the "file"
// facts in envelopes. Callers pass the VALID-only envelope set from
// partitionCodegraphFileFacts, so every "file" fact here already decoded
// cleanly; this function re-decodes each to recover its typed identity
// (RepoID, RelativePath, ParsedFileData) rather than re-reading raw payload
// keys. A "file" fact that somehow fails to decode is skipped defensively (the
// authoritative quarantine happened upstream in partitionCodegraphFileFacts).
//
// ParsedFileData stays untyped: the returned struct's inner AST keys are read
// exactly as before this conversion (issue #4750 defers typing them).
func extractCodeCallRowsWithIndex(
	envelopes []facts.Envelope,
	repositoryIDs []string,
	entityIndex codeEntityIndex,
	repositoryImports map[string]map[string][]string,
	reexportIndex codeCallReexportIndex,
) ([]string, []map[string]any) {
	cacheCodeCallRepositoryImportPaths(&entityIndex, repositoryImports)
	seenRows := make(map[string]struct{})
	rows := make([]map[string]any, 0)

	for _, env := range envelopes {
		if env.FactKind != factKindFile {
			continue
		}

		file, err := decodeCodegraphFile(env)
		if err != nil {
			// Unreachable for envelopes from partitionCodegraphFileFacts (every
			// "file" fact there already decoded); skip defensively so a future
			// caller passing un-partitioned envelopes cannot produce an
			// empty-identity row.
			continue
		}

		repositoryID := file.RepoID
		fileData := file.ParsedFileData
		relativePath := file.RelativePath

		rows = append(rows, extractSCIPCodeCallRows(repositoryID, entityIndex, seenRows, fileData)...)
		rows = append(
			rows,
			extractGenericCodeCallRows(
				repositoryID,
				relativePath,
				anyToString(fileData["path"]),
				entityIndex,
				repositoryImports[repositoryID],
				reexportIndex,
				seenRows,
				fileData,
			)...,
		)
	}

	sort.Slice(rows, func(i, j int) bool {
		left := anyToString(rows[i]["caller_entity_id"]) + "->" + anyToString(rows[i]["callee_entity_id"])
		right := anyToString(rows[j]["caller_entity_id"]) + "->" + anyToString(rows[j]["callee_entity_id"])
		if left == right {
			return anyToString(rows[i]["repo_id"]) < anyToString(rows[j]["repo_id"])
		}
		return left < right
	})

	recordCodeCallSelfLoopWritten(rows)
	return repositoryIDs, rows
}

// recordCodeCallSelfLoopWritten observes a materialized code-call row whose
// caller and callee resolve to the same entity (a genuinely recursive
// function/method) and logs a per-language tally.
//
// This is deliberately observe-only: unlike the skipped_self_loop counters on
// the IAM CAN_PERFORM/escalation projections (which drop a self-referential
// grant as noise), a code-call self-loop is real recursion — legitimate graph
// truth — and MUST still be written. Filtering it here would be the same
// class of accuracy bug as eshu-hq/eshu#5332 (the raw Dart byte-scanner
// recording every declaration as a self-call), just inverted: instead of a
// false self-loop being written, a true one would be silently dropped. See
// go/internal/parser/dart/calls.go for the parser-side fix that stopped
// declarations from ever reaching this path as spurious self-loops; this
// tally exists so a future regression in the opposite direction (a resolver
// change that starts filtering real recursion) is visible in logs rather
// than silently changing fan-in/call-count signals corpus-wide.
func recordCodeCallSelfLoopWritten(rows []map[string]any) {
	tally := make(map[string]int)
	total := 0
	for _, row := range rows {
		callerID := anyToString(row["caller_entity_id"])
		calleeID := anyToString(row["callee_entity_id"])
		if callerID == "" || callerID != calleeID {
			continue
		}
		lang := anyToString(row["lang"])
		if lang == "" {
			lang = "unknown"
		}
		tally[lang]++
		total++
	}
	if total == 0 {
		return
	}
	slog.Info(
		"code call self loop written",
		"event", "code_call_self_loop_written",
		"total", total,
		"by_lang", tally,
	)
}

func collectCodeCallRepositoryIDs(envelopes []facts.Envelope) []string {
	repositorySet := make(map[string]struct{})
	for _, env := range envelopes {
		switch env.FactKind {
		case "repository", "file":
			repositoryID := payloadStr(env.Payload, "repo_id")
			if repositoryID == "" {
				repositoryID = payloadStr(env.Payload, "graph_id")
			}
			if repositoryID != "" {
				repositorySet[repositoryID] = struct{}{}
			}
		}
	}

	repositoryIDs := make([]string, 0, len(repositorySet))
	for repositoryID := range repositorySet {
		repositoryIDs = append(repositoryIDs, repositoryID)
	}
	sort.Strings(repositoryIDs)
	return repositoryIDs
}
