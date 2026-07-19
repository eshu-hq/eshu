// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"sort"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// handlesRouteEvidenceSource labels HANDLES_ROUTE edges so retraction and
// re-projection only ever touch edges this emitter owns.
const handlesRouteEvidenceSource = "parser/framework-routes"

// buildHandlesRouteIntentRows resolves parser-owned framework route handlers to
// Function entities and emits one ordering-safe shared-projection intent per
// exact, unambiguous resolution (#2721).
//
// For every file envelope's framework route_entries it reads the entry handler
// name and resolves it to exactly one Function entity id, preferring a
// same-file unique match (codeprovenance.MethodSameFile) and falling back to a
// repository-wide unique name (codeprovenance.MethodRepoUniqueName). If the name
// resolves to zero or more than one Function — within the file or across the
// repository — the entry is skipped and no edge is produced, because a wrong or
// guessed handler binding would corrupt graph truth. Frameworks without a
// route_entries slice are tolerated and skipped; this preserves exact-only
// behavior for older facts or frameworks that still model roots without routes.
func buildHandlesRouteIntentRows(
	envelopes []facts.Envelope,
	index codeEntityIndex,
	contextByRepoID map[string]ProjectionContext,
	createdAt time.Time,
	evidenceSource string,
) []SharedProjectionIntentRow {
	if len(envelopes) == 0 || len(contextByRepoID) == 0 {
		return nil
	}
	if evidenceSource == "" {
		evidenceSource = handlesRouteEvidenceSource
	}

	intents := make([]SharedProjectionIntentRow, 0)
	seen := make(map[string]struct{})
	for _, env := range envelopes {
		if env.FactKind != factKindFile {
			continue
		}
		repositoryID := payloadStr(env.Payload, "repo_id")
		if repositoryID == "" {
			continue
		}
		context, ok := contextByRepoID[repositoryID]
		if !ok {
			continue
		}
		fileData, ok := env.Payload["parsed_file_data"].(map[string]any)
		if !ok {
			continue
		}
		relativePath := payloadStr(env.Payload, "relative_path")
		rawPath := anyToString(fileData["path"])
		pathKeys := codeCallPathKeys(rawPath, relativePath)

		for _, entry := range handlesRouteEntries(fileData) {
			handler := strings.TrimSpace(anyToString(entry["handler"]))
			routePath := strings.TrimSpace(anyToString(entry["path"]))
			if handler == "" || routePath == "" {
				continue
			}
			functionID, method := resolveHandlesRouteFunction(index, repositoryID, pathKeys, handler)
			if functionID == "" {
				continue
			}
			httpMethod := strings.ToUpper(strings.TrimSpace(anyToString(entry["method"])))
			dedupeKey := functionID + "\x00" + repositoryID + "\x00" + routePath + "\x00" + httpMethod
			if _, exists := seen[dedupeKey]; exists {
				continue
			}
			seen[dedupeKey] = struct{}{}

			payload := map[string]any{
				"function_entity_id": functionID,
				"repo_id":            repositoryID,
				"path":               routePath,
				"http_method":        httpMethod,
				"framework":          strings.TrimSpace(anyToString(entry["framework"])),
				"relative_path":      relativePath,
				"evidence_source":    evidenceSource,
				"resolution_method":  method,
				"confidence":         codeprovenance.Confidence(method),
				"reason":             codeprovenance.Reason(method),
			}

			intents = append(intents, BuildSharedProjectionIntent(SharedProjectionIntentInput{
				ProjectionDomain: DomainHandlesRoute,
				PartitionKey:     functionID + "->" + repositoryID + ":" + routePath,
				ScopeID:          context.ScopeID,
				AcceptanceUnitID: context.acceptanceUnitID(repositoryID),
				RepositoryID:     repositoryID,
				SourceRunID:      context.SourceRunID,
				GenerationID:     context.GenerationID,
				Payload:          payload,
				CreatedAt:        createdAt,
			}))
		}
	}

	sort.SliceStable(intents, func(i, j int) bool {
		if intents[i].RepositoryID != intents[j].RepositoryID {
			return intents[i].RepositoryID < intents[j].RepositoryID
		}
		return intents[i].IntentID < intents[j].IntentID
	})
	return intents
}

// BuildHandlesRouteIntentRowsForQueryProof runs the same HANDLES_ROUTE
// materialization pipeline production uses -- buildCodeCallProjectionContexts,
// buildCodeEntityIndex, and buildHandlesRouteIntentRows -- over the given file
// envelopes and returns the resulting intent rows.
//
// It exists solely so internal/query's Java route-to-caller proof test
// (code_route_to_caller_java_test.go) can derive its fakeGraphReader
// HANDLES_ROUTE row from the reducer's real materialized output instead of
// hand-inventing literals that could silently diverge from what the reducer
// actually writes. That gap was flagged as a false-green risk on #5333: a
// break at the reducer-to-edge boundary would otherwise leave the query test
// green because its fake row was disconnected from reducer behavior. Calling
// this from the query test closes that seam without exporting the full
// internal materialization surface.
func BuildHandlesRouteIntentRowsForQueryProof(envelopes []facts.Envelope) []SharedProjectionIntentRow {
	generationID := "gen-handles-route-query-proof"
	contextByRepoID := buildCodeCallProjectionContexts(envelopes, generationID)
	index := buildCodeEntityIndex(envelopes)
	return buildHandlesRouteIntentRows(envelopes, index, contextByRepoID, time.Unix(0, 0).UTC(), handlesRouteEvidenceSource)
}

// resolveHandlesRouteFunction resolves a route handler name to exactly one
// Function entity id. It first tries a same-file unique match across the route
// file's path keys, then a repository-wide unique name. It returns the entity id
// and the provenance method that resolved it, or an empty id when the name is
// unknown or ambiguous. The underlying index maps store a name only when it is
// unique in that scope, so a hit is always a single Function and a miss covers
// both the zero- and multiple-match cases.
func resolveHandlesRouteFunction(
	index codeEntityIndex,
	repositoryID string,
	pathKeys []string,
	handler string,
) (string, codeprovenance.Method) {
	for _, pathKey := range pathKeys {
		if entityID := index.uniqueNameByPath[pathKey][handler]; entityID != "" {
			return entityID, codeprovenance.MethodSameFile
		}
	}
	if entityID := index.uniqueNameByRepo[repositoryID][handler]; entityID != "" {
		return entityID, codeprovenance.MethodRepoUniqueName
	}
	return "", codeprovenance.MethodUnspecified
}

// handlesRouteEntries returns the framework route entries declared for a file,
// flattened across every framework the parser tagged. Each returned map carries
// the originating framework name under the "framework" key so the emitted
// intent can record provenance. Frameworks without a route_entries slice are
// skipped.
func handlesRouteEntries(fileData map[string]any) []map[string]any {
	semantics, ok := fileData["framework_semantics"].(map[string]any)
	if !ok {
		return nil
	}
	frameworks := toStringSlice(semantics["frameworks"])
	if len(frameworks) == 0 {
		return nil
	}

	var entries []map[string]any
	for _, framework := range frameworks {
		framework = strings.TrimSpace(framework)
		frameworkData, _ := semantics[framework].(map[string]any)
		if framework == "" || frameworkData == nil {
			continue
		}
		rawEntries, ok := frameworkData["route_entries"]
		if !ok {
			continue
		}
		for _, entry := range mapSlice(rawEntries) {
			withFramework := make(map[string]any, len(entry)+1)
			for key, value := range entry {
				withFramework[key] = value
			}
			withFramework["framework"] = framework
			entries = append(entries, withFramework)
		}
	}
	return entries
}
