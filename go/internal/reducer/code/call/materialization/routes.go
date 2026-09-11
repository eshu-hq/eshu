// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materialization

import (
	"sort"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/code/call/shared"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
	"github.com/eshu-hq/eshu/go/internal/reducer/schemadecode"
	"github.com/eshu-hq/eshu/go/internal/reducer/sharedintent"
)

// HandlesRouteEvidenceSource labels HANDLES_ROUTE edges so retraction and
// re-projection only ever touch edges this emitter owns.
const HandlesRouteEvidenceSource = "parser/framework-routes"

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
	index shared.EntityIndex,
	contextByRepoID map[string]sharedintent.ProjectionContext,
	createdAt time.Time,
	evidenceSource string,
) []sharedintent.Row {
	if len(envelopes) == 0 || len(contextByRepoID) == 0 {
		return nil
	}
	if evidenceSource == "" {
		evidenceSource = HandlesRouteEvidenceSource
	}

	intents := make([]sharedintent.Row, 0)
	seen := make(map[string]struct{})
	for _, env := range envelopes {
		if env.FactKind != factload.FactKindFile {
			continue
		}
		repositoryID := payloadcore.PayloadStr(env.Payload, "repo_id")
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
		relativePath := payloadcore.PayloadStr(env.Payload, "relative_path")
		rawPath := payloadcore.AnyToString(fileData["path"])
		pathKeys := shared.PathKeys(rawPath, relativePath)

		for _, entry := range handlesRouteEntries(fileData) {
			handler := strings.TrimSpace(payloadcore.AnyToString(entry["handler"]))
			routePath := strings.TrimSpace(payloadcore.AnyToString(entry["path"]))
			if handler == "" || routePath == "" {
				continue
			}
			framework := strings.TrimSpace(payloadcore.AnyToString(entry["framework"]))
			functionID, method := resolveHandlesRouteFunction(index, repositoryID, pathKeys, framework, handler)
			if functionID == "" {
				continue
			}
			httpMethod := strings.ToUpper(strings.TrimSpace(payloadcore.AnyToString(entry["method"])))
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
				"framework":          framework,
				"relative_path":      relativePath,
				"evidence_source":    evidenceSource,
				"resolution_method":  method,
				"confidence":         codeprovenance.Confidence(method),
				"reason":             codeprovenance.Reason(method),
			}

			intents = append(intents, sharedintent.Build(sharedintent.Input{
				ProjectionDomain: reducercontract.DomainHandlesRoute,
				PartitionKey:     functionID + "->" + repositoryID + ":" + routePath,
				ScopeID:          context.ScopeID,
				AcceptanceUnitID: context.ResolveAcceptanceUnitID(repositoryID),
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

// BuildRouteIntentRowsForQueryProof runs the same HANDLES_ROUTE
// materialization pipeline production uses -- schemadecode.BuildProjectionContexts,
// shared.BuildEntityIndex, and buildHandlesRouteIntentRows -- over the given file
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
func BuildRouteIntentRowsForQueryProof(envelopes []facts.Envelope) []sharedintent.Row {
	generationID := "gen-handles-route-query-proof"
	contextByRepoID := schemadecode.BuildProjectionContexts(envelopes, generationID)
	index := shared.BuildEntityIndex(envelopes)
	return buildHandlesRouteIntentRows(envelopes, index, contextByRepoID, time.Unix(0, 0).UTC(), HandlesRouteEvidenceSource)
}

// resolveHandlesRouteFunction resolves a route handler name to exactly one
// Function entity id. It first normalizes the parser-emitted handler token via
// handlesRouteHandlerCandidateNames; for Laravel, that adds dotted candidates
// for short Class@method string callables. It then tries a same-file unique
// match across the route file's path keys, followed by a repository-wide unique
// name. It returns the entity id and provenance method, or an empty id when the
// name is unknown or ambiguous. The index maps retain a name only when it is
// unique in that scope.
func resolveHandlesRouteFunction(
	index shared.EntityIndex,
	repositoryID string,
	pathKeys []string,
	framework string,
	handler string,
) (string, codeprovenance.Method) {
	candidateNames := handlesRouteHandlerCandidateNames(framework, handler)
	for _, pathKey := range pathKeys {
		for _, candidateName := range candidateNames {
			if entityID := index.UniqueNameByPath[pathKey][candidateName]; entityID != "" {
				return entityID, codeprovenance.MethodSameFile
			}
		}
	}
	for _, candidateName := range candidateNames {
		if entityID := index.UniqueNameByRepo[repositoryID][candidateName]; entityID != "" {
			return entityID, codeprovenance.MethodRepoUniqueName
		}
	}
	return "", codeprovenance.MethodUnspecified
}

// handlesRouteHandlerCandidateNames preserves the parser-emitted handler token
// and, for Laravel only, adds dotted forms for its Class@method string-callable
// convention when Class is unqualified. Fully-qualified PHP controllers stay
// unresolved because the parser currently exposes only file-level namespace
// evidence, which is insufficient for files containing multiple namespace
// blocks. It never falls back to a short class or bare method, so a controller
// in another namespace cannot bind merely because its terminal class or method
// name is unique, and unrelated frameworks keep their existing token semantics.
func handlesRouteHandlerCandidateNames(framework string, handler string) []string {
	handler = strings.TrimSpace(handler)
	if handler == "" {
		return nil
	}
	candidates := []string{handler}
	if !strings.EqualFold(strings.TrimSpace(framework), "laravel") || strings.Count(handler, "@") != 1 {
		return candidates
	}
	className, methodName, _ := strings.Cut(handler, "@")
	className = strings.Trim(strings.TrimSpace(className), `\`)
	methodName = strings.TrimSpace(methodName)
	if className == "" || methodName == "" {
		return candidates
	}
	if strings.Contains(className, `\`) {
		return candidates
	}
	candidates = append(candidates, className+"."+methodName)
	return candidates
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
	frameworks := payloadcore.ToStringSlice(semantics["frameworks"])
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
		for _, entry := range payloadcore.MapSlice(rawEntries) {
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
