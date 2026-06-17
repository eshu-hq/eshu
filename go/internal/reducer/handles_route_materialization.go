package reducer

import (
	"sort"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// handlesRouteEvidenceSource labels HANDLES_ROUTE edges so an operator can trace
// each edge back to the parser's framework route-to-handler binding.
const handlesRouteEvidenceSource = "parser/framework-routes"

// handlesRouteRow is one resolved Function-to-Endpoint binding, flattened into
// the JSON-safe payload shape consumed by the shared-projection edge writer.
type handlesRouteRow struct {
	repositoryID     string
	functionID       string
	path             string
	method           string
	resolutionMethod codeprovenance.Method
}

// extractHandlesRouteRows scans file facts for framework route entries that
// carry an unambiguous handler symbol and resolves each handler to a single
// Function uid using the same per-generation code-entity index the CALLS edge
// uses. Resolution prefers the route's own file (MethodSameFile) and falls back
// to a repository-wide unique-name match (MethodRepoUniqueName). A handler that
// resolves to zero or to more than one Function is dropped: an ambiguous binding
// never fabricates an edge. It is co-located with code-call extraction because
// the entity index and the file-fact scan already exist there.
func extractHandlesRouteRows(envelopes []facts.Envelope, index codeEntityIndex) []map[string]any {
	rows := make([]map[string]any, 0)
	seen := make(map[string]struct{})
	for _, env := range envelopes {
		if env.FactKind != factKindFile {
			continue
		}
		repositoryID := payloadStr(env.Payload, "repo_id")
		if repositoryID == "" {
			continue
		}
		fileData, ok := env.Payload["parsed_file_data"].(map[string]any)
		if !ok {
			continue
		}
		semantics, ok := fileData["framework_semantics"].(map[string]any)
		if !ok {
			continue
		}
		relativePath := payloadStr(env.Payload, "relative_path")
		rawPath := anyToString(fileData["path"])
		for _, entry := range handlesRouteEntries(semantics) {
			resolved, ok := resolveHandlesRouteEntry(index, repositoryID, rawPath, relativePath, entry)
			if !ok {
				continue
			}
			key := resolved.repositoryID + "|" + resolved.functionID + "|" + resolved.path + "|" + resolved.method
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			rows = append(rows, map[string]any{
				"repo_id":           resolved.repositoryID,
				"function_id":       resolved.functionID,
				"path":              resolved.path,
				"method":            resolved.method,
				"resolution_method": resolved.resolutionMethod,
			})
		}
	}

	sort.SliceStable(rows, func(i, j int) bool {
		left := anyToString(rows[i]["repo_id"]) + "|" + anyToString(rows[i]["function_id"]) + "|" + anyToString(rows[i]["path"]) + "|" + anyToString(rows[i]["method"])
		right := anyToString(rows[j]["repo_id"]) + "|" + anyToString(rows[j]["function_id"]) + "|" + anyToString(rows[j]["path"]) + "|" + anyToString(rows[j]["method"])
		return left < right
	})
	return rows
}

// handlesRouteEntry is one parser-emitted route registration with the optional
// handler symbol the binding resolves against.
type handlesRouteEntry struct {
	method  string
	path    string
	handler string
}

// handlesRouteEntries flattens framework_semantics into the route entries that
// carry a non-empty handler. Entries without a handler are skipped because the
// parser only records a handler when it observed an unambiguous route-to-handler
// binding; a missing handler means the route must not produce an edge.
func handlesRouteEntries(semantics map[string]any) []handlesRouteEntry {
	frameworks, _ := semantics["frameworks"].([]any)
	if len(frameworks) == 0 {
		return nil
	}
	entries := make([]handlesRouteEntry, 0, len(frameworks))
	for _, frameworkRaw := range frameworks {
		framework, _ := frameworkRaw.(string)
		if framework == "" {
			continue
		}
		frameworkData, _ := semantics[framework].(map[string]any)
		if frameworkData == nil {
			continue
		}
		for _, item := range mapSlice(frameworkData["route_entries"]) {
			handler := strings.TrimSpace(anyToString(item["handler"]))
			routePath := strings.TrimSpace(anyToString(item["path"]))
			if handler == "" || routePath == "" {
				continue
			}
			entries = append(entries, handlesRouteEntry{
				method:  strings.ToLower(strings.TrimSpace(anyToString(item["method"]))),
				path:    routePath,
				handler: handler,
			})
		}
	}
	return entries
}

// resolveHandlesRouteEntry resolves one route entry's handler symbol to a single
// Function uid. Same-file resolution is strongest because the handler is
// registered in the route's own file; only when the name is absent in-file does
// it fall back to a repository-wide unique-name match. A name that does not
// resolve to exactly one Function returns false so the caller drops it.
func resolveHandlesRouteEntry(
	index codeEntityIndex,
	repositoryID string,
	rawPath string,
	relativePath string,
	entry handlesRouteEntry,
) (handlesRouteRow, bool) {
	for _, pathKey := range codeCallPathKeys(rawPath, relativePath) {
		if functionID := index.uniqueNameByPath[pathKey][entry.handler]; functionID != "" {
			if index.entityTypeByID[functionID] != "Function" {
				continue
			}
			return handlesRouteRow{
				repositoryID:     repositoryID,
				functionID:       functionID,
				path:             entry.path,
				method:           entry.method,
				resolutionMethod: codeprovenance.MethodSameFile,
			}, true
		}
	}
	if functionID := index.uniqueNameByRepo[repositoryID][entry.handler]; functionID != "" {
		if index.entityTypeByID[functionID] != "Function" {
			return handlesRouteRow{}, false
		}
		return handlesRouteRow{
			repositoryID:     repositoryID,
			functionID:       functionID,
			path:             entry.path,
			method:           entry.method,
			resolutionMethod: codeprovenance.MethodRepoUniqueName,
		}, true
	}
	return handlesRouteRow{}, false
}

// buildHandlesRouteSharedIntentRows turns resolved HANDLES_ROUTE rows into
// durable shared-projection intents on the DomainHandlesRoute domain. Each intent
// is one (repo, function uid, route path, method) binding partitioned by
// repository, with the resolution method's tiered confidence and reason copied
// from codeprovenance so an operator sees the same provenance contract the CALLS
// edge carries.
func buildHandlesRouteSharedIntentRows(
	rows []map[string]any,
	contextByRepoID map[string]ProjectionContext,
	createdAt time.Time,
	evidenceSource string,
) []SharedProjectionIntentRow {
	intents := make([]SharedProjectionIntentRow, 0, len(rows))
	for _, row := range rows {
		repositoryID := anyToString(row["repo_id"])
		functionID := anyToString(row["function_id"])
		routePath := anyToString(row["path"])
		if repositoryID == "" || functionID == "" || routePath == "" {
			continue
		}
		context, ok := contextByRepoID[repositoryID]
		if !ok {
			continue
		}

		method := codeCallResolutionMethodForRow(row)
		payload := map[string]any{
			"repo_id":           repositoryID,
			"function_id":       functionID,
			"path":              routePath,
			"method":            anyToString(row["method"]),
			"evidence_source":   evidenceSource,
			"resolution_method": method,
			"confidence":        codeprovenance.Confidence(method),
			"reason":            codeprovenance.Reason(method),
		}

		identityKey := strings.Join([]string{
			"handles_route",
			functionID,
			routePath,
			anyToString(row["method"]),
		}, "|")

		intents = append(intents, BuildSharedProjectionIntent(SharedProjectionIntentInput{
			ProjectionDomain: DomainHandlesRoute,
			PartitionKey:     repositoryID,
			IdentityKey:      identityKey,
			ScopeID:          context.ScopeID,
			AcceptanceUnitID: context.acceptanceUnitID(repositoryID),
			RepositoryID:     repositoryID,
			SourceRunID:      context.SourceRunID,
			GenerationID:     context.GenerationID,
			Payload:          payload,
			CreatedAt:        createdAt,
		}))
	}

	sort.SliceStable(intents, func(i, j int) bool {
		if intents[i].RepositoryID != intents[j].RepositoryID {
			return intents[i].RepositoryID < intents[j].RepositoryID
		}
		return intents[i].IntentID < intents[j].IntentID
	})
	return intents
}

// codeCallResolutionMethodForRow reads the resolution method an extraction row
// stamped, defaulting to MethodUnspecified when absent so confidence and reason
// always derive from a concrete vocabulary value.
func codeCallResolutionMethodForRow(row map[string]any) codeprovenance.Method {
	if method := strings.TrimSpace(anyToString(row["resolution_method"])); method != "" {
		return method
	}
	return codeprovenance.MethodUnspecified
}
